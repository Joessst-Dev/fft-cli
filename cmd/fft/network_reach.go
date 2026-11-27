package main

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/network"
	"github.com/Joessst-Dev/fft-cli/internal/output"
)

const networkReachLong = `Show which connections lead to one facility for one kind of order, and why the
others do not.

It answers the question behind a no-source or peer-coverage finding: which
connection should reach this store, and which context entry or exclusion stops
it? It reads the snapshot and sends no request.

  fft network reach --to BER-01
  fft network reach --to BER-01 --tag orderType=express
  fft network reach --to BER-01 --tag orderType=bulky --result excluded -o json

--tag gives the order's tags, as id=value, once per tag; without it the order
carries none. Every connection from an online source whose target could be the
facility is listed: one that names it, and every wildcard of its type. RESULT is
applies, conditional (it depends on an item's category or the recipient, which
tags do not say) or excluded, and REASON names the first thing that rules the
facility out, in the order the router meets it: the target, its exclusions, then
each context entry.

Sources are hubs, stores and suppliers as 'fft network check' decides them,
following fft-network.yaml when there is one. stdout carries the connections;
the counts go to stderr.`

func newNetworkReachCmd(deps *Deps) *cobra.Command {
	var (
		snapshot   string
		configPath string
		to         string
		tags       []string
		result     string
	)

	cmd := &cobra.Command{
		Use:   "reach --to <facility>",
		Short: "Show which connections lead to a facility, and why the others do not",
		Long:  networkReachLong,
		Args:  usageArgs(cobra.NoArgs),

		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireFlag(cmd, "to"); err != nil {
				return err
			}
			profile, err := tagProfile(tags)
			if err != nil {
				return err
			}
			if result != "" && !slices.Contains(reachResults, result) {
				return exitcode.UsageError{Err: fmt.Errorf("unknown --result %q: want one of %s",
					result, strings.Join(reachResults, ", "))}
			}

			path, err := snapshotPath(deps, snapshot)
			if err != nil {
				return err
			}
			s, err := network.Load(path)
			if err != nil {
				return err
			}
			n := network.New(s)
			i, ok := n.Locate(to)
			if !ok {
				return exitcode.UsageError{Err: fmt.Errorf("%s is not a facility in the snapshot %s", to, path)}
			}

			cfg := network.DefaultConfig()
			lf, err := loadNetworkConfig(configPath, cmd.Flags().Changed("config"))
			if err != nil {
				return err
			}
			if lf != nil {
				cfg = lf.Settings(time.Now())
			}

			rows := n.ReachTo(i, profile, n.InferRoles(cfg))
			describeReach(deps, n.Facility(i), profile, rows)
			if result != "" {
				rows = slices.DeleteFunc(rows, func(r network.ReachRow) bool { return r.Result != result })
			}
			if len(rows) == 0 {
				return deps.Printer.Empty("connections")
			}
			return renderReach(deps, rows)
		},
	}

	f := cmd.Flags()
	f.StringVar(&to, "to", "", "The facility, by tenant id or platform id")
	f.StringArrayVar(&tags, "tag", nil, "A tag the order carries, as id=value (repeatable)")
	f.StringVar(&result, "result", "", "Only connections with this result: "+strings.Join(reachResults, ", "))
	f.StringVar(&snapshot, "snapshot", "", "Read this snapshot instead of the project's file in the cache directory")
	f.StringVar(&configPath, "config", network.DefaultConfigFile, "The config file whose roles to use, when it exists")
	registerEnumCompletion(cmd, "result", reachResults)

	return cmd
}

var reachResults = []string{network.ResultApplies, network.ResultConditional, network.ResultExcluded}

// tagProfile turns --tag id=value flags into an order profile.
func tagProfile(tags []string) (network.Profile, error) {
	values := make([]network.TagValue, 0, len(tags))
	for _, t := range tags {
		id, value, ok := strings.Cut(t, "=")
		if !ok || id == "" || value == "" {
			return network.Profile{}, exitcode.UsageError{Err: fmt.Errorf("--tag %q: want id=value", t)}
		}
		values = append(values, network.TagValue{ID: id, Value: value})
	}
	return network.NewProfile(values), nil
}

// describeReach counts the rows on stderr, and names the reason that excludes
// the most connections: on a real network one context entry typically shuts
// out thousands, and that line is the answer.
func describeReach(deps *Deps, f *network.Facility, p network.Profile, rows []network.ReachRow) {
	counts := map[string]int{}
	reasons := map[string]int{}
	for _, r := range rows {
		counts[r.Result]++
		if r.Result == network.ResultExcluded {
			reasons[r.Reason]++
		}
	}
	deps.Printer.Notef("%s (%s, %s), for an order with %s: %d connections could lead there; %d apply, %d conditionally, %d excluded.",
		f.Label(), strings.ToLower(f.Status), f.Type, p, len(rows),
		counts[network.ResultApplies], counts[network.ResultConditional], counts[network.ResultExcluded])

	top := make([]string, 0, len(reasons))
	for r := range reasons {
		top = append(top, r)
	}
	slices.SortFunc(top, func(a, b string) int { return cmp.Or(cmp.Compare(reasons[b], reasons[a]), strings.Compare(a, b)) })
	for _, r := range top[:min(3, len(top))] {
		deps.Printer.Notef("  %d excluded: %s", reasons[r], r)
	}
}

func renderReach(deps *Deps, rows []network.ReachRow) error {
	table := output.Rows{Headers: []string{"CONNECTION", "SOURCE", "KIND", "RESULT", "TRANSIT", "REASON"}}
	for _, r := range rows {
		transit := "-"
		if r.Transit != nil {
			transit = fmt.Sprintf("%g-%gd", r.Transit.Min, r.Transit.Max)
		}
		table.Rows = append(table.Rows, []string{r.Connection, r.Source, string(r.Kind), r.Result, transit, r.Reason})
	}
	return deps.Printer.Render(table, rows)
}
