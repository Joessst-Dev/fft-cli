package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/client"
)

const routingCategoryDeleteLong = `Delete a routing node config category.

A category is only a label, so deleting one removes no routing logic — but any node
that referenced it loses that grouping. This cannot be undone.

fft reads the category first so it can ask about it by name rather than by UUID.
-y/--yes answers for you. On a non-interactive terminal there is nobody to ask, and
fft refuses rather than assuming yes.`

func newRoutingCategoryDeleteCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "delete <id>",
		Short:   "Delete a routing category",
		Long:    routingCategoryDeleteLong,
		Args:    usageArgs(cobra.ExactArgs(1)),
		Aliases: []string{"rm"},

		Annotations: map[string]string{annotationOperationID: "deleteRoutingStrategyNodeConfigCategory"},

		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := tenantClient(deps)
			if err != nil {
				return err
			}

			ctx, cancel := deps.Context(cmd)
			defer cancel()

			id := args[0]

			// Read it before asking about it: "Delete category 3f9c…?" is not a question
			// anybody can answer, and "Delete the category 'Peak season' (red)?" is. Skipped
			// under --yes, which has already said no question is coming.
			what := fmt.Sprintf("category %s", id)
			if !deps.AssumeYes {
				who, err := lookupCategory(ctx, c, id)
				if err != nil {
					return err
				}
				what = who
			}

			ok, err := confirmDestructive(deps, fmt.Sprintf(
				"Delete %s? Nodes that reference it lose the grouping. This cannot be undone.", what))
			if err != nil {
				return err
			}
			if !ok {
				deps.Printer.Notef("Aborted; category %s was not deleted.", id)
				return nil
			}

			// A DELETE is idempotent, so Do may safely retry it on a dropped connection.
			if _, err := c.Do(ctx, "delete routing category "+id, func(ctx context.Context) (*http.Response, error) {
				return c.API().DeleteRoutingStrategyNodeConfigCategory(ctx, id)
			}); err != nil {
				return err
			}

			// Nothing on stdout: a delete has no data to emit.
			deps.Printer.Notef("Deleted routing category %s.", id)
			return nil
		},
	}

	return cmd
}

// lookupCategory reads a category so the delete prompt can name it. One GET buys a
// question the user can actually answer.
func lookupCategory(ctx context.Context, c *client.Client, id string) (string, error) {
	raw, err := getCategory(ctx, c, id)
	if err != nil {
		return "", err
	}

	var v routingCategoryView
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", fmt.Errorf("decode category %s: %w", id, err)
	}

	name := v.displayName()
	switch {
	case name != "" && v.Color != "":
		return fmt.Sprintf("the category %q (%s)", name, v.Color), nil
	case name != "":
		return fmt.Sprintf("the category %q", name), nil
	default:
		return fmt.Sprintf("category %s", id), nil
	}
}
