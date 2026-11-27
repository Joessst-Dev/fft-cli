package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/network"
	"github.com/Joessst-Dev/fft-cli/internal/output"
)

const networkCheckLong = `Find the routing gaps in a network snapshot.

It reads the snapshot 'fft network snapshot' wrote and sends no request, so it
runs in CI, offline, and as often as you like against the same data.

  fft network check
  fft network check --rule no-source --rule peer-coverage
  fft network check -o json | jq -r '.[] | select(.rule == "peer-coverage") | .subject'

The rules:

  no-source       a store no online source reaches, for some kind of order
  peer-coverage   a store no hub (or supplier) reaches, although one reaches at
                  least 90% of the stores in its country (groups of 10 or more)
  dangling-ref    a target, exclusion or context value that names a facility,
                  group or tag value the network does not have
  offline-ref     an offline facility that connections still lead to
  duplicate-edge  connections of one source that are the same connection

With fft-network.yaml, also:

  expect          a facility fewer sources reach than the file declares
  config-drift    the network has moved away from what the file pins

And on the routing strategy in use:

  strategy-in-use       no strategy in use, or more than one
  strategy-category     a node's categoryRef names no node config category
  strategy-inactive     an inactive condition or node with nodes below it
  strategy-expired      a node or condition whose time frames have all ended
  strategy-fallback     a fallback facility that is missing or offline
  strategy-no-rating    a path whose merged config has no active rating
  toolkit-hardcoded-id  a facility id in a toolkit rule that names nothing

A kind of order is the tags it carries, as far as any connection reads them,
taken from the orders the snapshot's decision logs routed. Hubs are the facilities
whose connections reach far more facilities than anybody else's. What the check
decided about both is printed on stderr, with how many of each country's stores a
hub and a supplier reach: a gap most of a country shares is no outlier, and only
that line shows it.

stdout carries the findings and nothing else; -o json adds each one's evidence
and next step. Exit 0 means no problems; exit 11 means the check ran and found
some. Fixing a gap is a change to the network, and that is its owner's decision.`

func newNetworkCheckCmd(deps *Deps) *cobra.Command {
	var (
		snapshot   string
		configPath string
		rules      []string
	)

	cmd := &cobra.Command{
		Use:   "check",
		Short: "Find the routing gaps in a network snapshot",
		Long:  networkCheckLong,
		Args:  usageArgs(cobra.NoArgs),

		RunE: func(cmd *cobra.Command, _ []string) error {
			for _, r := range rules {
				if !slices.Contains(network.RuleIDs(), r) {
					return exitcode.UsageError{Err: fmt.Errorf("unknown --rule %q: want one of %s",
						r, strings.Join(network.RuleIDs(), ", "))}
				}
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

			cfg := network.DefaultConfig()
			lf, err := loadNetworkConfig(configPath, cmd.Flags().Changed("config"))
			if err != nil {
				return err
			}
			if lf != nil {
				cfg = lf.Settings(time.Now())
				describeConfig(deps, lf, n)
			}
			if len(rules) > 0 {
				cfg.Rules = rules
			}
			report := n.Check(cfg)

			describeCheck(deps, path, n, cfg, report)
			return renderFindings(deps, report)
		},
	}

	f := cmd.Flags()
	f.StringVar(&snapshot, "snapshot", "", "Read this snapshot instead of the project's file in the cache directory")
	f.StringVar(&configPath, "config", network.DefaultConfigFile,
		"The config file; when it is the default and does not exist, the check infers everything")
	f.StringArrayVar(&rules, "rule", nil, "Run only this rule (repeatable): "+strings.Join(network.RuleIDs(), ", "))
	registerEnumCompletion(cmd, "rule", network.RuleIDs())

	return cmd
}

// snapshotPath is --snapshot, or the active project's snapshot. Naming a file
// needs no project at all, which is what lets CI check a snapshot it was handed.
func snapshotPath(deps *Deps, flag string) (string, error) {
	if flag != "" {
		return flag, nil
	}
	project, err := deps.ActiveProject()
	if err != nil {
		return "", err
	}
	return defaultSnapshotPath(deps, network.Source{Project: project.Name, BaseURL: project.BaseURL})
}

// loadNetworkConfig reads the config file. The default file is optional — a
// check without one infers everything — but one named with --config must exist:
// a CI job that points at a file which is not there is misconfigured, and
// quietly checking without it would pass gaps the file exists to catch.
func loadNetworkConfig(path string, named bool) (*network.LoadedFile, error) {
	lf, err := network.ReadConfig(path)
	switch {
	case err == nil:
		return lf, nil
	case network.IsNotExist(err) && !named:
		return nil, nil
	case network.IsNotExist(err):
		return nil, &network.ConfigError{Path: path, Problems: []network.Problem{{
			Message: "there is no config file there; write one with fft network infer"}}}
	default:
		return nil, err
	}
}

// describeConfig says on stderr what the config file changes, and warns about
// what in it has stopped doing anything.
func describeConfig(deps *Deps, lf *network.LoadedFile, n *network.Network) {
	inForce := len(lf.Settings(time.Now()).Suppress)
	deps.Printer.Notef("Config %s: %s, %s in force.", lf.Path,
		countOf(len(lf.Expect), "expectation"), countOf(inForce, "suppression"))
	for _, p := range lf.Expired(time.Now()) {
		deps.Printer.Warnf("%s: %s", lf.Path, p)
	}
	for _, p := range lf.Unmatched(n) {
		deps.Printer.Warnf("%s: %s", lf.Path, p)
	}
}

func countOf(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

// describeCheck tells the person on stderr what the check assumed. A finding is
// only as good as the roles and kinds of order behind it, and those were guessed.
func describeCheck(deps *Deps, path string, n *network.Network, cfg network.Config, r network.Report) {
	s := n.Snapshot()
	note := deps.Printer.Notef

	note("Snapshot %s, taken %s (%s ago): %d facilities, %d connections.", path,
		s.CreatedAt.Format(time.DateTime), time.Since(s.CreatedAt).Round(time.Minute), len(s.Facilities), len(s.Edges))

	switch hubs := r.Roles.Hubs.Count(); {
	case r.Roles.HubCut == 0 && hubs > 0:
		note("Hubs: %d, as the config pins them.", hubs)
	case hubs > 0:
		note("Hubs: %d, the facilities reaching %d or more others, %.1f× the next one down.",
			hubs, r.Roles.HubCut, r.Roles.HubJump)
	default:
		note("Hubs: none. No facility's connections reach %g× more facilities than the next one's.", cfg.HubRatio)
	}
	note("Stores: %d. Suppliers: %d.", r.Roles.Stores.Count(), r.Roles.Suppliers.Count())

	groups := make([]string, 0, len(r.Peers))
	for _, g := range r.Peers {
		groups = append(groups, fmt.Sprintf("%s %d", g.Key, g.Size))
	}
	by := "by country"
	if cfg.PeerBy != network.DefaultConfig().PeerBy {
		by = "by " + cfg.PeerBy
	}
	note("Peer groups, %s: %s (fewer than %d stores: not compared).", by, strings.Join(groups, ", "), cfg.PeerMinSize)

	orders := 0
	for _, p := range r.Profiles {
		orders += p.Orders
	}
	switch {
	case len(cfg.Declared) > 0:
		note("Order profiles: %d, as the config declares them.", len(r.Profiles))
	case orders > 0:
		note("Order profiles: %d, from %d routed orders.", len(r.Profiles), orders)
	default:
		note("Order profiles: %d, one per tag value a connection asks for (the snapshot has no routed orders).",
			len(r.Profiles))
	}

	for _, c := range r.Coverage {
		note("Coverage: a %s reaches %d of %d stores in %s (%s, for %s).",
			c.Kind, c.Have, c.Size, c.Group, pct(c.Have, c.Size), c.Profile)
	}
	if len(r.Suppressed) > 0 {
		note("Suppressed by the config: %s.", countOf(len(r.Suppressed), "finding"))
	}
}

func pct(have, size int) string {
	if size == 0 {
		return "-"
	}
	return strconv.Itoa(have*100/size) + "%"
}

// renderFindings prints the findings and turns their presence into exit 11.
func renderFindings(deps *Deps, r network.Report) error {
	if len(r.Findings) == 0 {
		if err := deps.Printer.Empty("problems"); err != nil {
			return err
		}
		return nil
	}

	rows := make([][]string, 0, len(r.Findings))
	for _, f := range r.Findings {
		rows = append(rows, []string{f.Rule, f.Subject, profilesCell(f.Profiles, len(r.Profiles)), f.Detail})
	}
	table := output.Rows{Headers: []string{"RULE", "SUBJECT", "PROFILES", "DETAIL"}, Rows: rows}
	if err := deps.Printer.Render(table, r.Findings); err != nil {
		return err
	}
	return &problemsError{findings: len(r.Findings)}
}

// profilesCell fits a finding's order profiles in a table cell.
func profilesCell(profiles []string, of int) string {
	switch {
	case len(profiles) == 0:
		return "-"
	case len(profiles) == of && of > 1:
		return "all"
	case len(profiles) == 1:
		return profiles[0]
	default:
		return fmt.Sprintf("%s +%d", profiles[0], len(profiles)-1)
	}
}

// problemsError is a check that ran and found problems: exit 11, after the
// findings are on stdout.
type problemsError struct{ findings int }

func (e *problemsError) Error() string {
	if e.findings == 1 {
		return "the check found 1 problem"
	}
	return fmt.Sprintf("the check found %d problems", e.findings)
}

// ExitCode is 11: the check worked, and its answer is no.
func (e *problemsError) ExitCode() int { return exitcode.ProblemsFound }

// Hint points at the detail a table leaves out.
func (e *problemsError) Hint() string {
	return "-o json gives each finding's evidence and the first thing to look at."
}
