package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/client"
)

const listingPurgeLong = `Delete EVERY listing of a facility.

This is DELETE /api/facilities/{id}/listings, and it does exactly what it says:
the facility's entire catalog is removed. There is no undo, and there is no
partial form — you cannot purge "the inactive ones".

Because the blast radius is the whole facility, purge is its own verb. It never
shares 'delete' with the single-listing case: a command that wipes a catalog
should not be one forgotten argument away from one that removes a single
article.

Before it asks, fft counts what it is about to destroy and shows you the
facility and the number:

  Purge all 4813 listings of facility urn:fft:facility:tenantFacilityId:BER-01?

--yes bypasses the question. On a non-interactive terminal without --yes, fft
refuses (exit 2) rather than assuming yes: a prompt nobody can see is not
consent.

Stock is a separate entity and is not touched — see 'fft stock'.`

func newListingPurgeCmd(deps *Deps) *cobra.Command {
	var facility string

	cmd := &cobra.Command{
		Use:   "purge --facility <id>",
		Short: "Delete every listing of a facility (destructive)",
		Long:  listingPurgeLong,
		Args:  usageArgs(cobra.NoArgs),

		Annotations: map[string]string{annotationOperationID: "deleteListingsOfFacility"},

		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireFlag(cmd, "facility"); err != nil {
				return err
			}

			c, err := tenantClient(deps)
			if err != nil {
				return err
			}

			ctx, cancel := deps.Context(cmd)
			defer cancel()

			// The facility is read in full, even when --facility was already a UUID and
			// needed no resolving. Two things come out of it, and a purge needs both:
			//
			//   - the platform id, because the DELETE addresses the facility by a path
			//     parameter (which takes the URN) while the count that decides what the
			//     DELETE is *about* is a search (which does not — it matches a URN against
			//     nothing and answers 200). Deriving both spellings from one resolved id is
			//     what makes it certain that the number in the question describes the set
			//     that is about to be destroyed.
			//
			//   - the facility's *name*, because "Purge all 4813 listings of facility
			//     8f14e45f-ceea-467a-9575-25a1b5c8b3a1?" is not a question anyone can answer
			//     safely, and "of facility Berlin Mitte (BER-01)?" is.
			target, err := lookupFacility(ctx, c, facility)
			if err != nil {
				return err
			}
			ref := client.FacilityRef(target.ID)

			// The count is fetched *before* the question, so that the question can name
			// it. "Delete all listings?" is a question a user answers yes to without
			// thinking; "Purge all 4813 listings of Berlin Mitte?" is one they read.
			//
			// It is fetched even under --yes, because it is also what the notice
			// afterwards reports, and because a purge that says how much it destroyed is
			// the only record the user will have.
			count, err := countListings(ctx, c, target.ID)
			if err != nil {
				return err
			}

			if count == 0 {
				deps.Printer.Notef("Facility %s has no listings; nothing to purge.", target)
				return nil
			}

			ok, err := confirmDestructive(deps, fmt.Sprintf(
				"Purge all %d listings of facility %s? This cannot be undone.", count, target))
			if err != nil {
				return err
			}
			if !ok {
				deps.Printer.Notef("Aborted; the %d listings of facility %s were not purged.", count, target)
				return nil
			}

			if _, err := c.Do(ctx, "purge the listings of facility "+ref,
				func(ctx context.Context) (*http.Response, error) {
					return c.API().DeleteListingsOfFacility(ctx, ref)
				}); err != nil {
				return err
			}

			// The number is the one from the count, taken a moment ago — the DELETE does
			// not report how many it removed. It is the honest figure to quote, and it is
			// the same set the DELETE addressed, but it is not a receipt.
			deps.Printer.Notef("Purged the listings of facility %s (%d at the time of the count).", target, count)
			return nil
		},
	}

	cmd.Flags().StringVar(&facility, "facility", "", "The facility, by tenantFacilityId or platform UUID (required)")

	return cmd
}

// countListings asks the API how many listings a facility has.
//
// It is a search for a single item with options.withTotal, so the API counts and
// fft transfers one listing rather than the whole catalog to find out. The total
// is a *int precisely because it is absent unless asked for — and here it was, so
// an absent one means the API did not answer the question it was asked, and
// purging on the strength of a number we do not have is not something to do
// quietly.
func countListings(ctx context.Context, c *client.Client, facilityID string) (int, error) {
	payload := client.ListingSearchPayload{
		Query: listingFacilityQuery(facilityID),
		Size:  ptr(1),
	}.WithTotal()

	page, err := client.Search(ctx, c, client.ListingSearch[json.RawMessage](), payload)
	if err != nil {
		return 0, fmt.Errorf("count the listings of facility %s: %w", facilityID, err)
	}
	if page.Total == nil {
		return 0, fmt.Errorf(
			"count the listings of facility %s: the API did not return a total, so fft cannot tell you what a purge would destroy",
			facilityID)
	}
	return *page.Total, nil
}
