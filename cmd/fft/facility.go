package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/client"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/output"
)

const facilityLong = `Manage the facilities of your tenant.

A facility is a place that fulfills orders: a store, a warehouse, or a supplier
you do not operate yourself. Facilities are polymorphic — every one is either a
MANAGED_FACILITY (you run it, it has picking times, capacity and services) or a
SUPPLIER (someone else runs it) — and the type is fixed at creation.

Every command that takes an <id> accepts either the platform's UUID or your own
tenantFacilityId: fft wraps the latter as
urn:fft:facility:tenantFacilityId:<id>, which every facility endpoint accepts.
So 'fft facility get BER-01' works without you ever looking up a UUID.

Reading is cheap and writing is versioned. Every mutation reads the facility
first to learn its current version and sends that version back — the API rejects
a write that carries a stale one. Pass --if-version to skip that read when you
already know the version; you will get a clean 409 instead of a silent
overwrite if you were wrong.`

func newFacilityCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "facility",
		Aliases: []string{"facilities"},
		Short:   "Manage facilities",
		Long:    facilityLong,
		Args:    usageArgs(cobra.NoArgs),

		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}

	cmd.AddCommand(
		newFacilityListCmd(deps),
		newFacilityGetCmd(deps),
		newFacilityCreateCmd(deps),
		newFacilityUpdateCmd(deps),
		newFacilityPatchCmd(deps),
		newFacilityDeleteCmd(deps),
		newFacilityCoordinatesCmd(deps),
		newFacilitySearchCmd(deps),
	)

	return cmd
}

// getFacility reads one facility, as the API wrote it.
func getFacility(ctx context.Context, c *client.Client, ref string) ([]byte, error) {
	res, err := c.Do(ctx, fmt.Sprintf("get facility %s", ref), func(ctx context.Context) (*http.Response, error) {
		return c.API().GetFacility(ctx, ref)
	})
	if err != nil {
		return nil, err
	}
	return res.Body, nil
}

// getFacilityDoc reads one facility and its version together, which is what every
// read-then-write mutation starts with.
func getFacilityDoc(ctx context.Context, c *client.Client, ref string) (entityDoc, int, error) {
	raw, err := getFacility(ctx, c, ref)
	if err != nil {
		return nil, 0, err
	}

	doc, err := decodeDoc(raw, "the facility")
	if err != nil {
		return nil, 0, err
	}

	version, err := docVersion(doc, "facility")
	if err != nil {
		return nil, 0, err
	}
	return doc, version, nil
}

// resolveFacilityID turns whatever the user typed into the facility's *platform
// id*, reading the facility to find it if it has to.
//
// Most facility parameters accept the URN form of a tenantFacilityId, so most
// callers need nothing but [client.FacilityRef]. The exceptions are the query
// parameters that take a facilityRef — GET /api/stocks/summaries?facilityRefs= is
// the one fft uses — and they do NOT resolve a URN. Confirmed against the live
// tenant (2026-07-12): summaries for facility 0090000020 answer 760 by UUID and
// **0 by URN**. Not a 400: a cheerful, empty 200, which reads as "this facility
// has no stock" rather than as "you asked the wrong question".
//
// So a value that is already a UUID passes through untouched, and anything else
// costs one GET to turn into one. A request fft cannot spell correctly is worth
// more than a wrong answer delivered quickly.
func resolveFacilityID(ctx context.Context, c *client.Client, value string) (string, error) {
	// Already a platform id (or a ref the caller built themselves): nothing to look
	// up, and looking it up anyway would cost a request to learn what we were told.
	if key, id := client.FacilitySelector(value); key == client.KeyFacilityRef {
		return id, nil
	}

	facility, err := lookupFacility(ctx, c, value)
	if err != nil {
		return "", err
	}
	return facility.ID, nil
}

// facilityIdentity is who a facility is: enough to address it, and enough to
// recognise it in a question that is about to destroy something.
type facilityIdentity struct {
	// ID is the platform id.
	ID string

	// Name is what a human calls it. It is why a destructive command looks the
	// facility up even when it was given a UUID and needs no resolving: "Purge all
	// 4813 listings of facility 8f14e45f-ceea-467a-9575-25a1b5c8b3a1?" is not a
	// question anybody can answer safely, and "of facility Berlin Mitte (BER-01)"
	// is.
	Name string

	// Typed is the value the user actually gave, echoed back so that the question
	// uses their words and not fft's.
	Typed string
}

// String names the facility the way a prompt should: by its name, with the id the
// user typed alongside it.
func (f facilityIdentity) String() string {
	switch {
	case f.Name != "" && f.Typed != "":
		return fmt.Sprintf("%s (%s)", f.Name, f.Typed)
	case f.Name != "":
		return f.Name
	default:
		return f.Typed
	}
}

// lookupFacility reads the facility named by value, which may be a UUID or the
// tenant's own id.
func lookupFacility(ctx context.Context, c *client.Client, value string) (facilityIdentity, error) {
	value = strings.TrimSpace(value)

	ref := client.FacilityRef(value)
	if ref == "" {
		return facilityIdentity{}, exitcode.UsageError{Err: fmt.Errorf("--facility cannot be empty")}
	}

	raw, err := getFacility(ctx, c, ref)
	if err != nil {
		return facilityIdentity{}, fmt.Errorf("resolve facility %s: %w", value, err)
	}

	doc, err := decodeDoc(raw, "the facility")
	if err != nil {
		return facilityIdentity{}, err
	}

	id := docString(doc, "id")
	if id == "" {
		return facilityIdentity{}, fmt.Errorf("resolve facility %s: the API returned it without an id", value)
	}

	return facilityIdentity{ID: id, Name: docString(doc, "name"), Typed: value}, nil
}

// facilityBody reads a facility request body and checks the one thing the API
// cannot forgive: the `type` discriminator. Without it the server answers with a
// 400 that does not say which of the two shapes it failed to match.
func facilityBody(deps *Deps, path string) (entityDoc, error) {
	raw, err := readBody(deps, path)
	if err != nil {
		return nil, err
	}

	doc, err := decodeDoc(raw, path)
	if err != nil {
		return nil, exitcode.UsageError{Err: err}
	}

	if err := checkFacilityType(docString(doc, "type")); err != nil {
		return nil, exitcode.UsageError{Err: fmt.Errorf("%s: %w", path, err)}
	}
	return doc, nil
}

// The facility types. A facility is one or the other, and it is decided at
// creation: the API has no action that turns a supplier into a managed facility.
const (
	typeManagedFacility = "MANAGED_FACILITY"
	typeSupplier        = "SUPPLIER"
)

func facilityTypes() []string { return []string{typeManagedFacility, typeSupplier} }

// The facility states.
const (
	statusOnline    = "ONLINE"
	statusSuspended = "SUSPENDED"
	statusOffline   = "OFFLINE"
)

func facilityStatuses() []string { return []string{statusOnline, statusSuspended, statusOffline} }

// checkFacilityType refuses a body whose discriminator is missing or unknown,
// naming the two values that work.
func checkFacilityType(v string) error {
	switch v {
	case "":
		return fmt.Errorf("the body has no %q field: a facility is a %s or a %s, and the API needs to be told which",
			"type", typeManagedFacility, typeSupplier)
	case typeManagedFacility, typeSupplier:
		return nil
	default:
		return fmt.Errorf("unknown facility type %q: want %s", v, strings.Join(facilityTypes(), " or "))
	}
}

// facilityView is the table's model of a facility: the handful of fields a human
// scanning a list actually reads, flattened out of a union whose generated form
// is two dozen optional pointers.
//
// It is also the reason [client.Search] is generic in its result type. The
// generated api.Facility has no Id field — the swagger omits it, though the API
// returns one on every facility — so a table built from the generated model
// could not show the id at all.
//
// This is the deliberate half of the output split: the table is fft's summary,
// while -o json is the API's own JSON with every field intact.
type facilityView struct {
	ID               string `json:"id"`
	TenantFacilityID string `json:"tenantFacilityId"`
	Name             string `json:"name"`
	Type             string `json:"type"`
	Status           string `json:"status"`
	LocationType     string `json:"locationType"`
	Version          int64  `json:"version"`

	// Address is absent on a supplier that has none, which is why the table falls
	// back to a dash rather than to an empty column.
	Address struct {
		City    string `json:"city"`
		Country string `json:"country"`
	} `json:"address"`
}

// facilityList is the view `fft facility list` and `fft facility search` render.
func facilityList() listView {
	return listView{Noun: "facilities", Rows: facilityRows}
}

// renderFacility renders one facility: the API's own JSON object under -o json,
// a one-row table otherwise.
//
// It is deliberately not renderFacilities with a slice of one. `fft facility get
// x -o json | jq .name` must see the object the API sent; wrapping it in an array
// would force every script to index into a list that can only ever hold one
// thing.
func renderFacility(deps *Deps, raw []byte) error {
	// A 2xx with no body is a legitimate answer to a mutation. There is nothing to
	// render, the notice on stderr has already said what happened, and inventing a
	// row for it would be worse than printing none.
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}

	rows, err := facilityRows(deps.Printer.Style(), []json.RawMessage{raw})
	if err != nil {
		return err
	}
	return deps.Printer.RenderRaw(rows, raw)
}

var facilityHeaders = []string{"ID", "TENANT ID", "NAME", "TYPE", "STATUS", "LOCATION", "CITY", "VERSION"}

// facilityRows renders both kinds of facility in one table. A supplier has no
// locationType and often no address; a managed facility has both. Rather than
// two tables, or a table of the intersection, the columns are the union and an
// absent value is a dash — so the two sort, diff and grep alongside each other.
//
// A facility fft cannot parse is a bug worth reporting, not a row worth skipping.
func facilityRows(style output.Style, items []json.RawMessage) (output.Rows, error) {
	rows := make([][]string, 0, len(items))

	for i, item := range items {
		var v facilityView
		if err := json.Unmarshal(item, &v); err != nil {
			return output.Rows{}, fmt.Errorf("decode facility %d of %d: %w", i+1, len(items), err)
		}

		rows = append(rows, []string{
			field(style, v.ID),
			field(style, v.TenantFacilityID),
			field(style, v.Name),
			field(style, v.Type),
			facilityStatusCell(style, v.Status),
			field(style, v.LocationType),
			field(style, v.Address.City),
			fmt.Sprintf("%d", v.Version),
		})
	}
	return output.Rows{Headers: facilityHeaders, Rows: rows}, nil
}

// facilityStatusCell colours the one column a reader scans for: a facility that
// is not ONLINE is not taking work.
func facilityStatusCell(style output.Style, status string) string {
	switch status {
	case statusOnline:
		return style.Green(status)
	case statusSuspended:
		return style.Yellow(status)
	case statusOffline:
		return style.Red(status)
	default:
		return field(style, status)
	}
}
