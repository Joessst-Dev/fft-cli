package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/buildinfo"
	"github.com/Joessst-Dev/fft-cli/internal/component"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

const componentInitLong = `Scaffold a new component.

Stamps a runnable skeleton — a valid component.yaml and an executable that does the
trivially-right thing — into <dir>/<name>, ready to install with:

  fft component install --path <dir>/<name>

Two kinds:

  command    adds 'fft <name>'; the skeleton prints its arguments and the FFT_
             environment fft handed it, so the contract is visible before you
             write any logic.
  transport  delivers emulator events to a broker; the skeleton answers the
             protocol's hello, refuses plan, and acks send.

Four languages (--lang): shell, python and node are interpreter scripts that run the
moment they are installed; go compiles to bin/, so it needs 'go build' first. The
default is shell for a command and go for a transport.

The emitted manifest is validated the same way 'fft component install' validates one,
so anything init produces is a manifest fft accepts.`

func newComponentInitCmd(deps *Deps) *cobra.Command {
	var (
		kind    = string(component.KindCommand)
		lang    string
		session = string(component.SessionNone)
		dir     = "."
	)

	cmd := &cobra.Command{
		Use:   "init <name>",
		Short: "Scaffold a new component",
		Long:  componentInitLong,
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := scaffoldFromFlags(cmd, args[0], kind, lang, session)
			if err != nil {
				return err
			}
			return runComponentInit(deps, s, dir)
		},
	}

	cmd.Flags().StringVar(&kind, "kind", kind, "What the component extends: command or transport")
	cmd.Flags().StringVar(&lang, "lang", "", "Language of the executable: shell, go, python or node (default depends on --kind)")
	cmd.Flags().StringVar(&session, "session", session, "For a command, the tenant session it receives: none, read or write")
	cmd.Flags().StringVar(&dir, "dir", dir, "Parent directory to create the component under")

	must(cmd.RegisterFlagCompletionFunc("kind", fixedCompletions(
		string(component.KindCommand), string(component.KindTransport))))
	must(cmd.RegisterFlagCompletionFunc("lang", fixedCompletions(
		component.LangShell, component.LangGo, component.LangPython, component.LangNode)))
	must(cmd.RegisterFlagCompletionFunc("session", fixedCompletions(
		string(component.SessionNone), string(component.SessionRead), string(component.SessionWrite))))

	return cmd
}

// scaffoldFromFlags turns the flag values into a validated [component.Scaffold],
// refusing the combinations that contradict themselves in the terms the user typed —
// before the manifest round-trip catches whatever slips through.
func scaffoldFromFlags(cmd *cobra.Command, name, kind, lang, session string) (component.Scaffold, error) {
	k := component.Kind(kind)
	if k != component.KindCommand && k != component.KindTransport {
		return component.Scaffold{}, exitcode.UsageError{Err: fmt.Errorf(
			"unknown --kind %q: want %s or %s", kind, component.KindCommand, component.KindTransport)}
	}

	if lang == "" {
		lang = component.DefaultLang(k)
	}
	switch lang {
	case component.LangShell, component.LangGo, component.LangPython, component.LangNode:
	default:
		return component.Scaffold{}, exitcode.UsageError{Err: fmt.Errorf(
			"unknown --lang %q: want %s, %s, %s or %s",
			lang, component.LangShell, component.LangGo, component.LangPython, component.LangNode)}
	}

	s := component.Scaffold{Name: name, Kind: k, Lang: lang}

	if k == component.KindTransport {
		// A transport declares no commands, so it has no session to receive. Ignoring the
		// flag silently would let 'init x --kind transport --session write' read as though
		// it did something; refusing it says which of the two the user has to drop.
		if cmd.Flags().Changed("session") {
			return component.Scaffold{}, exitcode.UsageError{Err: fmt.Errorf(
				"--session applies to a command component; a transport declares no commands")}
		}
	} else {
		sess := component.Session(session)
		if sess != component.SessionNone && sess != component.SessionRead && sess != component.SessionWrite {
			return component.Scaffold{}, exitcode.UsageError{Err: fmt.Errorf(
				"unknown --session %q: want %s, %s or %s",
				session, component.SessionNone, component.SessionRead, component.SessionWrite)}
		}
		s.Session = sess
	}

	// Pin fft in a Go transport's go.mod only from a released fft: a dev build's version
	// is no module version, so pinning it would produce a go.mod that will not resolve.
	// The scaffold's README covers the `go mod tidy` fallback either way.
	if s.Kind == component.KindTransport && s.Lang == component.LangGo && buildinfo.IsRelease() {
		s.FFTRequire = buildinfo.Version
	}

	return s, nil
}

// runComponentInit renders the scaffold and writes it, refusing to overwrite work.
func runComponentInit(deps *Deps, s component.Scaffold, dir string) error {
	files, err := s.Build()
	if err != nil {
		// The only way Build fails here is a name the manifest rules reject; flags were
		// checked above. That is the user's to fix, so it is a usage error.
		return exitcode.UsageError{Err: err}
	}

	target := filepath.Join(dir, s.Name)
	if err := refuseNonEmptyDir(target); err != nil {
		return err
	}

	for _, f := range files {
		full := filepath.Join(target, filepath.FromSlash(f.Name))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			return fmt.Errorf("create %s: %w", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, f.Data, f.Mode); err != nil {
			return fmt.Errorf("write %s: %w", full, err)
		}
		// WriteFile's mode is filtered by the umask, and a bin/ script that lost its
		// executable bit is one 'fft component install' would reject. Set it back.
		if err := os.Chmod(full, f.Mode); err != nil {
			return fmt.Errorf("set the mode of %s: %w", full, err)
		}
	}

	reportInit(deps, s, target, files)
	return nil
}

// reportInit says what was written and what to do next — all on stderr, because init
// emits no data and a sentence in the pipe is what the output contract prevents.
func reportInit(deps *Deps, s component.Scaffold, target string, files []component.File) {
	notef := deps.Printer.Notef

	notef("Scaffolded the %s component %s into %s:", s.Kind, s.Name, target)
	for _, f := range files {
		notef("  %s", f.Name)
	}
	notef("")

	if s.Lang == component.LangGo {
		notef("Build it, then install it:")
		notef("  cd %s && go mod tidy && go build -o bin/fft-%s .", target, s.Name)
		notef("  fft component install --path %s", target)
	} else {
		notef("Install it:")
		notef("  fft component install --path %s", target)
	}

	if s.Kind == component.KindCommand {
		notef("")
		notef("Then 'fft %s' runs it.", s.Name)
	}
}

// refuseNonEmptyDir stops init from writing over a directory that already has
// something in it — the name comes from a shell, and clobbering an author's work is
// not something a scaffolder gets to do.
func refuseNonEmptyDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read %s: %w", dir, err)
	}
	if len(entries) > 0 {
		return exitcode.UsageError{Err: fmt.Errorf("%s already exists and is not empty", dir)}
	}
	return nil
}

// fixedCompletions completes a flag with a fixed set of choices and no filenames.
func fixedCompletions(choices ...string) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return choices, cobra.ShellCompDirectiveNoFileComp
	}
}

// must panics on a wiring error that only a programming mistake can cause — a flag
// completion registered for a flag that does not exist. It fails at startup, in the
// tests, not for a user.
func must(err error) {
	if err != nil {
		panic(err)
	}
}
