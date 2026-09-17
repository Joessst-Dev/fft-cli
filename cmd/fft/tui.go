package main

import (
	"errors"
	"os"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/prompt"
	"github.com/Joessst-Dev/fft-cli/internal/secrets"
	"github.com/Joessst-Dev/fft-cli/internal/tui"
)

const tuiLong = `Open fft's interactive mode: a full-screen UI in the terminal.

Every request the UI sends is an fft command line, run through the same command
tree as in a shell: the read-only gate, the exit codes and the validation all
apply unchanged.

The first screen manages projects: switch, protect, remove and add them, and
refresh the current token. The Operations screen lists every operation of the
API by tag; press / to search it, D to describe one and enter to fill in its
request. The Request screen builds the command from its flags, opens the body in
$VISUAL (then $EDITOR) and asks before it sends a write. A command that asks its
own question, such as facility delete, asks it in the UI once it has looked up
what it is about to change; one that cannot be undone wants its verb typed back.
t saves the body as a template for the operation. The Response screen shows the exit code,
the HTTP status, the JSON, a table where the command prints one, and stderr; r
sends the request again and s saves the body to a file.

The Templates screen lists the saved templates. p fills in a template's
parameters, R renders it, and S renders it and hands the body to the operation's
command: anything the render warns about, such as a template saved under another
project, is shown first, and a write is still asked about before it goes. Every
question about sending a body shows its start, and for a body from a template names
the template, its scope and, for a project template, its file. What is rendered is
the template on screen: if its file changed after it was opened, nothing is sent and
the template is read again. S asks before it replaces a Request form holding work
you have not sent. x removes a template.

The History screen lists the requests sent to the current project, newest first;
t switches to the operations used most, and enter fills in the Request form the way
a request was sent. Values history does not keep, such as header values and
request bodies, are left empty. c clears the history, after asking. When requests
are not being recorded, the screen says why. The Operations screen stars each
operation with how often the current project sent it.

The Roles screen shows your roles on the current project, their permissions, and
the operations none of them permits. The Operations screen greys those, and the
question before a request warns that you appear to lack the permission. It is only
a hint: a role can be limited to some facilities, so a greyed operation can still
be sent, and the tenant decides.

Press ? for every key, i for the commands that are running, and y to copy the fft
command the focused action stands for. Secrets you type and request bodies are
passed to that command on stdin, never on its command line.

The UI is drawn on stderr and needs a terminal on both stdin and stderr. It
writes nothing to stdout.`

// newTUICmd builds `fft tui`.
func newTUICmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Browse and send requests in an interactive terminal UI",
		Long:  tuiLong,
		Args:  usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Refused up front rather than left to the renderer: in a pipe or a CI job
			// a full-screen UI is not degraded, it is garbage in a log that nobody can
			// answer.
			if !deps.interactiveTerminal(cmd) {
				return exitcode.UsageError{Err: errors.New(
					"fft tui needs a terminal on stdin and stderr; in a script, run the fft command itself")}
			}

			announceSecretsWarnings(deps.Secrets)

			session := sessionFlags(cmd, deps)
			// Closing cancels whatever the user left running when they quit, and waits
			// for it: a write interrupted mid-flight still reports how it ended, to a
			// history entry if nowhere else.
			runner := newCLIRunner(cmd.Context(), deps, session)
			defer runner.Close()
			runner.SetProject(deps.Project)

			start := deps.StartTUI
			if start == nil {
				start = tui.Run
			}
			return start(cmd.Context(), tui.Options{
				Runner:   runner,
				Catalog:  runner.Catalog(),
				History:  runner.History(),
				In:       cmd.InOrStdin(),
				Out:      cmd.ErrOrStderr(),
				Project:  deps.Project,
				Headless: deps.Ephemeral != nil,
				ReadOnly: session.readOnly || deps.ReadOnlyEnv,
				Color:    uiColor(cmd),
			})
		},
	}
}

// announceSecretsWarnings has the credential store say now whatever it has to say
// about itself, while stderr is still a plain stream.
//
// The runs share this command's store, and its warning sink writes to this
// command's stderr — which, once the UI starts, is the screen. The file store warns
// about loose permissions at most once, on its first read, so reading it here is
// what keeps that warning out of the frame and in the scrollback the user returns
// to. The keychain has nothing to warn about, and asking it anything now could
// raise a system dialog before the UI has even appeared.
func announceSecretsWarnings(store secrets.Store) {
	if store == nil || store.Kind() != "file" {
		return
	}
	// Only the read matters, not its answer: a file that cannot be read fails again,
	// with its context, in the first run that needs a credential from it.
	_, _ = store.Get(secrets.Key("", secrets.KindAPIKey))
}

// sessionFlags is what the global flags given to `fft tui` itself mean for the runs
// inside it. Each run parses only its own command line, so a session flag reaches
// it only if it is carried over here — and which ones are is a decision per flag:
//
//   - --read-only is carried over as a floor under every run. It is the one flag
//     whose absence from a run would be dangerous.
//   - --timeout is carried over as a default, which a run's own --timeout replaces.
//   - --project selects the project the runner starts on; see [cliRunner.SetProject].
//   - --no-keyring is carried over by construction: the runs share this command's
//     credential store, which it already chose.
//   - -y/--yes is not carried over: a session that answered yes to everything in
//     advance would be a UI that never asks. A command that asks puts its question
//     to the user through the UI. Only the Projects screen's remove and allow-writes
//     add --yes, because their typed-name dialogs are that question.
//   - --debug is not carried over. A trace of every request would bury each run's
//     own stderr, which the UI shows with its response; the UI can ask for one on
//     the request being investigated.
//   - -o and --no-color mean nothing to a run, which always speaks JSON to the UI.
func sessionFlags(cmd *cobra.Command, deps *Deps) uiRun {
	session := uiRun{readOnly: deps.ReadOnlyFlag != nil && *deps.ReadOnlyFlag}
	if rootFlagChanged(cmd, "timeout") {
		session.timeout = ptr(deps.Timeout)
	}
	return session
}

// uiColor reports whether the UI may draw in colour. It is asked of the UI's own
// stream rather than taken from the printer, whose answer is about stdout — and a
// UI on stderr with stdout redirected is still a UI on a terminal. The switches are
// the ones every other command honours: --no-color, FFT_NO_COLOR and NO_COLOR.
func uiColor(cmd *cobra.Command) bool {
	if f := cmd.Root().PersistentFlags().Lookup("no-color"); f != nil && f.Value.String() == "true" {
		return false
	}
	if off, err := strconv.ParseBool(os.Getenv("FFT_NO_COLOR")); err == nil && off {
		return false
	}
	return os.Getenv("NO_COLOR") == ""
}

// interactiveTerminal reports whether someone is at a terminal to drive the UI:
// keystrokes arrive on stdin, and the UI is drawn on stderr. [Deps.Terminal]
// overrides the answer, as it does for the update notice.
func (d *Deps) interactiveTerminal(cmd *cobra.Command) bool {
	if d.Terminal != nil {
		return *d.Terminal
	}
	return prompt.IsTerminal(cmd.InOrStdin()) && prompt.IsTerminal(cmd.ErrOrStderr())
}
