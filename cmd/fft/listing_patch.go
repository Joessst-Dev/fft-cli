package main

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/client"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

const listingPatchLong = `Change some fields of one listing, leaving the rest alone.

  fft listing patch --facility BER-01 4711 --status INACTIVE
  fft listing patch --facility BER-01 4711 --title "Adidas Superstar" --price 89.95

Taking a listing INACTIVE removes it from the catalog of that facility without
deleting it, and without touching its stock. That is usually what you want when
an article should stop being offered.

fft reads the listing, applies your changes, and writes them back with the
version it just read — the API's optimistic locking lives in the request body,
not in an If-Match header, so a mutation is always a read-then-write. If someone
else wrote in between, the API answers 409 and fft reads again and retries,
once. A second 409 is not retried: at that point something is writing faster
than fft can read, and saying so is more useful than trying again.

--if-version skips the read: fft sends the version you name and the API answers
409 if it is stale. That is one request instead of two. (It is --if-version and
never --version: cobra owns --version on the root command.)`

// listingModifyAction is the action name the PATCH body's discriminator takes
// (swagger:50285). The listing PATCH is not a field patch: its body is
// {version, actions: [...]} and the actions are discriminated on `action` — so
// even a one-field change travels as an action.
const listingModifyAction = "ModifyListing"

func newListingPatchCmd(deps *Deps) *cobra.Command {
	var (
		facility string
		status   string
		title    string
		price    float64
		version  versionFlag
	)

	cmd := &cobra.Command{
		Use:   "patch --facility <id> <tenantArticleId>",
		Short: "Change some fields of one listing",
		Long:  listingPatchLong,
		Args:  usageArgs(cobra.ExactArgs(1)),

		Annotations: map[string]string{annotationOperationID: "patchFacilityListing"},

		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := requireFacility(cmd, facility)
			if err != nil {
				return err
			}
			if err := version.check(); err != nil {
				return err
			}

			article := args[0]

			changes, err := listingChanges(cmd, status, title, price)
			if err != nil {
				return err
			}

			c, err := tenantClient(deps)
			if err != nil {
				return err
			}

			ctx, cancel := deps.Context(cmd)
			defer cancel()

			var raw []byte

			// Only the version is wanted from the listing. The PATCH body carries the
			// version and the actions, and nothing else — sending back the fields fft did
			// not touch would turn a patch into a replace.
			get := func(ctx context.Context) (entityDoc, int, error) {
				_, v, err := getListingDoc(ctx, c, ref, article)
				if err != nil {
					return nil, 0, err
				}
				return entityDoc{}, v, nil
			}

			put := func(ctx context.Context, doc entityDoc, v int) (entityDoc, error) {
				doc["version"] = v

				op := fmt.Sprintf("patch listing %s of facility %s", article, ref)
				answer, err := sendEntity(ctx, c, op, doc,
					func(ctx context.Context, body io.Reader) (*http.Response, error) {
						return c.API().PatchFacilityListingWithBody(ctx, ref, article, contentTypeJSON, body)
					})
				if err != nil {
					return nil, err
				}
				raw = answer
				return nil, nil
			}

			// Re-applied to the *fresh* listing after a 409, which is the whole point of
			// expressing the change as a function rather than as a prebuilt body.
			apply := func(doc *entityDoc) error {
				*doc = entityDoc{"actions": []any{changes}}
				return nil
			}

			if _, err := client.UpdateVersioned(ctx, get, put, apply, version.value()); err != nil {
				return err
			}

			deps.Printer.Notef("Patched listing %s of facility %s.", article, ref)
			return renderListing(deps, raw)
		},
	}

	f := cmd.Flags()
	f.StringVar(&facility, "facility", "", "The facility, by tenantFacilityId or platform UUID (required)")
	f.StringVar(&status, "status", "", "New state: "+strings.Join(listingStatuses(), ", "))
	f.StringVar(&title, "title", "", "New title")
	f.Float64Var(&price, "price", 0, "New price")
	version.register(f)

	registerEnumCompletion(cmd, "status", listingStatuses())

	return cmd
}

// listingChanges builds the single ModifyListing action from the flags.
//
// A patch with nothing in it is refused rather than sent: the API would answer 200
// and change nothing, which looks exactly like success and is how a broken script
// goes unnoticed for a week.
//
// Every flag is read through Changed(), not through its value. --price 0 is a
// legitimate price and --title "" is a legitimate (if unwise) title; guarding on
// the zero value would make both of them silently do nothing.
func listingChanges(cmd *cobra.Command, status, title string, price float64) (entityDoc, error) {
	action := entityDoc{"action": listingModifyAction}
	changed := false

	if cmd.Flags().Changed("title") {
		action["title"] = title
		changed = true
	}
	if cmd.Flags().Changed("price") {
		if err := checkPrice(price); err != nil {
			return nil, err
		}
		action["price"] = price
		changed = true
	}
	if cmd.Flags().Changed("status") {
		v, err := enumValue("status", status, listingStatuses())
		if err != nil {
			return nil, err
		}
		action["status"] = v
		changed = true
	}

	if !changed {
		return nil, exitcode.UsageError{Err: fmt.Errorf(
			"there is nothing to patch: pass --status, --title or --price")}
	}
	return action, nil
}

// checkPrice refuses a price the JSON encoder cannot represent, and one the API
// will not accept.
//
// NaN and Inf are checked explicitly: pflag parses both, every comparison against
// NaN is false so a range check alone waves them through, and they die later
// inside json.Marshal as an unclassified "unsupported value". A negative price is
// refused because the schema's minimum is 0 and the API's complaint about it does
// not name the field.
func checkPrice(price float64) error {
	switch {
	case math.IsNaN(price) || math.IsInf(price, 0):
		return exitcode.UsageError{Err: fmt.Errorf("--price must be a number, and %g is not", price)}
	case price < 0:
		return exitcode.UsageError{Err: fmt.Errorf("--price cannot be negative, and %g is", price)}
	}
	return nil
}
