package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

const listingSetLong = `Put a set of listings into one facility's catalog.

This is PUT /api/facilities/{id}/listings. It creates the listings that do not
exist and replaces the ones that do — for the articles named in the file, and
only those. Listings the file does not mention are left alone; this is not a
replacement of the whole catalog. (To empty a catalog, use 'fft listing purge'.)

  fft listing set --example > listings.json
  $EDITOR listings.json
  fft listing set --facility BER-01 --file listings.json

The API calls this endpoint legacy for one reason worth knowing: it is
all-or-nothing. If a single listing in the file is rejected, the whole PUT is
rejected. fft therefore does NOT split the file into chunks — doing so would
turn one atomic write into several, which is not what you asked for. To write
across facilities, or to write a catalog too large for one request, use
'fft listing upsert', which chunks on purpose and reports per item.

The answer is a per-item result table. Any FAILED item exits 8.

Only tenantArticleId is required per listing. --file - reads from stdin.`

// maxListingSet is the API's cap on one PUT (swagger:48115, maxItems: 500). It is
// refused here rather than chunked: the endpoint's contract is all-or-nothing, and
// silently splitting a file across three requests would quietly give the user
// three partial writes where they asked for one atomic one.
const maxListingSet = 500

// listingSetExample is a body that is valid as it stands. Only tenantArticleId is
// required (swagger:47653), but a catalog entry with no title is not one a human
// would recognise, so the example shows the fields that make a listing usable.
// listingSetExample is hand-written, not synthesized. The synthesized body for
// putFacilityListing carries a "version" (the schema's example is 42), which on a PUT
// of a fresh listing set is a 409 waiting to happen; and it shows one listing where
// the point of this endpoint is a set. See stockCreateExample for the general rule.
const listingSetExample = `{
  "listings": [
    {
      "tenantArticleId": "4711",
      "title": "Adidas Superstar",
      "status": "ACTIVE",
      "price": 89.95,
      "measurementUnitKey": "piece",
      "imageUrl": "https://example.com/images/4711.jpg"
    },
    {
      "tenantArticleId": "4712",
      "title": "Adidas Gazelle",
      "status": "INACTIVE",
      "price": 79.95
    }
  ]
}
`

func newListingSetCmd(deps *Deps) *cobra.Command {
	var (
		facility string
		file     string
		example  bool
	)

	cmd := &cobra.Command{
		Use:   "set --facility <id> --file <file>",
		Short: "Put a set of listings into a facility (PUT, all-or-nothing)",
		Long:  listingSetLong,
		Args:  usageArgs(cobra.NoArgs),

		Annotations: map[string]string{annotationOperationID: "putFacilityListing"},

		RunE: func(cmd *cobra.Command, _ []string) error {
			// --example is answered before anything else: it needs no project, no
			// credentials and no network, and a user reaching for it is usually a user
			// who has not set those up yet.
			if example {
				_, err := fmt.Fprint(cmd.OutOrStdout(), listingSetExample)
				return err
			}

			ref, err := requireFacility(cmd, facility)
			if err != nil {
				return err
			}
			if err := requireFlag(cmd, "file"); err != nil {
				return err
			}

			body, err := readBody(deps, file)
			if err != nil {
				return err
			}
			if err := checkListingSet(body, file); err != nil {
				return err
			}

			c, err := tenantClient(deps)
			if err != nil {
				return err
			}

			ctx, cancel := deps.Context(cmd)
			defer cancel()

			raw, err := sendDoc(ctx, c, "put the listings of facility "+ref, body,
				func(ctx context.Context, body io.Reader) (*http.Response, error) {
					return c.API().PutFacilityListingWithBody(ctx, ref, contentTypeJSON, body)
				})
			if err != nil {
				return err
			}

			results := listingSetResults(raw)

			// No results does not mean no listings. The PUT has landed by now, so an
			// answer fft could not read must not be rendered as "No listings found." —
			// which is the exact opposite of what happened, and is what renderBulk would
			// say for an empty slice.
			if len(results) == 0 {
				deps.Printer.Warnf(
					"The listings were written, but the API's per-item answer could not be read. "+
						"Run 'fft listing list --facility %s' to see them.", ref)
				return nil
			}

			return renderBulk(deps, "listings", results)
		},
	}

	f := cmd.Flags()
	f.StringVar(&facility, "facility", "", "The facility, by tenantFacilityId or platform UUID (required)")
	f.StringVar(&file, "file", "", `JSON file holding {"listings": [...]} ('-' for stdin)`)
	f.BoolVar(&example, "example", false, "Print a sample request body and exit")
	cmd.MarkFlagsMutuallyExclusive("file", "example")

	return cmd
}

// listingSetBody is the envelope PUT /api/facilities/{id}/listings expects. It is
// declared only to *check* the file: the bytes the user wrote are what is sent, so
// a field fft has no model for still reaches the API.
type listingSetBody struct {
	Listings []struct {
		TenantArticleID string `json:"tenantArticleId"`
	} `json:"listings"`
}

// checkListingSet refuses a body the API would reject.
//
// The schema requires the envelope, a tenantArticleId on every entry, and at most
// 500 of them (swagger:48110, 47653). The API's answer to a bare array, or to an
// entry without an article id, is a 400 that does not say which entry it meant —
// and because the PUT is all-or-nothing, that 400 means none of the listings
// landed and the user is left to find the bad one by hand.
func checkListingSet(raw []byte, path string) error {
	var body listingSetBody
	if err := json.Unmarshal(raw, &body); err != nil {
		return exitcode.UsageError{Err: fmt.Errorf(
			`%s is not a listing set: it must be an object like {"listings": [...]}: %w`, path, err)}
	}

	switch {
	case len(body.Listings) == 0:
		return exitcode.UsageError{Err: fmt.Errorf(
			`%s holds no listings: the body must be {"listings": [...]} with at least one entry — run 'fft listing set --example' for one to start from`,
			path)}

	case len(body.Listings) > maxListingSet:
		return exitcode.UsageError{Err: fmt.Errorf(
			"%s holds %d listings, and this endpoint accepts at most %d in one request; use 'fft listing upsert', which chunks",
			path, len(body.Listings), maxListingSet)}
	}

	for i, l := range body.Listings {
		if l.TenantArticleID == "" {
			return exitcode.UsageError{Err: fmt.Errorf(
				"%s: listing %d of %d has no tenantArticleId, and the API requires one on every listing",
				path, i+1, len(body.Listings))}
		}
	}

	return nil
}

// listingBulkOperationResult is one entry of the PUT's answer (swagger:47271):
// the listing as it now stands, and what happened to it. FAILED is one of the
// four statuses the API itself can report here.
type listingBulkOperationResult struct {
	Status  string `json:"status"`
	Listing struct {
		TenantArticleID string `json:"tenantArticleId"`
		FacilityID      string `json:"facilityId"`
	} `json:"listing"`
}

// listingSetResults reads the API's per-item answer.
//
// A body it cannot parse yields no results rather than an error. The PUT has
// already landed by this point, and failing on the *answer* would tell the user to
// re-run a write that succeeded — which for a catalog import means doing it twice.
func listingSetResults(raw []byte) []bulkResult {
	var items []listingBulkOperationResult
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil
	}

	results := make([]bulkResult, 0, len(items))
	for _, item := range items {
		results = append(results, bulkResult{
			Key:      item.Listing.TenantArticleID,
			Facility: item.Listing.FacilityID,
			Status:   item.Status,
		})
	}
	return results
}
