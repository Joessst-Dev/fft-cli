package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/client"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

const facilityPatchLong = `Change some fields of a facility, leaving the rest alone.

Unlike 'fft facility update', this never deletes a field you did not mention.

fft reads the facility, applies your changes, and writes it back with the version
it just read — the API's optimistic locking lives in the body, not in an
If-Match header, so a mutation is always a read-then-write. If someone else wrote
in between, the API answers 409 and fft reads again and retries, once. A second
409 is not retried: at that point something is writing faster than fft can read,
and saying so is more useful than trying again.

  fft facility patch BER-01 --name "Berlin Mitte"
  fft facility patch BER-01 --status SUSPENDED

--if-version skips the read. Because the PATCH body is discriminated on the
facility's type and the read is where fft would have learned it, --type must then
be given too.`

func newFacilityPatchCmd(deps *Deps) *cobra.Command {
	var (
		name     string
		status   string
		typ      string
		tenantID string
		version  versionFlag
	)

	cmd := &cobra.Command{
		Use:   "patch <id>",
		Short: "Change some fields of a facility",
		Long:  facilityPatchLong,
		Args:  usageArgs(cobra.ExactArgs(1)),

		Annotations: map[string]string{annotationOperationID: "patchFacility"},

		RunE: func(cmd *cobra.Command, args []string) error {
			if err := version.check(); err != nil {
				return err
			}

			changes, err := facilityChanges(cmd, name, status, tenantID)
			if err != nil {
				return err
			}

			// Without a read there is nothing to learn the discriminator from, and a
			// PATCH body without a type is a 400 that does not say why.
			if version.value() != nil && typ == "" {
				return exitcode.UsageError{Err: fmt.Errorf(
					"--if-version needs --type as well: it skips the read, and the PATCH body has to name the facility's type (%s)",
					strings.Join(facilityTypes(), " or "))}
			}

			wantType := ""
			if typ != "" {
				if wantType, err = enumValue("type", typ, facilityTypes()); err != nil {
					return err
				}
			}

			// --type supplies the discriminator only when there is no read to learn it
			// from. On the read path the API has already told fft what the facility is,
			// and letting the flag overwrite that would send a body whose discriminator
			// contradicts the entity it addresses.
			if version.value() != nil {
				changes["type"] = wantType
			}

			c, err := tenantClient(deps)
			if err != nil {
				return err
			}

			ctx, cancel := deps.Context(cmd)
			defer cancel()

			ref := client.FacilityRef(args[0])
			var raw []byte

			// The read fetches the whole facility, but only the discriminator survives
			// into the request: a PATCH body carries the type, the version and the
			// fields being changed, and nothing else. Sending the fields fft did not
			// touch back again would turn a patch into a replace.
			get := func(ctx context.Context) (entityDoc, int, error) {
				current, v, err := getFacilityDoc(ctx, c, ref)
				if err != nil {
					return nil, 0, err
				}

				// A --type that disagrees with the facility is a mistake, not an
				// instruction: the type is fixed at creation, so the user is either
				// patching something other than what they think, or expecting a
				// conversion the API does not offer. Either way, say so.
				actual := docString(current, "type")
				if wantType != "" && wantType != actual {
					return nil, 0, exitcode.UsageError{Err: fmt.Errorf(
						"facility %s is a %s, not a %s — and a facility's type cannot be changed",
						ref, actual, wantType)}
				}

				return entityDoc{"type": actual}, v, nil
			}

			put := func(ctx context.Context, doc entityDoc, v int) (entityDoc, error) {
				doc["version"] = v

				answer, err := sendEntity(ctx, c, "patch facility "+ref, doc,
					func(ctx context.Context, body io.Reader) (*http.Response, error) {
						return c.API().PatchFacilityWithBody(ctx, ref, contentTypeJSON, body)
					})
				if err != nil {
					return nil, err
				}
				raw = answer
				return nil, nil
			}

			// Re-applied to the *fresh* facility after a 409, which is the whole point
			// of expressing the change as a function rather than as a prebuilt body.
			apply := func(doc *entityDoc) error {
				if *doc == nil {
					*doc = entityDoc{}
				}
				for k, v := range changes {
					(*doc)[k] = v
				}
				return nil
			}

			if _, err := client.UpdateVersioned(ctx, get, put, apply, version.value()); err != nil {
				return err
			}

			deps.Printer.Notef("Patched facility %s.", ref)
			return renderFacility(deps, raw)
		},
	}

	f := cmd.Flags()
	f.StringVar(&name, "name", "", "New name")
	f.StringVar(&status, "status", "", "New state: "+strings.Join(facilityStatuses(), ", "))
	f.StringVar(&tenantID, "tenant-facility-id", "", "New tenantFacilityId")
	f.StringVar(&typ, "type", "",
		"The facility's type, required only with --if-version: "+strings.Join(facilityTypes(), " or "))
	version.register(f)

	registerEnumCompletion(cmd, "status", facilityStatuses())
	registerEnumCompletion(cmd, "type", facilityTypes())

	return cmd
}

// facilityChanges collects the fields the user asked to change.
//
// A patch with nothing in it is refused rather than sent: the API would answer
// 200 and change nothing, which looks exactly like success and is how a broken
// script goes unnoticed for a week.
func facilityChanges(cmd *cobra.Command, name, status, tenantID string) (entityDoc, error) {
	changes := entityDoc{}

	if cmd.Flags().Changed("name") {
		changes["name"] = name
	}
	if cmd.Flags().Changed("tenant-facility-id") {
		changes["tenantFacilityId"] = tenantID
	}
	if cmd.Flags().Changed("status") {
		v, err := enumValue("status", status, facilityStatuses())
		if err != nil {
			return nil, err
		}
		changes["status"] = v
	}

	if len(changes) == 0 {
		return nil, exitcode.UsageError{Err: fmt.Errorf(
			"there is nothing to patch: pass --name, --status or --tenant-facility-id")}
	}
	return changes, nil
}
