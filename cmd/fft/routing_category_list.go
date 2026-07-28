package main

import (
	"context"
	"encoding/json"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/client"
)

const routingCategoryListLong = `List the routing node config categories.

  fft routing category list
  fft routing category list --all -o json | jq -r '.[].id'

This endpoint pages by id, so --all fetches every category and stops at --max-items,
saying so on stderr if it had to. stdout carries the categories and nothing else.`

func newRoutingCategoryListCmd(deps *Deps) *cobra.Command {
	var page pageFlags

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the routing categories",
		Long:  routingCategoryListLong,
		Args:  usageArgs(cobra.NoArgs),

		Annotations: map[string]string{annotationOperationID: "getRoutingStrategyNodeConfigCategories"},

		RunE: func(cmd *cobra.Command, _ []string) error {
			build := func(context.Context, *client.Client) (client.ListOp[json.RawMessage], error) {
				return client.RoutingCategories(), nil
			}
			return runList(cmd, deps, build, page, routingCategoryList())
		},
	}

	page.register(cmd.Flags(), "categories", client.DefaultListSize)

	return cmd
}
