package main

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/Joessst-Dev/fft-cli/internal/api"
	"github.com/Joessst-Dev/fft-cli/internal/config"
	"github.com/Joessst-Dev/fft-cli/internal/history"
)

// runRecord collects, while a command runs, what its history entry needs and only
// the run itself learns: the project it resolved and the last HTTP status it got.
// Both are written from whichever goroutine made the request, hence atomics.
type runRecord struct {
	project atomic.Pointer[string]
	status  atomic.Int64
}

// observe is the API client's status observer for this run: it keeps the status
// for history, and passes it on to whoever else asked for it.
func (d *Deps) observe(status int) {
	if d.run != nil {
		d.run.status.Store(int64(status))
	}
	if d.observeStatus != nil {
		d.observeStatus(status)
	}
}

// historyLog is the history file this run records to and reads from.
func (d *Deps) historyLog() (history.Log, error) {
	path := d.HistoryPath
	if path == "" {
		var err error
		if path, err = history.Path(); err != nil {
			return history.Log{}, err
		}
	}
	return history.Log{Path: path, MaxBytes: d.historyMaxBytes}, nil
}

// historyOff says why this run records no history, and "" when it does.
//
// The environment decides first, as it does for every setting: FFT_HISTORY=off
// switches recording off everywhere, and FFT_HISTORY=on switches it on even in
// headless mode — which is otherwise off, because a CI job's history is a file on a
// runner nobody reads, filling up with the job's project names. Then the config
// file's settings.noHistory.
//
// It may run for a command that failed before [Deps.complete] did, so it opens
// what it needs itself rather than relying on what complete would have set.
func (d *Deps) historyOff() string {
	if enabled, set := config.HistoryFromEnv(os.LookupEnv); set {
		if enabled {
			return ""
		}
		return config.EnvHistory + " is off"
	}

	if d.Ephemeral != nil {
		return "fft is running from the environment (set " + config.EnvHistory + "=on to record)"
	}
	if _, headless, err := config.FromEnv(os.LookupEnv); headless || err != nil {
		return "fft is running from the environment (set " + config.EnvHistory + "=on to record)"
	}

	if d.Config == nil {
		path, err := config.DefaultPath()
		if err != nil {
			return "the config file cannot be located"
		}
		d.Config = config.NewStore(path)
	}
	cfg, err := d.LoadConfig()
	if err != nil {
		// A setting that cannot be read cannot be honoured, and the one it might say
		// is "do not record".
		return "the config file cannot be read"
	}
	if cfg.Settings.NoHistory {
		return "settings.noHistory is set in the config file"
	}
	return ""
}

// recordHistory appends the run of cmd to the request history, if it addressed an
// operation and history is on.
//
// It has no way to fail the command, by construction: it returns nothing, it runs
// after the exit code was decided and the error reported, and it recovers from its
// own panics. What goes wrong is said on the --debug stream and nowhere else — a
// command's output must not depend on whether a file in the state directory could
// be written.
func (d *Deps) recordHistory(cmd *cobra.Command, code int, elapsed time.Duration) {
	defer func() {
		if r := recover(); r != nil {
			d.debugHistory("recording failed: %v", r)
		}
	}()

	entry, ok := d.historyEntry(cmd, code, elapsed)
	if !ok {
		return
	}
	if reason := d.historyOff(); reason != "" {
		d.debugHistory("not recorded: %s", reason)
		return
	}

	log, err := d.historyLog()
	if err == nil {
		err = log.Append(entry)
	}
	if err != nil {
		d.debugHistory("not recorded: %v", err)
	}
}

func (d *Deps) debugHistory(format string, args ...any) {
	if d.Debug != nil {
		fmt.Fprintf(d.Debug, "history: "+format+"\n", args...)
	}
}

// historyEntry describes the run of cmd, and reports false for a run history does
// not keep: one that addressed no operation, asked only for help or an example,
// or started a component, whose requests fft never sees.
func (d *Deps) historyEntry(cmd *cobra.Command, code int, elapsed time.Duration) (history.Entry, bool) {
	if cmd == nil {
		return history.Entry{}, false
	}
	if _, component := cmd.Annotations[annotationComponent]; component {
		return history.Entry{}, false
	}
	if flagTrue(cmd, "help") || exampleWanted(cmd) {
		return history.Entry{}, false
	}

	positional := cmd.Flags().Args()
	id, ok := historyOperation(cmd, positional)
	if !ok {
		return history.Entry{}, false
	}

	source := history.SourceCLI
	if d.ui != nil {
		source = history.SourceTUI
	}

	entry := history.Entry{
		V:           history.Version,
		TS:          time.Now().UTC(),
		Source:      source,
		Project:     d.historyProject(),
		OperationID: id,
		Command:     cmd.CommandPath(),
		Args:        history.Redact(slices.Concat(positional, changedFlags(cmd))),
		Exit:        code,
		DurationMS:  elapsed.Milliseconds(),
	}
	if d.run != nil {
		entry.Status = int(d.run.status.Load())
	}
	return entry, true
}

// historyOperation is the operation cmd addressed: its annotation, or for
// `fft api <operationId>` the operation its argument names. A command that
// addresses none — `fft project add`, `fft version` — is not history.
func historyOperation(cmd *cobra.Command, positional []string) (string, bool) {
	if id, ok := cmd.Annotations[annotationOperationID]; ok {
		return id, true
	}
	if cmd.CommandPath() != "fft api" || len(positional) == 0 {
		return "", false
	}
	op, ok := api.LookupOperation(strings.TrimSpace(positional[0]))
	return op.ID, ok
}

// historyProject is the project the run acted on: the one it resolved, or, for a
// run that failed before resolving one, the one it would have resolved. It never
// reads the keychain — only a name is needed.
func (d *Deps) historyProject() string {
	if d.run != nil {
		if p := d.run.project.Load(); p != nil {
			return *p
		}
	}
	if d.Ephemeral != nil && (d.Project == "" || d.Project == d.Ephemeral.Name) {
		return d.Ephemeral.Name
	}
	if d.Config == nil {
		return d.Project
	}
	cfg, err := d.LoadConfig()
	if err != nil {
		return d.Project
	}
	p, err := cfg.Resolve(d.Project)
	if err != nil {
		return d.Project
	}
	return p.Name
}

// changedFlags is every flag given on cmd's command line, as --name=value, with a
// repeated or list flag given once per value and a true boolean as a bare --name.
func changedFlags(cmd *cobra.Command) []string {
	var out []string
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if !f.Changed || f.Name == "help" {
			return
		}
		name := "--" + f.Name
		switch v := f.Value.(type) {
		case pflag.SliceValue:
			for _, item := range v.GetSlice() {
				out = append(out, name+"="+item)
			}
		default:
			if f.Value.Type() == "bool" && f.Value.String() == "true" {
				out = append(out, name)
				return
			}
			out = append(out, name+"="+f.Value.String())
		}
	})
	return out
}

// flagTrue reports whether cmd's boolean flag name was set to true.
func flagTrue(cmd *cobra.Command, name string) bool {
	f := cmd.Flags().Lookup(name)
	return f != nil && f.Value.String() == "true"
}
