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

const routingStrategyLong = `Manage routing strategies.

A routing strategy is the decision tree the engine walks to source an order: a root
node with fences that reject ineligible facilities and ratings that rank the rest,
then conditions that branch to further nodes. Strategies are drafted, edited and then
made live one at a time — only one is ever 'inUse'.

  fft routing strategy list
  fft routing strategy get <id>
  fft routing strategy activate <id>

'get -o json' prints the whole tree; the table is a one-line summary. 'evaluate'
dry-runs a strategy against an order and reserves nothing.`

func newRoutingStrategyCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "strategy",
		Aliases: []string{"strategies"},
		Short:   "Manage routing strategies",
		Long:    routingStrategyLong,
		Args:    usageArgs(cobra.NoArgs),

		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}

	cmd.AddCommand(
		newRoutingStrategyListCmd(deps),
		newRoutingStrategyGetCmd(deps),
		newRoutingStrategyCreateCmd(deps),
		newRoutingStrategyUpdateCmd(deps),
		newRoutingStrategyActivateCmd(deps),
		newRoutingStrategyEvaluateCmd(deps),
	)

	return cmd
}

// getStrategy reads one strategy, as the API wrote it.
func getStrategy(ctx context.Context, c *client.Client, id string) ([]byte, error) {
	res, err := c.Do(ctx, fmt.Sprintf("get routing strategy %s", id),
		func(ctx context.Context) (*http.Response, error) {
			return c.API().GetRoutingStrategy(ctx, id)
		})
	if err != nil {
		return nil, err
	}
	return res.Body, nil
}

// getStrategyDoc reads one strategy and its version together — the read half of the
// read-then-write update and activate.
func getStrategyDoc(ctx context.Context, c *client.Client, id string) (entityDoc, int, error) {
	raw, err := getStrategy(ctx, c, id)
	if err != nil {
		return nil, 0, err
	}

	doc, err := decodeDoc(raw, "the routing strategy")
	if err != nil {
		return nil, 0, err
	}

	version, err := docVersion(doc, "routing strategy")
	if err != nil {
		return nil, 0, err
	}
	return doc, version, nil
}

// routingStrategyView is the table's model of a strategy: the handful of fields a
// human scanning the list reads.
//
// It is hand-written because the generated model cannot be used. RoutingStrategy is
// an allOf-with-siblings (VersionedResource plus properties), which oapi-codegen
// collapses down to the bare VersionedResource — every field but created,
// lastModified and version is gone. See the note on entityDoc.
type routingStrategyView struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	NameLocalized map[string]string `json:"nameLocalized"`
	Revision      int64             `json:"revision"`
	InUse         bool              `json:"inUse"`
	Version       int64             `json:"version"`
}

// displayName is the strategy's human name: the flat name when the API sent one, the
// localized name otherwise.
func (v routingStrategyView) displayName() string {
	if v.Name != "" {
		return v.Name
	}
	return localeName(v.NameLocalized)
}

// routingStrategyList is the view `fft routing strategy list` renders.
func routingStrategyList() listView {
	return listView{Noun: "routing strategies", Rows: routingStrategyRows}
}

// renderStrategy renders one strategy: the API's own JSON under -o json, a one-row
// table otherwise.
func renderStrategy(deps *Deps, raw []byte) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}

	rows, err := routingStrategyRows(deps.Printer.Style(), []json.RawMessage{raw})
	if err != nil {
		return err
	}
	return deps.Printer.RenderRaw(rows, raw)
}

var routingStrategyHeaders = []string{"ID", "NAME", "REVISION", "IN USE", "VERSION"}

func routingStrategyRows(style output.Style, items []json.RawMessage) (output.Rows, error) {
	rows := make([][]string, 0, len(items))

	for i, item := range items {
		var v routingStrategyView
		if err := json.Unmarshal(item, &v); err != nil {
			return output.Rows{}, fmt.Errorf("decode routing strategy %d of %d: %w", i+1, len(items), err)
		}

		rows = append(rows, []string{
			field(style, v.ID),
			field(style, v.displayName()),
			fmt.Sprintf("%d", v.Revision),
			inUseCell(style, v.InUse),
			fmt.Sprintf("%d", v.Version),
		})
	}
	return output.Rows{Headers: routingStrategyHeaders, Rows: rows}, nil
}

// inUseCell marks the one live strategy. Only the live one is worth calling out, so
// the others get the same dash an absent value gets rather than a shouted "no".
func inUseCell(style output.Style, live bool) string {
	if live {
		return "yes"
	}
	return field(style, "")
}
