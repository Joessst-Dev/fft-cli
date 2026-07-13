package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/client"
	"github.com/Joessst-Dev/fft-cli/internal/output"
)

const stockLong = `Manage the stocks of your tenant.

A stock is a QUANTITY: how many of one article are at one location of one
facility. It is not the catalog entry — that is a listing, 'fft listing'. The
two were split in 2023 and are separate entities: an article can be listed as
ACTIVE with no stock at all, and can have stock while its listing is INACTIVE.
If you are asking "do we offer this", you want 'fft listing'.

  fft stock list --facility BER-01
  fft stock create --tenant-article-id 4711 --facility BER-01 --value 12
  fft stock summary --facility BER-01

Stocks are versioned. Every mutation reads the stock first to learn its current
version and sends that version back; --if-version skips the read when you
already know it.

A stock is created against exactly one facility, named as one of "facility",
"facilityRef" or "tenantFacilityId". The API marks none of the three as
required, so a body with none of them — or with two — fails server-side with an
error that does not say which. fft checks first.`

func newStockCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "stock",
		Aliases: []string{"stocks"},
		Short:   "Manage stocks (the quantity of an article at a facility)",
		Long:    stockLong,
		Args:    usageArgs(cobra.NoArgs),

		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}

	cmd.AddCommand(
		newStockListCmd(deps),
		newStockGetCmd(deps),
		newStockCreateCmd(deps),
		newStockUpdateCmd(deps),
		newStockDeleteCmd(deps),
		newStockUpsertCmd(deps),
		newStockActionsCmd(deps),
		newStockSummaryCmd(deps),
		newStockSearchCmd(deps),
	)

	return cmd
}

// getStock reads one stock, as the API wrote it. A stock *is* addressed by its
// platform id — unlike a listing.
func getStock(ctx context.Context, c *client.Client, id string) ([]byte, error) {
	res, err := c.Do(ctx, "get stock "+id, func(ctx context.Context) (*http.Response, error) {
		return c.API().GetStock(ctx, id)
	})
	if err != nil {
		return nil, err
	}
	return res.Body, nil
}

// getStockDoc reads one stock and its version together, which is what a
// read-then-write mutation starts with.
func getStockDoc(ctx context.Context, c *client.Client, id string) (entityDoc, int, error) {
	raw, err := getStock(ctx, c, id)
	if err != nil {
		return nil, 0, err
	}

	doc, err := decodeDoc(raw, "the stock")
	if err != nil {
		return nil, 0, err
	}

	version, err := docVersion(doc, "stock")
	if err != nil {
		return nil, 0, err
	}
	return doc, version, nil
}

// stockView is the table's model of a stock: the quantities a human is actually
// asking about, plus enough identity to act on the row.
type stockView struct {
	ID              string  `json:"id"`
	TenantArticleID string  `json:"tenantArticleId"`
	FacilityRef     string  `json:"facilityRef"`
	LocationRef     string  `json:"locationRef"`
	Value           int64   `json:"value"`
	Reserved        float64 `json:"reserved"`
	Available       float64 `json:"available"`
	Version         int64   `json:"version"`
}

// stockList is the view `fft stock list` and `fft stock search` render.
func stockList() listView {
	return listView{Noun: "stocks", Rows: stockRows}
}

var stockHeaders = []string{"ID", "TENANT ARTICLE ID", "FACILITY", "LOCATION", "VALUE", "RESERVED", "AVAILABLE", "VERSION"}

// stockRows renders the quantity table. VALUE is what is physically there,
// RESERVED is what is already promised to an order, and AVAILABLE is the
// difference the API computed — all three are shown, because a user who sees only
// one of them will draw the wrong conclusion from it.
func stockRows(style output.Style, items []json.RawMessage) (output.Rows, error) {
	rows := make([][]string, 0, len(items))

	for i, item := range items {
		var v stockView
		if err := json.Unmarshal(item, &v); err != nil {
			return output.Rows{}, fmt.Errorf("decode stock %d of %d: %w", i+1, len(items), err)
		}

		rows = append(rows, []string{
			field(style, v.ID),
			field(style, v.TenantArticleID),
			field(style, v.FacilityRef),
			field(style, v.LocationRef),
			fmt.Sprintf("%d", v.Value),
			quantity(v.Reserved),
			availableCell(style, v.Available),
			fmt.Sprintf("%d", v.Version),
		})
	}
	return output.Rows{Headers: stockHeaders, Rows: rows}, nil
}

// availableCell colours the number the reader came for: nothing available means
// nothing can be sold from this stock, whatever its value says.
func availableCell(style output.Style, available float64) string {
	if available <= 0 {
		return style.Red(quantity(available))
	}
	return style.Green(quantity(available))
}

// quantity renders a count. The API types these as `number` rather than integer,
// so a whole one is printed whole — "12" and not "12.0", which is what a reader
// scanning a column of counts expects.
func quantity(v float64) string {
	if v == float64(int64(v)) {
		return fmt.Sprintf("%d", int64(v))
	}
	return fmt.Sprintf("%.2f", v)
}

// renderStock renders one stock: the API's own JSON object under -o json, a
// one-row table otherwise. A 2xx with no body renders nothing.
func renderStock(deps *Deps, raw []byte) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}

	rows, err := stockRows(deps.Printer.Style(), []json.RawMessage{raw})
	if err != nil {
		return err
	}
	return deps.Printer.RenderRaw(rows, raw)
}
