package main

import (
	"context"
	"os"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/buildinfo"
	"github.com/Joessst-Dev/fft-cli/internal/output"
	"github.com/Joessst-Dev/fft-cli/internal/prompt"
	"github.com/Joessst-Dev/fft-cli/internal/update"
)

// envNoUpdateCheck turns the background check off entirely. Any non-empty value
// counts, following the NO_COLOR convention the rest of fft already honours.
const envNoUpdateCheck = "FFT_NO_UPDATE_CHECK"

const updateLong = `Check whether a newer fft release is available.

fft looks for a new release at most once a day, in the background, and mentions
it on stderr when there is one. The check can never delay or fail a command: it
is given 1.5 seconds, and whatever has not arrived by the time the command
finishes is dropped.

It is skipped entirely when the output is being piped or redirected, when -o json
or -o yaml is in effect, on a build that did not come from a release tag, when
FFT_NO_UPDATE_CHECK is set, and when settings.updateCheck is false in
~/.config/fft/config.yaml.

This command asks now, regardless of all of that, and ignores the once-a-day
cache.`

// updateView is what `fft update check` renders.
//
// UpToDate is a pointer because the question has three answers, not two: a build
// that did not come from a release tag has no place in the version ordering, and
// null is the only honest thing to say about it. Reporting "up to date" for a
// `go build` binary that is in fact six releases behind would be a lie, and the
// kind a script would act on.
type updateView struct {
	Current  string `json:"current" yaml:"current"`
	Latest   string `json:"latest" yaml:"latest"`
	UpToDate *bool  `json:"upToDate" yaml:"upToDate"`
	URL      string `json:"url,omitempty" yaml:"url,omitempty"`
}

func newUpdateCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Check whether a newer fft release is available",
		Long:  updateLong,
	}
	cmd.AddCommand(newUpdateCheckCmd(deps))
	return cmd
}

func newUpdateCheckCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "Ask GitHub for the latest release now",
		Long:  updateLong,
		Args:  usageArgs(cobra.NoArgs),

		RunE: func(cmd *cobra.Command, _ []string) error {
			return runUpdateCheck(cmd, deps)
		},
	}
}

func runUpdateCheck(cmd *cobra.Command, deps *Deps) error {
	checker, err := deps.updateChecker()
	if err != nil {
		return err
	}

	ctx, cancel := deps.Context(cmd)
	defer cancel()

	// Unlike the background check, this one reports its failure: the user asked
	// for an answer, and silence would be a lie.
	state, err := checker.Refresh(ctx)
	if err != nil {
		return err
	}

	current := checker.Current()
	notice := checker.Notice(state)

	view := updateView{
		Current: current,
		Latest:  state.LatestVersion,
		URL:     state.URL,
	}
	if update.Comparable(current) {
		view.UpToDate = ptr(notice == "")
	}

	if err := deps.Printer.Render(updateRows(view), view); err != nil {
		return err
	}

	// The upgrade line is advice, not data, so it goes where advice goes — and
	// only to a human. Under -o json the same fact is already in the payload, and
	// a second copy of it on stderr is noise a script did not ask for.
	if notice != "" && deps.Printer.Format() == output.Table {
		deps.Printer.Notef("%s", notice)
	}
	return nil
}

var updateHeaders = []string{"CURRENT", "LATEST", "STATUS"}

func updateRows(v updateView) output.Rows {
	status := "unknown (not a release build)"
	switch {
	case v.UpToDate == nil:
	case *v.UpToDate:
		status = "up to date"
	default:
		status = "update available"
	}

	return output.Rows{
		Headers: updateHeaders,
		Rows:    [][]string{{v.Current, v.Latest, status}},
	}
}

// updateChecker builds the release checker, or returns the one a spec injected.
func (d *Deps) updateChecker() (*update.Checker, error) {
	if d.Update != nil {
		return d.Update, nil
	}

	path, err := update.DefaultCachePath()
	if err != nil {
		return nil, err
	}
	d.Update = update.New(buildinfo.Version, path)
	return d.Update, nil
}

// startUpdateCheck kicks the release check off, if it is allowed to run at all.
//
// It is called from PersistentPreRunE and must return immediately: whatever it
// starts, the command does not wait for it. [Deps.reportUpdate] collects the
// result in PersistentPostRun, and drops it if it has not arrived.
func (d *Deps) startUpdateCheck(cmd *cobra.Command) {
	// A Deps outlives one command in the specs, and would outlive one in a future
	// interactive shell. Last run's answer is not this run's.
	d.updates, d.updateState, d.updateDone = nil, nil, nil

	if !d.updateAllowed(cmd) {
		return
	}

	checker, err := d.updateChecker()
	if err != nil {
		// No cache directory, no check. This is a chore, not a command.
		return
	}

	// A fresh cache answers without a goroutine and without a request. This is the
	// overwhelmingly common case — 1 check a day, every other invocation reads one
	// small file — and doing it here rather than in the background is what makes
	// the notice reliable instead of a race the command usually loses.
	if state, fresh := checker.Cached(); fresh {
		d.updateState = &state
		return
	}

	// Stamped before the request, not after it: this process will very likely exit
	// while the request is still in flight — most commands are faster than a round
	// trip to GitHub — and a stamp that only the goroutine writes is a stamp that
	// usually never gets written. Without this, fft asks GitHub on every single
	// invocation.
	if err := checker.Claim(); err != nil {
		// No cache to write to, so no way to remember we asked — and asking without
		// remembering is the thing this exists to prevent.
		return
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	// Buffered, so that the send cannot block: if the command has already finished
	// and nobody is left to receive, the goroutine still exits.
	results := make(chan update.State, 1)
	done := make(chan struct{})
	d.updates, d.updateDone = results, done

	go func() {
		defer close(done)

		// Its own deadline, rooted in the command's context so that Ctrl-C ends it
		// too. The goroutine's only exits are this deadline and the request
		// finishing; it holds nothing the command can see, and writes nothing but
		// the cache file.
		ctx, cancel := context.WithTimeout(ctx, update.Timeout)
		defer cancel()

		state, err := checker.Refresh(ctx)
		if err != nil {
			// Silently. A failed update check is not the user's problem — and the
			// cache was stamped before we asked, so we will not ask again today.
			return
		}
		results <- state
	}()
}

// reportUpdate prints the notice, if a newer release is known by the time the
// command has finished.
//
// The read is non-blocking on purpose. A check that has not come back yet is
// dropped, not waited for: it has written its result to the cache regardless, so
// the next command will say it. Nothing here may cost the user a millisecond.
func (d *Deps) reportUpdate() {
	if d.Update == nil {
		// startUpdateCheck decided the check must not run at all.
		return
	}

	state := d.updateState
	if state == nil {
		select {
		case s := <-d.updates:
			state = &s
		default:
			return
		}
	}

	if notice := d.Update.Notice(*state); notice != "" {
		// stderr, always: a notice on stdout would corrupt the one thing a script is
		// allowed to trust.
		d.Printer.Notef("%s", notice)
	}
}

// updateAllowed reports whether the background check may run.
//
// The check is skipped whenever its notice could not be shown, rather than run
// and thrown away: a CI job with no terminal has no use for a release check, and
// hitting GitHub on every build of every fork is how a shared rate limit gets
// spent.
func (d *Deps) updateAllowed(cmd *cobra.Command) bool {
	switch {
	case !buildinfo.IsRelease():
		// A `go build` has no version to compare, and a developer running the thing
		// they are working on does not need to be told to brew upgrade it.
		return false

	case os.Getenv(envNoUpdateCheck) != "":
		return false

	case d.Printer.Format() != output.Table:
		// -o json and -o yaml mean a machine is reading. Even on stderr, a notice is
		// noise a script did not ask for.
		return false

	case !d.terminal(cmd):
		return false

	case updateExempt(cmd):
		return false
	}

	// Headless mode must not read the config file, so it does not, and takes the
	// default. Nothing is lost: a CI run has no terminal and was ruled out above,
	// and a developer with FFT_* exported in their shell still gets the notice.
	if d.Ephemeral != nil {
		return true
	}

	cfg, err := d.LoadConfig()
	if err != nil {
		return false
	}
	return cfg.Settings.UpdateCheck
}

// terminal reports whether the notice would be seen: whether stderr, where it is
// written, is a terminal rather than a pipe or a file.
func (d *Deps) terminal(cmd *cobra.Command) bool {
	if d.Terminal != nil {
		return *d.Terminal
	}
	return prompt.IsTerminal(cmd.ErrOrStderr())
}

// updateExempt reports whether cmd is one the notice must never interrupt.
//
// `fft version` and `fft update` are about versions already; `fft completion`
// emits a shell script; and the two hidden completion commands run on every
// press of the TAB key, which is the last place to be starting an HTTP request.
//
// It is the *top-level* command that decides, not any ancestor. `fft facility
// update` and `fft stock update` are facility and stock commands that merely
// share a spelling with `fft update`, and matching them would quietly deny the
// notice — and the cache stamp with it — to anyone whose daily driver is one of
// them.
func updateExempt(cmd *cobra.Command) bool {
	// Walk up to the root's own child: `fft update check` is exempt because it
	// hangs below `fft update`, while `fft facility update` hangs below `facility`.
	top := cmd
	for top.Parent() != nil && top.Parent().Parent() != nil {
		top = top.Parent()
	}

	switch top.Name() {
	case "version", "completion", "update",
		cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd:
		return true
	}
	return false
}
