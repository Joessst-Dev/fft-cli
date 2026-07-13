package main

import (
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

const stockSummaryLong = `Show the accumulated stock of each article.

This is GET /api/stocks/summaries: one row per article, with its stock added up
across the facilities you asked about — rather than one row per stock, which is
what 'fft stock list' gives you.

  fft stock summary --facility BER-01
  fft stock summary --tenant-article-id 4711 --tenant-article-id 4712
  fft stock summary --facility BER-01 -o json | jq '.[] | select(.details.availableOnStock == 0)'

ON HAND is what is physically there, RESERVED is what is promised to orders,
AVAILABLE is what is left to sell, and SAFETY is the buffer held back from
routing.

This endpoint has its own page size — default 25, maximum 100 — which is NOT the
search API's (default 20, maximum 250). They are different endpoints and the
numbers are not interchangeable.`

// The summaries endpoint's page size (swagger:27075). It is deliberately not
// unified with the search API's 20/250: a user who set --size 100 here and got 250
// somewhere else would be right to file a bug.
const (
	minSummarySize     = 1
	maxSummarySize     = 100
	defaultSummarySize = 25
)

func newStockSummaryCmd(deps *Deps) *cobra.Command {
	var (
		facilities []string
		articles   []string
		size       int
		allowStale bool
	)

	cmd := &cobra.Command{
		Use:     "summary",
		Short:   "Show the accumulated stock of each article",
		Long:    stockSummaryLong,
		Args:    usageArgs(cobra.NoArgs),
		Aliases: []string{"summaries"},

		Annotations: map[string]string{annotationOperationID: "getStockSummaries"},

		RunE: func(cmd *cobra.Command, _ []string) error {
			params := &api.GetStockSummariesParams{}

			// Guarded on Changed(), not on the value: --size 0 is out of range and must
			// fail like --size 500 does, rather than falling through as "unset" and
			// silently becoming the API's default of 25.
			if cmd.Flags().Changed("size") {
				if size < minSummarySize || size > maxSummarySize {
					return exitcode.UsageError{Err: fmt.Errorf(
						"--size must be between %d and %d on this endpoint, and %d is not (the search API's limit is different)",
						minSummarySize, maxSummarySize, size)}
				}
				params.Size = ptr(float32(size))
			}

			if len(articles) > 0 {
				params.TenantArticleIds = &articles
			}
			if cmd.Flags().Changed("allow-stale") {
				params.AllowStale = &allowStale
			}

			c, err := tenantClient(deps)
			if err != nil {
				return err
			}

			ctx, cancel := deps.Context(cmd)
			defer cancel()

			// facilityRefs wants platform ids, and — unlike every facility *path*
			// parameter — it does not resolve the URN form of a tenantFacilityId. It
			// answers a URN with an empty 200 rather than an error, so a --facility that
			// works everywhere else in fft would quietly report that the store has no
			// stock. Each one is therefore resolved to its id first.
			if len(facilities) > 0 {
				refs := make([]string, 0, len(facilities))
				for _, f := range facilities {
					id, err := resolveFacilityID(ctx, c, f)
					if err != nil {
						return err
					}
					refs = append(refs, id)
				}
				params.FacilityRefs = &refs
			}

			raw, err := getStockSummaries(ctx, c, params)
			if err != nil {
				return err
			}

			return renderStockSummaries(deps, raw)
		},
	}

	f := cmd.Flags()
	f.StringSliceVar(&facilities, "facility", nil,
		"Only these facilities, by tenantFacilityId or platform UUID (repeatable)")
	f.StringSliceVar(&articles, "tenant-article-id", nil, "Only these articles (repeatable)")
	f.IntVar(&size, "size", 0,
		fmt.Sprintf("Articles per page, %d–%d (default %d)", minSummarySize, maxSummarySize, defaultSummarySize))
	f.BoolVar(&allowStale, "allow-stale", false,
		"Let the API answer from a cache: faster, and possibly out of date")

	return cmd
}

func getStockSummaries(ctx context.Context, c *client.Client, params *api.GetStockSummariesParams) ([]byte, error) {
	res, err := c.Do(ctx, "get the stock summaries", func(ctx context.Context) (*http.Response, error) {
		return c.API().GetStockSummaries(ctx, params)
	})
	if err != nil {
		return nil, err
	}
	return res.Body, nil
}

// stockSummaryView is the table's model of one article's accumulated stock.
type stockSummaryView struct {
	Article struct {
		TenantArticleID string `json:"tenantArticleId"`
		Title           string `json:"title"`
	} `json:"article"`

	Details struct {
		StockOnHand         float64 `json:"stockOnHand"`
		Reserved            float64 `json:"reserved"`
		AvailableOnStock    float64 `json:"availableOnStock"`
		AvailableForPicking float64 `json:"availableForPicking"`
		SafetyStock         float64 `json:"safetyStock"`
	} `json:"details"`

	IncludedFacilityRefs []string `json:"includedFacilityRefs"`
}

var summaryHeaders = []string{"TENANT ARTICLE ID", "TITLE", "ON HAND", "RESERVED", "AVAILABLE", "PICKABLE", "SAFETY", "FACILITIES"}

// renderStockSummaries keeps the output contract: -o json is the API's own array
// of summaries, the table is fft's view of it, and the total goes to stderr.
//
// Unlike a search, `total` is required on this endpoint's envelope — so it is
// always reported, and there is no absent-versus-zero question to answer.
func renderStockSummaries(deps *Deps, raw []byte) error {
	var envelope struct {
		StockSummaries []json.RawMessage `json:"stockSummaries"`
		Total          *int              `json:"total"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("decode the stock summaries: %w", err)
	}

	shown := len(envelope.StockSummaries)

	if envelope.Total != nil {
		deps.Printer.Notef("Total: %d", *envelope.Total)

		// Every other list command says when it is showing you part of an answer. This
		// one has no --all, because the endpoint pages on a startAfterId cursor rather
		// than the search API's, so the least it can do is not let a total of 383,484
		// sit above a table of 25 rows without a word.
		if shown < *envelope.Total && deps.Printer.Format() == output.Table {
			deps.Printer.Notef("Showing %d of %d articles. Raise --size (maximum %d) to see more.",
				shown, *envelope.Total, maxSummarySize)
		}
	}

	if shown == 0 {
		return deps.Printer.Empty("stock summaries")
	}

	rows, err := summaryRows(deps.Printer.Style(), envelope.StockSummaries)
	if err != nil {
		return err
	}

	// The array of summaries, not the envelope: the envelope's `total` is metadata
	// and has already gone to stderr, and a script piping this into jq wants the
	// same array shape every other list command emits.
	items, err := json.Marshal(envelope.StockSummaries)
	if err != nil {
		return fmt.Errorf("encode the stock summaries: %w", err)
	}

	return deps.Printer.RenderRaw(rows, items)
}

func summaryRows(style output.Style, items []json.RawMessage) (output.Rows, error) {
	rows := make([][]string, 0, len(items))

	for i, item := range items {
		var v stockSummaryView
		if err := json.Unmarshal(item, &v); err != nil {
			return output.Rows{}, fmt.Errorf("decode stock summary %d of %d: %w", i+1, len(items), err)
		}

		rows = append(rows, []string{
			field(style, v.Article.TenantArticleID),
			field(style, v.Article.Title),
			quantity(v.Details.StockOnHand),
			quantity(v.Details.Reserved),
			availableCell(style, v.Details.AvailableOnStock),
			quantity(v.Details.AvailableForPicking),
			quantity(v.Details.SafetyStock),
			fmt.Sprintf("%d", len(v.IncludedFacilityRefs)),
		})
	}
	return output.Rows{Headers: summaryHeaders, Rows: rows}, nil
}
