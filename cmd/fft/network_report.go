package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/atomicfile"
	"github.com/Joessst-Dev/fft-cli/internal/buildinfo"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/network"
	"github.com/Joessst-Dev/fft-cli/internal/output"
)

const networkReportLong = `Draw a network snapshot as a page you open in a browser.

The page shows what 'fft network check' assumed (the hubs, the peer groups, the
kinds of order) and a coverage heatmap: one row per peer group of stores, one
column per kind of order, each cell the share of the group's stores a hub (or a
supplier, or any source) reaches. A gap most of a country shares, which no
outlier rule reports, is the dark row. Choose a cell to list the stores it
misses.

Choose a store to see its neighbourhood: every connection that could lead to
it, as 'fft network reach' lists them, drawn in bundles of the same kind of
source, result and reason. Where one context entry shuts out two thousand
connections, that is one line. It explains every facility a finding names and
the stores the cells list, up to --max-facilities; --facility adds others.

  fft network report
  fft network report --facility BER-01 --out gaps.html
  fft network report -o json | jq '.coverage[] | select(.kind == "hub")'

Every number is the check's own, from the same snapshot and fft-network.yaml;
the page only draws them. It is one file with nothing to fetch: it needs no
server, works offline, and its Content-Security-Policy forbids it any request.

The report holds the tenant's network, as the snapshot does. It is written
next to the snapshot, in the cache directory, readable by its owner only; keep
it out of a repository. stdout carries its path, or with -o json the data the
page draws (--out - prints the page instead, so it cannot be
combined with -o json). It sends no request, and exits 0 whatever it shows: 'fft network
check' is the gate.`

func newNetworkReportCmd(deps *Deps) *cobra.Command {
	var (
		snapshot      string
		configPath    string
		out           string
		facilities    []string
		maxFacilities int
	)

	cmd := &cobra.Command{
		Use:   "report",
		Short: "Draw a network snapshot as a self-contained HTML page",
		Long:  networkReportLong,
		Args:  usageArgs(cobra.NoArgs),

		RunE: func(cmd *cobra.Command, _ []string) error {
			// Both want stdout, for different things: the page, or the data the
			// page draws. Silently picking one hands an agent markup where it
			// asked for JSON.
			if out == "-" && deps.Printer.Format() != output.Table {
				return exitcode.UsageError{Err: fmt.Errorf(
					"--out - prints the page on stdout; it cannot be combined with -o %s", deps.Printer.Format())}
			}
			if maxFacilities < 1 {
				return exitcode.UsageError{Err: fmt.Errorf("--max-facilities must be at least 1, not %d", maxFacilities)}
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
			}

			opts := network.ViewOptions{FFTVersion: buildinfo.Version, MaxFacilities: maxFacilities}
			for _, ref := range facilities {
				i, ok := n.Locate(ref)
				if !ok {
					return exitcode.UsageError{Err: fmt.Errorf("%s is not a facility in the snapshot %s", ref, path)}
				}
				opts.Facilities = append(opts.Facilities, i)
			}

			v := n.View(cfg, opts)
			page, err := network.RenderReport(v)
			if err != nil {
				return err
			}
			describeReport(deps, v)

			if out == "-" {
				_, err := deps.Printer.Out().Write(page)
				return err
			}
			if out == "" {
				out = reportPathFor(path)
			}
			if repo := enclosingRepo(out); repo != "" {
				deps.Printer.Warnf("%s is inside the repository at %s. The report holds the tenant's network; "+
					"keep it out of version control.", out, repo)
			}
			// Owner-only, as the snapshot it was drawn from.
			if err := atomicfile.Write(out, page); err != nil {
				return fmt.Errorf("write %s: %w", out, err)
			}

			// Said in every format: under -o json stdout is the data, and this is
			// the only place the page's path is told.
			deps.Printer.Notef("Report written to %s; open it in a browser.", out)
			if deps.Printer.Format() == output.Table {
				_, err := fmt.Fprintln(deps.Printer.Out(), out)
				return err
			}
			raw, err := json.Marshal(v)
			if err != nil {
				return err
			}
			return deps.Printer.RenderRaw(output.Rows{}, raw)
		},
	}

	f := cmd.Flags()
	f.StringVar(&out, "out", "", "Write the page here instead of next to the snapshot; '-' prints it on stdout")
	f.StringArrayVar(&facilities, "facility", nil,
		"Explain this facility's connections too, by tenant id or platform id (repeatable)")
	f.IntVar(&maxFacilities, "max-facilities", network.DefaultMaxFacilities,
		"Explain at most this many facilities' connections")
	f.StringVar(&snapshot, "snapshot", "", "Read this snapshot instead of the project's file in the cache directory")
	f.StringVar(&configPath, "config", network.DefaultConfigFile, "The config file to follow, when it exists")

	return cmd
}

// reportPathFor is where the report of the snapshot at path goes by default:
// beside it, so it lives where the snapshot does — the cache directory, unless
// the snapshot was put elsewhere on purpose.
func reportPathFor(path string) string {
	for _, ext := range []string{".snapshot.json.gz", ".json.gz", ".gz"} {
		if base, ok := strings.CutSuffix(path, ext); ok {
			return base + ".report.html"
		}
	}
	return path + ".report.html"
}

// enclosingRepo is the root of the git working tree path would be written
// into, or "" when there is none. A report is the tenant's whole network, and
// a file in a working tree is one 'git add .' away from a public repository.
func enclosingRepo(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	for dir := filepath.Dir(abs); ; {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		} else if !errors.Is(err, fs.ErrNotExist) {
			return ""
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// describeReport says on stderr what the page will show, and that the check,
// not the report, is what decides.
func describeReport(deps *Deps, v network.View) {
	findings := 0
	for _, c := range v.Findings {
		findings += c
	}
	deps.Printer.Notef("%s in %s, %s of order; %s (fft network check lists them).",
		countOf(v.Counts.Stores, "store"), countOf(len(v.Peers), "peer group"),
		countOf(len(v.Profiles), "kind"), countOf(findings, "finding"))
	if v.Omitted > 0 {
		deps.Printer.Notef("It explains the connections of %d facilities; %d more are left out "+
			"(--max-facilities), and fft network reach --to <facility> explains any of them.",
			len(v.Neighbourhoods), v.Omitted)
	}
}
