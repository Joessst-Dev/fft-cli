package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/auth"
	"github.com/Joessst-Dev/fft-cli/internal/buildinfo"
	"github.com/Joessst-Dev/fft-cli/internal/client"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/network"
	"github.com/Joessst-Dev/fft-cli/internal/output"
)

const networkSnapshotLong = `Read the whole fulfillment network into a local snapshot file.

The snapshot holds every facility, every facility's connections, the facility
groups, the tags, the routing strategies, and the most recent routing decision
logs together with the tags of the orders they routed. The other network
commands work on it offline, so a check costs no requests and can run in CI.

  fft network snapshot
  fft network snapshot --out network.json.gz --concurrency 2
  fft network snapshot --resume

There is no tenant-wide connection list, so a crawl is one paged request per
facility: a large network takes an hour or more. It reads --concurrency
facilities at a time, backs off for a minute (then longer) when a gateway
throttles, and records each facility as it finishes. If it stops — an expired
session, a gateway that will not recover, Ctrl-C — run it again with --resume to
continue where it stopped.

The global --timeout bounds the whole crawl, and its 30-second default would end
any real one, so unless you set --timeout yourself the crawl allows itself 4h.

The snapshot is written to the cache directory, one file per project, readable
by you only. Keep it out of a repository: it is the tenant's whole network.

stdout carries a summary of what was read (with -o json, a document holding the
snapshot's path); progress goes to stderr. Every request is a read.`

// snapshotTimeout is the crawl's bound when --timeout was not set. The 30-second
// default exists to stop a hung request from hanging a terminal; a crawl of a
// production network took about an hour and a half.
const snapshotTimeout = 4 * time.Hour

func newNetworkSnapshotCmd(deps *Deps) *cobra.Command {
	var (
		out          string
		resume       bool
		concurrency  int
		decisionLogs int
		noOrders     bool
	)

	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Read every facility and connection into a local snapshot",
		Long:  networkSnapshotLong,
		Args:  usageArgs(cobra.NoArgs),

		Annotations: map[string]string{
			annotationOperations: strings.Join(network.CrawlOperations, ","),
			// Two runs writing one snapshot would interleave their journals.
			annotationExclusive: exclusiveSnapshot,
		},

		RunE: func(cmd *cobra.Command, _ []string) error {
			if concurrency < 1 || concurrency > network.MaxConcurrency {
				return exitcode.UsageError{Err: fmt.Errorf(
					"--concurrency must be between 1 and %d, not %d", network.MaxConcurrency, concurrency)}
			}
			if decisionLogs < 0 || decisionLogs > client.MaxSize {
				return exitcode.UsageError{Err: fmt.Errorf(
					"--decision-logs must be between 0 and %d, not %d", client.MaxSize, decisionLogs)}
			}
			return runNetworkSnapshot(cmd, deps, network.CrawlOptions{
				Out:          out,
				Resume:       resume,
				Concurrency:  concurrency,
				DecisionLogs: decisionLogs,
				Orders:       !noOrders,
			})
		},
	}

	f := cmd.Flags()
	f.StringVar(&out, "out", "", "Write the snapshot here instead of the project's file in the cache directory")
	f.BoolVar(&resume, "resume", false, "Continue a crawl that stopped, from the facilities it had already read")
	f.IntVar(&concurrency, "concurrency", network.DefaultConcurrency,
		"How many facilities' connections to read at once (at most "+strconv.Itoa(network.MaxConcurrency)+")")
	f.IntVar(&decisionLogs, "decision-logs", client.MaxSize,
		"How many recent routing decision logs to read; 0 skips them and the orders")
	f.BoolVar(&noOrders, "no-orders", false, "Do not read the orders the decision logs name (their tags make up the order profiles)")

	return cmd
}

func runNetworkSnapshot(cmd *cobra.Command, deps *Deps, o network.CrawlOptions) error {
	project, src, err := deps.tokenSource()
	if err != nil {
		return err
	}
	c, err := deps.apiClient(project, src)
	if err != nil {
		return err
	}

	o.Source = network.Source{Project: project.Name, BaseURL: project.BaseURL}
	if o.Out == "" {
		if o.Out, err = defaultSnapshotPath(deps, o.Source); err != nil {
			return err
		}
	}
	if _, renewable := src.(auth.Renewer); !renewable {
		deps.Printer.Warnf("this session's token cannot be renewed, and a crawl can outlast it. " +
			"If it expires, the crawl stops; take a new token and run again with --resume.")
	}

	ctx, cancel := snapshotContext(cmd, deps)
	defer cancel()

	o.FFTVersion = buildinfo.Version
	o.Progress = cmd.ErrOrStderr()
	// The client's back-off seam doubles as the crawl's, so a spec that proves a
	// throttled crawl carries on does not wait out a minute's pause to do it.
	o.Sleep = deps.Retry.Sleep

	s, err := network.Crawl(ctx, c, o)
	if errors.Is(err, context.Canceled) {
		// Ctrl-C exits quietly, so the hint the error carries would never be shown.
		if _, statErr := os.Stat(o.Out + network.JournalSuffix); statErr == nil {
			deps.Printer.Notef("Interrupted. Run again with --resume to continue from %s.", o.Out+network.JournalSuffix)
		}
	}
	if err != nil {
		return err
	}
	return renderSnapshotSummary(deps, o.Out, s)
}

// snapshotContext bounds the crawl by --timeout when the user set one, and by
// [snapshotTimeout] otherwise.
func snapshotContext(cmd *cobra.Command, deps *Deps) (context.Context, context.CancelFunc) {
	explicit := rootFlagChanged(cmd, "timeout") || (deps.ui != nil && deps.ui.timeout != nil)
	if v, ok := deps.env()("FFT_TIMEOUT"); ok && strings.TrimSpace(v) != "" {
		explicit = true
	}
	if explicit {
		return deps.Context(cmd)
	}
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(ctx, snapshotTimeout)
}

// snapshotSummary is what -o json prints: where the snapshot is, and what is in
// it. The snapshot itself is never printed; it is the file.
type snapshotSummary struct {
	Path          string    `json:"path" yaml:"path"`
	CreatedAt     time.Time `json:"createdAt" yaml:"createdAt"`
	Facilities    int       `json:"facilities" yaml:"facilities"`
	Online        int       `json:"online" yaml:"online"`
	Connections   int       `json:"connections" yaml:"connections"`
	ValueSets     int       `json:"valueSets" yaml:"valueSets"`
	Groups        int       `json:"groups" yaml:"groups"`
	Tags          int       `json:"tags" yaml:"tags"`
	Strategies    int       `json:"strategies" yaml:"strategies"`
	DecisionLogs  int       `json:"decisionLogs" yaml:"decisionLogs"`
	OrderProfiles int       `json:"orderProfiles" yaml:"orderProfiles"`
	Resumed       int       `json:"resumed" yaml:"resumed"`
	Pauses        int       `json:"pauses" yaml:"pauses"`
	Duration      string    `json:"duration" yaml:"duration"`
}

func renderSnapshotSummary(deps *Deps, path string, s *network.Snapshot) error {
	sum := snapshotSummary{
		Path:          path,
		CreatedAt:     s.CreatedAt,
		Facilities:    len(s.Facilities),
		Connections:   len(s.Edges),
		ValueSets:     len(s.ValueSets),
		Groups:        len(s.Groups),
		Tags:          len(s.Tags),
		Strategies:    len(s.Strategies),
		DecisionLogs:  len(s.DecisionLogs),
		OrderProfiles: len(s.OrderProfiles),
		Resumed:       s.Stats.Resumed,
		Pauses:        s.Stats.Pauses,
		Duration:      s.Stats.Duration.Round(time.Second).String(),
	}
	for _, f := range s.Facilities {
		if f.Online() {
			sum.Online++
		}
	}

	table := output.Rows{
		Headers: []string{"FACILITIES", "ONLINE", "CONNECTIONS", "GROUPS", "STRATEGIES", "DECISION LOGS", "ORDER PROFILES"},
		Rows: [][]string{{
			strconv.Itoa(sum.Facilities), strconv.Itoa(sum.Online), strconv.Itoa(sum.Connections),
			strconv.Itoa(sum.Groups), strconv.Itoa(sum.Strategies), strconv.Itoa(sum.DecisionLogs),
			strconv.Itoa(sum.OrderProfiles),
		}},
	}
	if deps.Printer.Format() == output.Table {
		deps.Printer.Notef("Snapshot written to %s (%s).", path, sum.Duration)
	}
	return deps.Printer.Render(table, sum)
}
