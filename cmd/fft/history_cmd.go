package main

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/config"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/history"
	"github.com/Joessst-Dev/fft-cli/internal/output"
)

const historyLong = `Show and clear the requests fft has sent.

fft keeps a local record of every command that addressed an API operation, typed
in a shell or sent from fft tui: when it ran, against which project, which
operation, the flags it was given, the HTTP status and the exit code. The requests
fft tui makes on its own, such as reading your roles, are not recorded. Request and
response bodies are never recorded, and so are no inline --data bodies, header
values, template values, credential-shaped flags, or flags and query parameters
that name a person's data (email, phone, address, street, postal code, first and
last name, username, search term): those are kept as <redacted>.

Everything else is kept as typed, because reopening a request needs it: the ids
and other arguments, the names of --file bodies, and filter values such as a
status or a date. Anything personal passed that way is in the record too.

The record is ~/.local/state/fft/history.jsonl (or under $XDG_STATE_HOME), mode
0600 in a 0700 directory: fft takes group and other access away from either
before it records. It never leaves the machine.

Recording is off in headless mode (FFT_BASE_URL and friends, with no --project
naming a configured project) unless FFT_HISTORY=on is set. FFT_HISTORY=off, or settings.noHistory: true in the config file, switches
it off everywhere.`

const historyListLong = `List the most recent requests, newest first.

The list covers the current project — --project, or the active one — unless
--all-projects is given. Under -o json the entries are printed as recorded.`

const historyTopLong = `List the operations used most, most used first.

The count is per project, and covers the current project — --project, or the
active one — unless --all-projects is given. Under -o json each operation is an
object with project, operationId, command, count and lastUsed.`

const historyClearLong = `Delete the request history.

It asks first, unless --yes is given; without a terminal to ask on, it refuses.`

func newHistoryCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "history",
		Short: "Show the requests fft has sent",
		Long:  historyLong,
		Args:  usageArgs(cobra.NoArgs),

		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}

	cmd.AddCommand(
		newHistoryListCmd(deps),
		newHistoryTopCmd(deps),
		newHistoryClearCmd(deps),
	)
	return cmd
}

func newHistoryListCmd(deps *Deps) *cobra.Command {
	var (
		limit       int
		allProjects bool
	)

	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List the most recent requests",
		Long:    historyListLong,
		Aliases: []string{"ls"},
		Args:    usageArgs(cobra.NoArgs),
		RunE: func(_ *cobra.Command, _ []string) error {
			if limit < 0 {
				return exitcode.UsageError{Err: errors.New("--limit cannot be negative")}
			}

			project, err := historyProject(deps, allProjects)
			if err != nil {
				return err
			}

			entries, err := readHistory(deps)
			if err != nil {
				return err
			}
			if project != "" {
				entries = slices.DeleteFunc(entries, func(e history.Entry) bool { return e.Project != project })
			}
			slices.Reverse(entries)
			if limit > 0 && len(entries) > limit {
				entries = entries[:limit]
			}

			if len(entries) == 0 {
				return deps.Printer.Empty("recorded requests")
			}
			return deps.Printer.Render(historyRows(deps.Printer.Style(), entries), entries)
		},
	}

	f := cmd.Flags()
	f.IntVar(&limit, "limit", 20, "Show at most this many requests (0 for all)")
	f.BoolVar(&allProjects, "all-projects", false, "List every project's requests")
	return cmd
}

func newHistoryTopCmd(deps *Deps) *cobra.Command {
	var (
		limit       int
		allProjects bool
	)

	cmd := &cobra.Command{
		Use:   "top",
		Short: "List the operations used most",
		Long:  historyTopLong,
		Args:  usageArgs(cobra.NoArgs),
		RunE: func(_ *cobra.Command, _ []string) error {
			if limit < 0 {
				return exitcode.UsageError{Err: errors.New("--limit cannot be negative")}
			}

			project, err := historyProject(deps, allProjects)
			if err != nil {
				return err
			}

			entries, err := readHistory(deps)
			if err != nil {
				return err
			}

			top := history.Top(entries, project, limit)
			if len(top) == 0 {
				return deps.Printer.Empty("recorded operations")
			}
			return deps.Printer.Render(usageRows(top), top)
		},
	}

	f := cmd.Flags()
	f.IntVar(&limit, "limit", 10, "Show at most this many operations (0 for all)")
	f.BoolVar(&allProjects, "all-projects", false, "Count every project's requests, each project separately")
	return cmd
}

func newHistoryClearCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "clear",
		Short: "Delete the request history",
		Long:  historyClearLong,
		Args:  usageArgs(cobra.NoArgs),
		Annotations: map[string]string{
			annotationExclusive: exclusiveHistory,
			annotationConfirms:  confirmsYes,
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			ok, err := confirmHistoryClear(deps)
			if err != nil || !ok {
				return err
			}

			log, err := deps.historyLog()
			if err != nil {
				return err
			}
			n, err := log.Clear()
			if err != nil {
				return err
			}
			deps.Printer.Notef("Cleared %d %s from the request history.", n, plural(n, "entry", "entries"))
			return nil
		},
	}
}

// confirmHistoryClear asks before deleting the history, the way
// [confirmRemoval] asks before deleting a project: never on a guess.
func confirmHistoryClear(deps *Deps) (bool, error) {
	if deps.AssumeYes {
		return true, nil
	}
	if !deps.Prompt.CanConfirm() {
		return false, exitcode.UsageError{Err: errors.New(
			"stdin is not a terminal, so fft cannot ask for confirmation: pass --yes to clear the history")}
	}
	return deps.Prompt.Confirm("Delete the whole request history?")
}

// readHistory reads the history file, and says on stderr when nothing new is
// being added to it — an empty or stale list is otherwise a mystery.
func readHistory(deps *Deps) ([]history.Entry, error) {
	if reason := deps.historyOff(); reason != "" {
		deps.Printer.Notef("Requests are not being recorded: %s.", reason)
	}

	log, err := deps.historyLog()
	if err != nil {
		return nil, err
	}
	return log.Read()
}

// historyProject is the project a history command covers: the current one, or
// every project ("") when all is set.
func historyProject(deps *Deps, all bool) (string, error) {
	if all {
		return "", nil
	}
	return deps.currentProjectName()
}

// currentProjectName is the name of the project a command would act on, found
// without reading the keychain: only the name is needed.
func (d *Deps) currentProjectName() (string, error) {
	if d.Ephemeral != nil && (d.Project == "" || d.Project == d.Ephemeral.Name) {
		return d.Ephemeral.Name, nil
	}
	cfg, err := d.LoadConfig()
	if err != nil {
		return "", err
	}
	p, err := cfg.Resolve(d.Project)
	if err != nil {
		if errors.Is(err, config.ErrNoActiveProject) {
			return "", config.NewError(config.ErrNoActiveProject,
				"Name one with --project, or pass --all-projects to cover every project.")
		}
		return "", err
	}
	return p.Name, nil
}

func historyRows(style output.Style, entries []history.Entry) output.Rows {
	rows := make([][]string, 0, len(entries))
	for _, e := range entries {
		exit := strconv.Itoa(e.Exit)
		if e.Exit != exitcode.OK {
			exit = style.Red(exit)
		}
		rows = append(rows, []string{
			e.TS.Local().Format(time.DateTime),
			e.Project,
			e.OperationID,
			orDash(e.Status),
			exit,
			(time.Duration(e.DurationMS) * time.Millisecond).String(),
			strings.Join(append([]string{e.Command}, e.Args...), " "),
		})
	}
	return output.Rows{
		Headers: []string{"WHEN", "PROJECT", "OPERATION", "STATUS", "EXIT", "TOOK", "COMMAND"},
		Rows:    rows,
	}
}

func usageRows(top []history.Usage) output.Rows {
	rows := make([][]string, 0, len(top))
	for _, u := range top {
		rows = append(rows, []string{
			strconv.Itoa(u.Count),
			u.LastUsed.Local().Format(time.DateTime),
			u.Project,
			u.OperationID,
			u.Command,
		})
	}
	return output.Rows{
		Headers: []string{"COUNT", "LAST USED", "PROJECT", "OPERATION", "COMMAND"},
		Rows:    rows,
	}
}

func orDash(status int) string {
	if status == 0 {
		return "-"
	}
	return strconv.Itoa(status)
}
