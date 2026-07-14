package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/Joessst-Dev/fft-cli/internal/auth"
	"github.com/Joessst-Dev/fft-cli/internal/buildinfo"
	"github.com/Joessst-Dev/fft-cli/internal/client"
	"github.com/Joessst-Dev/fft-cli/internal/config"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/output"
	"github.com/Joessst-Dev/fft-cli/internal/prompt"
	"github.com/Joessst-Dev/fft-cli/internal/secrets"
	"github.com/Joessst-Dev/fft-cli/internal/update"
)

const rootLong = `fft is a command-line client for the fulfillmenttools API.

It replaces hand-rolled curl requests and Postman collections: set up your
projects once, switch between them freely, and let fft obtain and refresh
access tokens for you.

Every command's --help explains what the underlying endpoint does, which
permission it needs, and shows a sample request body.

Configuration lives in ~/.config/fft/config.yaml (mode 0600); credentials live in
your OS keychain. In CI, set FFT_BASE_URL, FFT_FIREBASE_API_KEY, FFT_EMAIL and
FFT_PASSWORD (or FFT_ID_TOKEN): fft then runs entirely from the environment and
touches neither the config file nor the keychain.

Every global flag can also be given as an environment variable: --output becomes
FFT_OUTPUT, --project becomes FFT_PROJECT, and so on.`

// VerifyFunc authenticates a project's credentials, returning the email address
// that actually worked.
//
// The real implementation is [verifyCredentials], a Firebase sign-in.
// `fft project add` calls it before it writes anything, so a project that cannot
// authenticate is never persisted. Specs replace it with a fake, which is the
// whole reason it is a seam rather than an inline call.
// debug is where --debug's dump goes: stderr, or nil when it is off.
type VerifyFunc func(ctx context.Context, p config.Project, password string, debug io.Writer) (email string, err error)

// TokenSourceFunc builds the TokenSource an authenticated command signs its
// requests with. [newTokenSource] is the real one; specs replace it with a static
// token so that a command spec never talks to Google.
type TokenSourceFunc func(p config.Project, store secrets.Store, now func() time.Time, debug io.Writer) (auth.TokenSource, error)

// Deps are the collaborators the commands need. root.go builds the real ones in
// PersistentPreRunE; specs construct a Deps of fakes and pass it to newRootCmd.
//
// Any field left nil is filled in with its real implementation, so a spec
// overrides only what it cares about. Printer is the exception: it is rebuilt
// from the command's streams on every run, because that is what lets a spec
// capture the output with cmd.SetOut.
type Deps struct {
	Config         *config.Store
	Secrets        secrets.Store
	Printer        *output.Printer
	Prompt         *prompt.Prompter
	Clock          func() time.Time
	Verify         VerifyFunc
	NewTokenSource TokenSourceFunc

	// In is the command's standard input: the source of an interactive answer, or
	// of a --password-stdin secret.
	In io.Reader

	// Debug is where --debug logs requests and responses. It is stderr, so that a
	// trace never contaminates `-o json | jq`, and nil when --debug is off.
	Debug io.Writer

	// Project is the value of --project / FFT_PROJECT, "" when neither is set.
	Project string

	// Ephemeral is the project synthesized from FFT_* variables in headless mode.
	// While it is set, no command may read the config file or the keychain.
	Ephemeral *config.Project

	// Timeout bounds a single command's API calls.
	Timeout time.Duration

	// Retry is the API client's retry policy. The zero value is the real one
	// ([client.Retry] fills in its own defaults); a spec replaces Sleep so that
	// exercising a retry costs no wall-clock time.
	Retry client.Retry

	// AssumeYes answers every confirmation prompt with yes (-y/--yes).
	AssumeYes bool

	// ReadOnlyFlag is --read-only as the user gave it, and nil when they did not
	// give it at all.
	//
	// It is a *bool because false is a thing a user can type and a thing fft must
	// refuse to honour: --read-only=false against a project configured read-only is
	// an attempt to *loosen*, and the flag can only ever tighten. Absence and false
	// are different answers, so absence cannot be spelled as the zero value.
	ReadOnlyFlag *bool

	// ReadOnlyEnv is FFT_READ_ONLY.
	//
	// It is read straight from the environment rather than through viper, and that
	// is the whole point of it. Viper would rank it *below* the flag, as a mere
	// default — so `--read-only=false` would silently switch off a guardrail a CI
	// job exported on purpose. Here it is a floor that the flag can raise and
	// cannot lower.
	ReadOnlyEnv bool

	// Update is the release checker behind the "a new version is available"
	// notice, and behind `fft update check`. nil means the real one, reading and
	// writing ~/.cache/fft/update.json; a spec points it at a fake GitHub.
	//
	// Setting it does not force the check to run: the notice is suppressed on a
	// dev build, a pipe, -o json and the rest of it either way.
	Update *update.Checker

	// Terminal overrides terminal detection for the update notice. nil means "ask
	// stderr", which is what production does.
	//
	// It is a seam in the same spirit as prompt.WithInteractive: a bytes.Buffer
	// has no file descriptor, so without it a spec could only ever prove that the
	// notice is *suppressed*, never that it appears and never that it lands on
	// stderr — which is the whole contract.
	Terminal *bool

	// cfg caches the parsed config file, so that a command reading it twice does
	// not read the disk twice and a mutation followed by a save sees its own
	// writes.
	cfg *config.Config

	// updates carries the background release check's result to
	// PersistentPostRun. It is buffered and read without blocking: a check that
	// has not finished is dropped, never waited for.
	updates <-chan update.State

	// updateState is the answer a fresh cache gave, which needed no goroutine at
	// all. nil means "ask updates, if anything was started".
	updateState *update.State

	// updateDone is closed when the background check's goroutine has exited. nil
	// when no goroutine was started.
	//
	// Nothing in production waits on it: the process exits and the goroutine dies
	// with it, which is the entire point. It exists because a goroutine with no way
	// to be awaited is one that goes on writing files after whoever started it has
	// finished — under the specs that is a race with Ginkgo's temp-directory
	// cleanup, and a goroutine that can be joined is simply a goroutine one owns.
	updateDone chan struct{}
}

// LoadConfig returns the parsed config file, reading it at most once.
func (d *Deps) LoadConfig() (*config.Config, error) {
	if d.cfg != nil {
		return d.cfg, nil
	}

	cfg, err := d.Config.Load()
	if err != nil {
		return nil, err
	}
	d.cfg = cfg
	return cfg, nil
}

// SaveConfig persists the config file.
func (d *Deps) SaveConfig(cfg *config.Config) error {
	if err := d.Config.Save(cfg); err != nil {
		return err
	}
	d.cfg = cfg
	return nil
}

// ActiveProject resolves the project the command should act on: the headless
// project synthesized from the environment, else --project / FFT_PROJECT, else
// the config file's activeProject.
//
// An explicit --project beats the headless project, so that a developer with
// FFT_* exported in their shell can still reach a configured project. Otherwise
// the environment wins, which is what makes a CI job deterministic.
func (d *Deps) ActiveProject() (config.Project, error) {
	if d.Ephemeral != nil && (d.Project == "" || d.Project == d.Ephemeral.Name) {
		return *d.Ephemeral, nil
	}

	cfg, err := d.LoadConfig()
	if err != nil {
		return config.Project{}, err
	}
	return cfg.Resolve(d.Project)
}

// Context bounds the command's work by --timeout. A zero timeout means no bound.
func (d *Deps) Context(cmd *cobra.Command) (context.Context, context.CancelFunc) {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	if d.Timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, d.Timeout)
}

// newRootCmd builds the command tree against deps. deps must not be nil; any of
// its fields may be, and PersistentPreRunE fills those in.
func newRootCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fft",
		Short: "Command-line client for the fulfillmenttools API",
		Long:  rootLong,

		// Errors are printed and classified by main; usage noise on a runtime
		// failure just buries the actual message.
		SilenceUsage:  true,
		SilenceErrors: true,

		Version: buildinfo.Version,

		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			if err := deps.complete(cmd); err != nil {
				return err
			}
			// Cobra checks its own flag groups — but only after this hook, and it
			// returns the failure straight to the caller rather than through
			// SetFlagErrorFunc. So `--file` together with `--data` would exit 1, as
			// though something had gone wrong out in the world, when it is a bad
			// command line like any other. Caught here, it exits 2 like the rest of
			// them.
			//
			// And before the gate below, because a command line that contradicts
			// itself is not a request to write. `--file x --data y` against a
			// read-only project would otherwise exit 10 — sending whoever read that
			// code, human or agent, to argue about a permission they do not need,
			// over a command that was never going to be sent either way.
			if err := cmd.ValidateFlagGroups(); err != nil {
				return exitcode.UsageError{Err: err}
			}

			// Before the update check and long before the token: a write refused
			// against a read-only project must start no goroutine, open no keychain
			// and sign in to nothing.
			if err := deps.guard(cmd); err != nil {
				return err
			}

			deps.startUpdateCheck(cmd)
			return nil
		},

		// Cobra runs this only when the command succeeded, which is the only time a
		// "by the way, there is a new version" is worth reading.
		PersistentPostRun: func(*cobra.Command, []string) {
			deps.reportUpdate()
		},
	}

	// Bad flags are a usage problem (exit 2), not a generic failure (exit 1).
	// Cobra reports them as plain errors, so we tag them on the way out.
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return exitcode.UsageError{Err: err}
	})

	// The flag values are read back through viper, which owns the
	// flag → env → config → default precedence chain, so nothing binds them to
	// local variables here.
	pf := cmd.PersistentFlags()
	pf.String("project", "", "Project to act on (default: the active project)")
	pf.StringP("output", "o", string(output.Table),
		"Output format: "+strings.Join(output.Formats(), ", "))
	pf.Bool("no-color", false, "Disable coloured output")
	pf.Bool("debug", false, "Log requests and responses to stderr")
	pf.Duration("timeout", 30*time.Second, "Timeout for a single command")
	pf.BoolP("yes", "y", false, "Assume yes for every confirmation prompt")
	pf.Bool("no-keyring", false, "Store credentials in a 0600 file instead of the OS keychain")
	pf.Bool("read-only", false, "Refuse any request that would change data (can only tighten, never loosen)")

	if err := cmd.RegisterFlagCompletionFunc("output", completeOutput); err != nil {
		// The only way this fails is a typo in the flag name above — a
		// programming error we would rather find at startup than never.
		panic(fmt.Sprintf("register --output completion: %v", err))
	}

	// The groups exist because the tree does not fit on a screen without them: six
	// hand-written commands and sixty tag-derived ones, in one flat list, would be a
	// help page nobody reads.
	cmd.AddGroup(
		&cobra.Group{ID: groupCore, Title: "Core commands:"},
		&cobra.Group{ID: groupResource, Title: "API operations, by resource:"},
	)
	cmd.SetHelpCommandGroupID(groupCore)
	cmd.SetCompletionCommandGroupID(groupCore)

	for _, c := range []*cobra.Command{
		newVersionCmd(deps),
		newProjectCmd(deps),
		newAuthCmd(deps),
		newPingCmd(deps),
		newAPICmd(deps),
		newSkillCmd(deps),
		newUpdateCmd(deps),
	} {
		c.GroupID = groupCore
		cmd.AddCommand(c)
	}

	for _, c := range []*cobra.Command{
		newFacilityCmd(deps),
		newListingCmd(deps),
		newStockCmd(deps),
	} {
		c.GroupID = groupResource
		cmd.AddCommand(c)
	}

	// Last, and it has to be last: a generated command is registered only for an
	// operation no curated command has claimed, and it learns what has been claimed
	// by walking the tree the curated commands were just added to.
	addGeneratedCommands(deps, cmd)

	// The help function is the spec-aware one for every command carrying an
	// operationId, and cobra's own for the rest. Setting it on the root is enough:
	// cobra inherits it down the tree.
	installHelp(deps, cmd)

	return cmd
}

func completeOutput(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return output.Formats(), cobra.ShellCompDirectiveNoFileComp
}

// registerEnumCompletion makes a flag with a fixed set of values complete to it,
// so that a user never has to remember whether it is MANAGED_FACILITY or
// MANAGED-FACILITY.
func registerEnumCompletion(cmd *cobra.Command, flag string, values []string) {
	err := cmd.RegisterFlagCompletionFunc(flag,
		func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
			return values, cobra.ShellCompDirectiveNoFileComp
		})
	if err != nil {
		// The only way this fails is a flag name that does not exist — a programming
		// error, and one better found at startup than never.
		panic(fmt.Sprintf("register --%s completion on %q: %v", flag, cmd.Name(), err))
	}
}

// complete fills in whatever the caller did not supply. It runs before every
// subcommand, so a RunE can assume every field is set.
func (d *Deps) complete(cmd *cobra.Command) error {
	if d.Clock == nil {
		d.Clock = time.Now
	}
	if d.Verify == nil {
		d.Verify = verifyCredentials
	}
	if d.NewTokenSource == nil {
		d.NewTokenSource = newTokenSource
	}
	if d.In == nil {
		d.In = cmd.InOrStdin()
	}
	if d.Prompt == nil {
		// Questions go to stderr: a command that asks something must still be
		// safe to pipe.
		d.Prompt = prompt.New(d.In, cmd.ErrOrStderr())
	}

	if d.Config == nil {
		path, err := config.DefaultPath()
		if err != nil {
			return config.NewError(err, "Set XDG_CONFIG_HOME to a writable directory.")
		}
		d.Config = config.NewStore(path)
	}

	// Headless mode is decided before anything reads the disk, because the whole
	// point of it is that nothing does.
	if d.Ephemeral == nil {
		if p, ok := config.FromEnv(os.LookupEnv); ok {
			d.Ephemeral = &p
		}
	}

	v, err := d.bindFlags(cmd)
	if err != nil {
		return err
	}

	d.Project = v.GetString("project")
	d.Timeout = v.GetDuration("timeout")
	d.AssumeYes = v.GetBool("yes")

	// Assigned on every run, absence included: the spec harness reuses one Deps
	// across commands, and a --read-only left over from the previous run would be a
	// gate nobody asked for.
	//
	// Read off the *root's* persistent set, never cmd.Flags(): `fft project add`
	// declares a local --read-only, which cobra lets shadow the global one there,
	// and reading it here would mistake "the project I am configuring is read-only"
	// for "block writes in this session". And read off the flag rather than viper,
	// so that FFT_READ_ONLY cannot be talked down to a default — see [Deps.ReadOnlyEnv].
	d.ReadOnlyFlag = nil
	if f := cmd.Root().PersistentFlags().Lookup("read-only"); f != nil && f.Changed {
		d.ReadOnlyFlag = ptr(f.Value.String() == "true")
	}
	d.ReadOnlyEnv = config.ReadOnlyFromEnv(os.LookupEnv)

	// The trace goes to stderr, never to stdout: `fft facility list -o json --debug
	// | jq` must still be piping JSON and nothing else.
	if d.Debug == nil && v.GetBool("debug") {
		d.Debug = cmd.ErrOrStderr()
	}

	format, err := output.ParseFormat(v.GetString("output"))
	if err != nil {
		return exitcode.UsageError{Err: err}
	}
	// The printer is rebuilt from the command's streams on every run, so a spec
	// that calls cmd.SetOut captures the output without constructing one.
	out := cmd.OutOrStdout()
	d.Printer = output.New(out, cmd.ErrOrStderr(), format, useColor(v, out))

	if d.Secrets == nil {
		if d.Secrets, err = d.openSecrets(v.GetBool("no-keyring")); err != nil {
			return err
		}
	}
	return nil
}

// bindFlags builds the flag → env → config → default precedence chain.
//
// Viper does this one job well, and is used for nothing else: it lower-cases
// every key it touches, which would quietly corrupt `activeProject` and
// `baseUrl` if it were let anywhere near the config file itself.
func (d *Deps) bindFlags(cmd *cobra.Command) (*viper.Viper, error) {
	v := viper.New()
	v.SetEnvPrefix("FFT")
	// So that --no-color reads FFT_NO_COLOR rather than FFT_NO-COLOR.
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	v.AutomaticEnv()

	var bindErr error
	cmd.Root().PersistentFlags().VisitAll(func(f *pflag.Flag) {
		if err := v.BindPFlag(f.Name, f); err != nil && bindErr == nil {
			bindErr = fmt.Errorf("bind --%s: %w", f.Name, err)
		}
	})
	if bindErr != nil {
		return nil, bindErr
	}

	// The config file supplies the default output format — but only when there is
	// a config file to read at all. A headless run must not touch it.
	if d.Ephemeral == nil {
		cfg, err := d.LoadConfig()
		if err != nil {
			return nil, err
		}
		if cfg.Settings.Output != "" {
			v.SetDefault("output", cfg.Settings.Output)
		}
	}

	return v, nil
}

// openSecrets picks the credential store. In headless mode it is the read-only
// environment: a GitHub Linux runner has no Secret Service, so there the keychain
// is not merely inconvenient, it does not exist.
func (d *Deps) openSecrets(noKeyring bool) (secrets.Store, error) {
	if d.Ephemeral != nil {
		return secrets.NewEnv(os.LookupEnv), nil
	}
	return secrets.Open(noKeyring)
}

// useColor decides whether to colour output. NO_COLOR (the cross-tool
// convention) and a non-terminal destination both disable it: escape codes in a
// pipe are corruption, not decoration.
//
// The question is asked of out — the writer the printer will actually use — and
// not of os.Stdout. They are the same thing in production and different things
// under test, and asking the wrong one is how a spec's golden output acquires
// colour codes on the day someone runs the suite from a terminal.
func useColor(v *viper.Viper, out io.Writer) bool {
	if v.GetBool("no-color") {
		return false
	}
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	return prompt.IsTerminal(out)
}

// usageArgs tags a positional-argument validator's failures as usage errors, so
// that "wrong number of arguments" also exits 2 rather than 1.
func usageArgs(validator cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := validator(cmd, args); err != nil {
			return exitcode.UsageError{Err: err}
		}
		return nil
	}
}
