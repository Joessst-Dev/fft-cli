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

const routingCategoryLong = `Manage routing node config categories.

A category is a label — a name and a colour — that a routing strategy's nodes can be
grouped under. It carries no routing logic of its own; it organises the strategy tree
so a human editing it in the UI can tell one cluster of nodes from another.

  fft routing category list
  fft routing category get <id>
  fft routing category create --example`

func newRoutingCategoryCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "category",
		Aliases: []string{"categories"},
		Short:   "Manage routing node config categories",
		Long:    routingCategoryLong,
		Args:    usageArgs(cobra.NoArgs),

		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}

	cmd.AddCommand(
		newRoutingCategoryListCmd(deps),
		newRoutingCategoryGetCmd(deps),
		newRoutingCategoryCreateCmd(deps),
		newRoutingCategoryUpdateCmd(deps),
		newRoutingCategoryDeleteCmd(deps),
	)

	return cmd
}

// getCategory reads one category, as the API wrote it.
func getCategory(ctx context.Context, c *client.Client, id string) ([]byte, error) {
	res, err := c.Do(ctx, fmt.Sprintf("get routing category %s", id),
		func(ctx context.Context) (*http.Response, error) {
			return c.API().GetRoutingStrategyNodeConfigCategory(ctx, id)
		})
	if err != nil {
		return nil, err
	}
	return res.Body, nil
}

// getCategoryDoc reads one category and its version together — the read half of the
// read-then-write update.
func getCategoryDoc(ctx context.Context, c *client.Client, id string) (entityDoc, int, error) {
	raw, err := getCategory(ctx, c, id)
	if err != nil {
		return nil, 0, err
	}

	doc, err := decodeDoc(raw, "the routing category")
	if err != nil {
		return nil, 0, err
	}

	version, err := docVersion(doc, "routing category")
	if err != nil {
		return nil, 0, err
	}
	return doc, version, nil
}

// routingCategoryView is the table's model of a category. Like the strategy, the
// category is an allOf that oapi-codegen collapses lossily, so this is hand-written.
type routingCategoryView struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	NameLocalized map[string]string `json:"nameLocalized"`
	Color         string            `json:"color"`
	Version       int64             `json:"version"`
}

func (v routingCategoryView) displayName() string {
	if v.Name != "" {
		return v.Name
	}
	return localeName(v.NameLocalized)
}

// routingCategoryList is the view `fft routing category list` renders.
func routingCategoryList() listView {
	return listView{Noun: "routing categories", Rows: routingCategoryRows}
}

// renderCategory renders one category: the API's own JSON under -o json, a one-row
// table otherwise.
func renderCategory(deps *Deps, raw []byte) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}

	rows, err := routingCategoryRows(deps.Printer.Style(), []json.RawMessage{raw})
	if err != nil {
		return err
	}
	return deps.Printer.RenderRaw(rows, raw)
}

var routingCategoryHeaders = []string{"ID", "NAME", "COLOR", "VERSION"}

func routingCategoryRows(style output.Style, items []json.RawMessage) (output.Rows, error) {
	rows := make([][]string, 0, len(items))

	for i, item := range items {
		var v routingCategoryView
		if err := json.Unmarshal(item, &v); err != nil {
			return output.Rows{}, fmt.Errorf("decode routing category %d of %d: %w", i+1, len(items), err)
		}

		rows = append(rows, []string{
			field(style, v.ID),
			field(style, v.displayName()),
			field(style, v.Color),
			fmt.Sprintf("%d", v.Version),
		})
	}
	return output.Rows{Headers: routingCategoryHeaders, Rows: rows}, nil
}
