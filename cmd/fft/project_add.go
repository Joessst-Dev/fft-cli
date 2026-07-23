package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/config"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/prompt"
	"github.com/Joessst-Dev/fft-cli/internal/secrets"
)

const projectAddLong = `Configure a fulfillmenttools project.

Run without flags on a terminal and fft asks for each value in turn, masking the
password. Pass every flag and --password-stdin and it runs unattended, which is
what a provisioning script wants.

There is deliberately no --password flag: a password on the command line is
recorded in your shell history and visible in the process list to every other
user on the machine. Pipe it in instead:

  fft project add prd \
    --base-url https://acme.api.fulfillmenttools.com \
    --api-key AIza... \
    --project-id acme --env prd --username warehouse-bot \
    --password-stdin < password.txt

The base URL is stored exactly as you give it. fft never derives it from the
project id, because the official documentation disagrees with itself about
whether the host is "{projectId}.api…" or "ocff-{projectId}.api…".

You may give either --email (used verbatim) or --username, from which fft builds
the synthetic address fulfillmenttools issues, {username}@ocff-{projectId}-{env}.com.

The fulfillmenttools API key (a Firebase Web API key) is treated as sensitive: it
goes to the keychain alongside the password and tokens, each under its own entry,
and is never written to the config file. It grants nothing on its own and is sent
only to Google's identity endpoints — never to fulfillmenttools.`

// addFlags are the non-interactive inputs to `project add`.
type addFlags struct {
	baseURL       string
	apiKey        string
	email         string
	username      string
	tenant        string
	projectID     string
	environment   string
	passwordStdin bool
	force         bool
	readOnly      bool
}

func newProjectAddCmd(deps *Deps) *cobra.Command {
	var flags addFlags

	cmd := &cobra.Command{
		Use:   "add [name]",
		Short: "Configure a project",
		Long:  projectAddLong,
		Args:  usageArgs(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			var name string
			if len(args) == 1 {
				name = args[0]
			}
			return runProjectAdd(cmd, deps, &flags, name)
		},
	}

	f := cmd.Flags()
	f.StringVar(&flags.baseURL, "base-url", "", "API root, e.g. https://acme.api.fulfillmenttools.com")
	f.StringVar(&flags.apiKey, "api-key", "", "fulfillmenttools API key")
	f.StringVar(&flags.email, "email", "", "Email address to sign in with (use instead of --username)")
	f.StringVar(&flags.username, "username", "", "Login name; the email is derived from it")
	f.StringVar(&flags.tenant, "tenant", "", "Tenant name (informational)")
	f.StringVar(&flags.projectID, "project-id", "", "fulfillmenttools project id")
	f.StringVar(&flags.environment, "env", "", "Environment, e.g. pre or prd")
	f.BoolVar(&flags.passwordStdin, "password-stdin", false, "Read the password from stdin")
	f.BoolVar(&flags.force, "force", false, "Overwrite an existing project of the same name")

	// This local --read-only shadows the root's persistent one on this command, which
	// cobra allows and which is what we want: on `project add`, --read-only can only
	// sensibly mean "the project I am configuring is read-only", not "block writes in
	// this session". It is also why the gate reads the *root's* flag set and never
	// cmd.Flags() — see [Deps.complete].
	f.BoolVar(&flags.readOnly, "read-only", false, "Refuse every request that would change this project")

	cmd.MarkFlagsMutuallyExclusive("email", "username")

	return cmd
}

func runProjectAdd(cmd *cobra.Command, deps *Deps, flags *addFlags, name string) error {
	if err := deps.requireMutableConfig("add"); err != nil {
		return err
	}

	cfg, err := deps.LoadConfig()
	if err != nil {
		return err
	}

	// Nothing may be prompted for once stdin has been handed to --password-stdin:
	// the password is the whole of stdin, so there is no line left to read an
	// answer from.
	interactive := deps.Prompt.Interactive() && !flags.passwordStdin

	project, password, err := gatherProject(deps, flags, name, interactive)
	if err != nil {
		return err
	}

	existing, exists := cfg.Find(project.Name)
	if exists && !flags.force {
		return config.NewError(
			fmt.Errorf("project %q is already configured", project.Name),
			fmt.Sprintf("Pass --force to overwrite it, or 'fft project remove %s' first.", project.Name),
		)
	}

	// Overwriting a read-only project does not quietly make it writable. --force is
	// for rotating a password or fixing a typo in the base URL, and this command
	// rebuilds the project from its flags — so without this the guardrail on prod
	// would come off as a side effect of a command that never mentioned it. Saying
	// --read-only=false is how you take it off, and that has to be said.
	//
	// And saying it is not enough on its own: taking the mark off here re-arms writes
	// against a protected tenant, which is the same irreversible step that
	// `project read-only --off` asks about, so it asks the same question. Otherwise
	// the confirmation on that command would be worth nothing — this would be the way
	// around it.
	if exists && existing.ReadOnly {
		if !cmd.Flags().Changed("read-only") {
			project.ReadOnly = true
		} else if !project.ReadOnly {
			confirmed, err := confirmWritable(deps, project.Name)
			if err != nil {
				return err
			}
			if !confirmed {
				deps.Printer.Notef("Aborted; %q is unchanged and still read-only.", project.Name)
				return nil
			}
		}
	}

	// M3 replaces this nil with the real Firebase sign-in. Verification runs
	// before anything is written, so a project that cannot authenticate is never
	// persisted — and the email that actually worked is what gets stored, rather
	// than the one we guessed at.
	if deps.Verify != nil {
		ctx, cancel := deps.Context(cmd)
		defer cancel()

		email, err := deps.Verify(ctx, project, password, deps.Debug)
		if err != nil {
			return fmt.Errorf("verify the credentials for %q: %w", project.Name, err)
		}
		project.Email = email
	}

	if err := persistProject(deps, cfg, project, password); err != nil {
		return err
	}

	return renderAdded(deps, cfg, project)
}

// gatherProject assembles the project from flags, falling back to prompts on a
// terminal and to a precise "you are missing these flags" error without one.
func gatherProject(deps *Deps, flags *addFlags, name string, interactive bool) (config.Project, string, error) {
	p := config.Project{
		Name:           strings.TrimSpace(name),
		BaseURL:        strings.TrimSpace(flags.baseURL),
		FirebaseAPIKey: strings.TrimSpace(flags.apiKey),
		Email:          strings.TrimSpace(flags.email),
		Username:       strings.TrimSpace(flags.username),
		Tenant:         strings.TrimSpace(flags.tenant),
		ProjectID:      strings.TrimSpace(flags.projectID),
		Environment:    strings.TrimSpace(flags.environment),
		ReadOnly:       flags.readOnly,
	}

	if interactive {
		if err := promptProject(deps.Prompt, &p); err != nil {
			return config.Project{}, "", err
		}
	}

	if p.Email == "" {
		p.Email = config.CandidateEmail(p.Username, p.ProjectID, p.Environment)
	}

	if err := requireFields(p, interactive); err != nil {
		return config.Project{}, "", err
	}

	normalized, err := config.NormalizeBaseURL(p.BaseURL)
	if err != nil {
		return config.Project{}, "", exitcode.UsageError{Err: err}
	}
	p.BaseURL = normalized

	password, err := readPassword(deps, flags, interactive)
	if err != nil {
		return config.Project{}, "", err
	}

	return p, password, nil
}

func promptProject(p *prompt.Prompter, project *config.Project) error {
	fields := []struct {
		label    string
		value    *string
		required bool
	}{
		{"Project name", &project.Name, true},
		{"Base URL (e.g. https://acme.api.fulfillmenttools.com)", &project.BaseURL, true},
		{"fulfillmenttools API key", &project.FirebaseAPIKey, true},
		{"fulfillmenttools project id", &project.ProjectID, false},
		{"Environment (pre or prd)", &project.Environment, false},
	}

	for _, f := range fields {
		// A value already given as a flag is not asked for again.
		if *f.value != "" {
			continue
		}

		ask := p.Line
		if f.required {
			ask = func(label, _ string) (string, error) { return p.Required(label) }
		}

		val, err := ask(f.label, "")
		if err != nil {
			return err
		}
		*f.value = strings.TrimSpace(val)
	}

	// The interactive path asks for the login name, never an email: the address
	// that authenticates is the synthetic {username}@ocff-{projectId}-{env}.com,
	// and users typing their own corporate email instead was the common way to a
	// failed sign-in. A genuinely non-synthetic account uses --email, so an "@"
	// here is almost certainly the mistake — reject it and re-ask. The username is
	// skipped only when --username or --email already settled it.
	if project.Email == "" && project.Username == "" {
		username, err := p.Validated("Username (login name)", func(v string) error {
			if strings.Contains(v, "@") {
				return errors.New("enter the short login name, not an email address (use --email for a full sign-in address)")
			}
			return nil
		})
		if err != nil {
			return err
		}
		project.Username = strings.TrimSpace(username)
	}
	return nil
}

// requireFields turns a half-filled project into the shortest useful error. On a
// terminal that cannot happen — the prompts insisted — so this is really the
// non-interactive path's message, and it names every missing flag at once rather
// than making the user rerun the command six times.
func requireFields(p config.Project, interactive bool) error {
	missing := make([]string, 0, 4)
	for _, f := range []struct {
		flag  string
		value string
	}{
		{"name", p.Name},
		{"--base-url", p.BaseURL},
		{"--api-key", p.FirebaseAPIKey},
	} {
		if f.value == "" {
			missing = append(missing, f.flag)
		}
	}

	if p.Email == "" {
		// The email is either given outright or derived, and the derivation needs
		// all three parts — so say which one it is short of.
		switch {
		case p.Username == "":
			missing = append(missing, "--email or --username")
		default:
			missing = append(missing, "--project-id and --env (needed to build the email from --username), or --email")
		}
	}

	if len(missing) == 0 {
		return nil
	}

	if interactive {
		return fmt.Errorf("missing required values: %s", strings.Join(missing, ", "))
	}
	return exitcode.UsageError{Err: fmt.Errorf(
		"stdin is not a terminal, so fft cannot prompt: pass %s", strings.Join(missing, ", "))}
}

func readPassword(deps *Deps, flags *addFlags, interactive bool) (string, error) {
	if flags.passwordStdin {
		password, err := prompt.ReadAll(deps.In)
		if err != nil {
			return "", err
		}
		if password == "" {
			return "", exitcode.UsageError{Err: errors.New("--password-stdin was given but stdin was empty")}
		}
		return password, nil
	}

	if !interactive {
		return "", exitcode.UsageError{Err: errors.New(
			"stdin is not a terminal, so fft cannot prompt for the password: pass --password-stdin")}
	}

	for {
		password, err := deps.Prompt.Password("Password")
		if err != nil {
			return "", err
		}
		if password != "" {
			return password, nil
		}
		deps.Printer.Notef("The password cannot be empty.")
	}
}

// persistProject writes the secrets first and the config second.
//
// The order matters. A project in the config file with no credential behind it
// is a project every later command fails on, so if a keychain write fails we
// have written nothing; and if the config write fails after the keychain
// succeeded, the orphaned secrets are removed rather than left behind for a
// later `project add` of the same name to silently inherit.
//
// The API key goes to the store alongside the password: it is sensitive too, and
// Project.FirebaseAPIKey is yaml:"-", so the config write below never records it.
func persistProject(deps *Deps, cfg *config.Config, project config.Project, password string) error {
	if err := deps.Secrets.Set(secrets.Key(project.Name, secrets.KindPassword), password); err != nil {
		return fmt.Errorf("store the password for %q: %w", project.Name, err)
	}
	if err := deps.Secrets.Set(secrets.Key(project.Name, secrets.KindAPIKey), project.FirebaseAPIKey); err != nil {
		if delErr := secrets.DeleteAll(deps.Secrets, project.Name); delErr != nil {
			return errors.Join(fmt.Errorf("store the API key for %q: %w", project.Name, err), delErr)
		}
		return fmt.Errorf("store the API key for %q: %w", project.Name, err)
	}

	cfg.Upsert(project)
	// The first project configured becomes the active one; there is nothing else
	// it could sensibly be, and making the user run `project use` immediately
	// afterwards is a step with no decision in it.
	if cfg.ActiveProject == "" {
		cfg.ActiveProject = project.Name
	}

	if err := deps.SaveConfig(cfg); err != nil {
		if delErr := secrets.DeleteAll(deps.Secrets, project.Name); delErr != nil {
			return errors.Join(err, fmt.Errorf("roll back the stored password: %w", delErr))
		}
		return err
	}
	return nil
}

func renderAdded(deps *Deps, cfg *config.Config, project config.Project) error {
	active := cfg.ActiveProject == project.Name
	view := newProjectView(project, active, deps.Secrets)

	if err := deps.Printer.Render(projectRows([]projectView{view}), view); err != nil {
		return err
	}

	// Status lines go to stderr, so that `project add -o json` still emits nothing
	// but JSON on stdout.
	if active {
		deps.Printer.Notef("Project %q added and is now active.", project.Name)
	} else {
		deps.Printer.Notef("Project %q added. Run 'fft project use %s' to switch to it.", project.Name, project.Name)
	}

	if project.ReadOnly {
		deps.Printer.Notef("It is read-only: fft will refuse every request that would change it.")
	}
	return nil
}
