package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/api"
	"github.com/Joessst-Dev/fft-cli/internal/client"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/output"
)

const listingLong = `Manage the listings of your tenant.

A listing is the CATALOG entry for one article at one facility: whether that
facility offers the article at all (status ACTIVE or INACTIVE), what it is
called, what it costs, what it weighs.

A listing is NOT the quantity. The quantity is a stock — 'fft stock'. The two
were split in 2023 and are separate entities: a listing can be ACTIVE with no
stock at all (nothing to sell yet), and a stock can exist for an article whose
listing is INACTIVE (goods on the shelf, not offered). If you are asking "how
many are there", you want 'fft stock'.

Listings are addressed by your own tenantArticleId, not by a platform UUID —
alone among the entities in this API. --facility takes either the platform's
UUID or your own tenantFacilityId.

  fft listing list  --facility BER-01
  fft listing get   --facility BER-01 4711
  fft listing patch --facility BER-01 4711 --status INACTIVE`

// Why there is no `fft listing partialstocks`, and why there must never be one.
//
// The API still exposes /api/facilities/{fid}/listings/{taid}/partialstocks
// (swagger:9633-9871), and Listing still carries `partialStocks`
// (swagger:47180) and `stockinformation` (swagger:47207). All of it is
// deprecated. The Listing/Stock split in 2023 moved quantity out of the catalog
// entry and into /api/stocks, and these fields are the vestige of the model that
// came before — they are not maintained, they do not agree with /api/stocks, and
// a user who reads a quantity off a listing is reading a number that has been
// wrong since 2023.
//
// So fft does not surface them, and a future contributor looking at the swagger
// and thinking "there is an endpoint here that fft is missing" should read this
// paragraph instead of implementing it. The stock commands are the answer.

// The listing states. A listing is offered, or it is not; there is no third
// thing, and there is no transition endpoint — it is a field on the entity.
const (
	listingActive   = "ACTIVE"
	listingInactive = "INACTIVE"
)

func listingStatuses() []string { return []string{listingActive, listingInactive} }

func newListingCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "listing",
		Aliases: []string{"listings"},
		Short:   "Manage listings (the article-at-facility catalog entry)",
		Long:    listingLong,
		Args:    usageArgs(cobra.NoArgs),

		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}

	cmd.AddCommand(
		newListingListCmd(deps),
		newListingGetCmd(deps),
		newListingSetCmd(deps),
		newListingPatchCmd(deps),
		newListingDeleteCmd(deps),
		newListingPurgeCmd(deps),
		newListingUpsertCmd(deps),
		newListingSearchCmd(deps),
	)

	return cmd
}

// facilityFlag is --facility, which every per-facility listing command needs.
//
// The value is passed through [client.FacilityRef], so a tenantFacilityId is
// wrapped as urn:fft:facility:tenantFacilityId:<id> — the form every facility
// path parameter accepts — and a platform UUID passes through untouched.
func requireFacility(cmd *cobra.Command, id string) (string, error) {
	if err := requireFlag(cmd, "facility"); err != nil {
		return "", err
	}

	ref := client.FacilityRef(id)
	if ref == "" {
		return "", exitcode.UsageError{Err: fmt.Errorf("--facility cannot be empty")}
	}
	return ref, nil
}

// getListing reads one listing, as the API wrote it.
//
// The path is /api/facilities/{facilityId}/listings/{tenantArticleId}: a listing
// is addressed by the tenant's own article id, not by a platform UUID. Alone in
// this API. Hence the separate helper, rather than the generic "get by id" every
// other entity shares.
func getListing(ctx context.Context, c *client.Client, facility, tenantArticleID string) ([]byte, error) {
	op := fmt.Sprintf("get listing %s of facility %s", tenantArticleID, facility)

	res, err := c.Do(ctx, op, func(ctx context.Context) (*http.Response, error) {
		return c.API().GetListing(ctx, facility, tenantArticleID, &api.GetListingParams{})
	})
	if err != nil {
		return nil, err
	}
	return res.Body, nil
}

// getListingDoc reads one listing and its version together, which is what a
// read-then-write mutation starts with.
func getListingDoc(ctx context.Context, c *client.Client, facility, tenantArticleID string) (entityDoc, int, error) {
	raw, err := getListing(ctx, c, facility, tenantArticleID)
	if err != nil {
		return nil, 0, err
	}

	doc, err := decodeDoc(raw, "the listing")
	if err != nil {
		return nil, 0, err
	}

	version, err := docVersion(doc, "listing")
	if err != nil {
		return nil, 0, err
	}
	return doc, version, nil
}

// listingView is the table's model of a listing: what a human scanning a catalog
// reads. Price and title are optional on the wire, so both fall back to a dash.
type listingView struct {
	TenantArticleID string  `json:"tenantArticleId"`
	FacilityID      string  `json:"facilityId"`
	Title           string  `json:"title"`
	Status          string  `json:"status"`
	Price           float64 `json:"price"`
	Version         int64   `json:"version"`
}

// listingList is the view `fft listing list` and `fft listing search` render.
func listingList() listView {
	return listView{Noun: "listings", Rows: listingRows}
}

var listingHeaders = []string{"TENANT ARTICLE ID", "FACILITY", "TITLE", "STATUS", "PRICE", "VERSION"}

// listingRows renders the catalog table. A listing fft cannot parse is a bug
// worth reporting, not a row worth skipping.
func listingRows(style output.Style, items []json.RawMessage) (output.Rows, error) {
	rows := make([][]string, 0, len(items))

	for i, item := range items {
		var v listingView
		if err := json.Unmarshal(item, &v); err != nil {
			return output.Rows{}, fmt.Errorf("decode listing %d of %d: %w", i+1, len(items), err)
		}

		rows = append(rows, []string{
			field(style, v.TenantArticleID),
			field(style, v.FacilityID),
			field(style, v.Title),
			listingStatusCell(style, v.Status),
			priceCell(style, v.Price),
			fmt.Sprintf("%d", v.Version),
		})
	}
	return output.Rows{Headers: listingHeaders, Rows: rows}, nil
}

// listingStatusCell colours the column a reader scans for: an INACTIVE listing is
// not being offered, whatever its stock says.
func listingStatusCell(style output.Style, status string) string {
	switch status {
	case listingActive:
		return style.Green(status)
	case listingInactive:
		return style.Yellow(status)
	default:
		return field(style, status)
	}
}

// priceCell renders a price. Zero is a legitimate price (a giveaway, a bundled
// item) but it is also what an absent field decodes to, and the two cannot be
// told apart once the number is a float64 — so 0 renders as 0 and the table does
// not pretend to know which it was.
func priceCell(style output.Style, price float64) string {
	if price == 0 {
		return field(style, "")
	}
	return fmt.Sprintf("%.2f", price)
}

// renderListing renders one listing: the API's own JSON object under -o json, a
// one-row table otherwise. A 2xx with no body renders nothing — the notice on
// stderr has already said what happened.
func renderListing(deps *Deps, raw []byte) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}

	rows, err := listingRows(deps.Printer.Style(), []json.RawMessage{raw})
	if err != nil {
		return err
	}
	return deps.Printer.RenderRaw(rows, raw)
}
