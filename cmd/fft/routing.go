package main

import (
	"sort"

	"github.com/spf13/cobra"
)

const routingLong = `Configure how orders are routed.

The routing engine decides which facility fulfils an order. This group reaches the
pieces of that decision you can shape:

  fft routing strategy list        the strategies, one of which is 'inUse'
  fft routing category list        the node config categories a strategy references
  fft routing decision-logs        why the router chose what it chose

A strategy is a tree of nodes, each with fences (which facilities are eligible) and
ratings (which is preferred). Only one strategy is live at a time — 'fft routing
strategy activate' is how you switch — and 'fft routing strategy evaluate' dry-runs
one against an order without touching a thing.

'fft sourcing simulate' is the neighbouring tool: it shows the answer the live
strategy produces for an order, named down to each connection it would use.`

func newRoutingCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "routing",
		Aliases: []string{"route"},
		Short:   "Configure order routing",
		Long:    routingLong,
		Args:    usageArgs(cobra.NoArgs),

		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}

	cmd.AddCommand(
		newRoutingStrategyCmd(deps),
		newRoutingCategoryCmd(deps),
		newRoutingDecisionLogsCmd(deps),
	)

	return cmd
}

// localeName renders a LocaleString — the API's map of locale to text — as the one
// string a table cell can hold.
//
// The routing entities carry their human name only as a nameLocalized map; there is
// often no flat `name` beside it. en_US is preferred because the CLI's own language
// is English, and a sorted key is the fallback rather than a range over the map so
// that the same entity renders the same cell every time — a map's iteration order is
// deliberately random, and a table whose names shuffled between runs would read as a
// bug.
func localeName(m map[string]string) string {
	if v, ok := m["en_US"]; ok && v != "" {
		return v
	}

	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		if m[k] != "" {
			return m[k]
		}
	}
	return ""
}
