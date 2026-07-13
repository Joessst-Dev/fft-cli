package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/config"
	"github.com/Joessst-Dev/fft-cli/internal/output"
	"github.com/Joessst-Dev/fft-cli/internal/secrets"
)

const projectLong = `Manage the fulfillmenttools projects fft can talk to.

A project is one tenant plus the Firebase project that authenticates against it.
Its non-secret settings live in ~/.config/fft/config.yaml; its password and
tokens live in your OS keychain, one entry per secret.

The base URL is stored, never derived from the project id — the official docs
disagree with themselves about the host format, so fft refuses to guess.`

func newProjectCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "project",
		Short:   "Manage projects",
		Long:    projectLong,
		Aliases: []string{"projects"},
		Args:    usageArgs(cobra.NoArgs),

		// A bare `fft project` is a user asking what the group can do.
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}

	cmd.AddCommand(
		newProjectAddCmd(deps),
		newProjectListCmd(deps),
		newProjectUseCmd(deps),
		newProjectRemoveCmd(deps),
		newProjectCurrentCmd(deps),
	)

	return cmd
}

// requireMutableConfig refuses the config-mutating subcommands while headless
// mode is active.
//
// In headless mode the project comes from FFT_* variables and lives only in
// memory. Letting `project add` write a config file that the very next command
// would ignore — because the environment still wins — is worse than refusing:
// the user would think they had configured something and they would not have.
func (d *Deps) requireMutableConfig(verb string) error {
	if d.Ephemeral == nil {
		return nil
	}
	return config.NewError(
		fmt.Errorf("cannot %s a project: fft is running from the environment (%s is set)", verb, config.EnvBaseURL),
		"Unset FFT_BASE_URL, FFT_FIREBASE_API_KEY, FFT_EMAIL and FFT_PASSWORD/FFT_ID_TOKEN to manage the config file.",
	)
}

// credentialStatus describes where a project's credentials are, for the
// CREDENTIAL column of `fft project list`: the store's name when it holds
// something, "missing" when the config knows about a project that nothing can
// sign in as.
func credentialStatus(store secrets.Store, project config.Project) string {
	if project.Ephemeral {
		return "env"
	}
	if secrets.Has(store, project.Name) {
		return store.Kind()
	}
	return "missing"
}

// projectView is what `project add`, `list` and `current` render. It exists so
// that the table and the JSON are two projections of one value rather than two
// hand-maintained lists of fields, and so that -o json never leaks a secret: a
// view has no field to put one in.
type projectView struct {
	Name        string `json:"name" yaml:"name"`
	Active      bool   `json:"active" yaml:"active"`
	BaseURL     string `json:"baseUrl" yaml:"baseUrl"`
	Email       string `json:"email" yaml:"email"`
	Credential  string `json:"credential" yaml:"credential"`
	Username    string `json:"username,omitempty" yaml:"username,omitempty"`
	Tenant      string `json:"tenant,omitempty" yaml:"tenant,omitempty"`
	ProjectID   string `json:"projectId,omitempty" yaml:"projectId,omitempty"`
	Environment string `json:"environment,omitempty" yaml:"environment,omitempty"`
	Ephemeral   bool   `json:"ephemeral,omitempty" yaml:"ephemeral,omitempty"`
}

func newProjectView(p config.Project, active bool, store secrets.Store) projectView {
	return projectView{
		Name:        p.Name,
		Active:      active,
		BaseURL:     p.BaseURL,
		Email:       p.Email,
		Credential:  credentialStatus(store, p),
		Username:    p.Username,
		Tenant:      p.Tenant,
		ProjectID:   p.ProjectID,
		Environment: p.Environment,
		Ephemeral:   p.Ephemeral,
	}
}

var projectHeaders = []string{"NAME", "BASE URL", "EMAIL", "CREDENTIAL"}

func (v projectView) row() []string {
	// The active project is marked in the NAME column rather than given a column
	// of its own: one glance, one asterisk, and the table stays four columns wide.
	name := "  " + v.Name
	if v.Active {
		name = "* " + v.Name
	}
	return []string{name, v.BaseURL, v.Email, v.Credential}
}

func projectRows(views []projectView) output.Rows {
	rows := make([][]string, 0, len(views))
	for _, v := range views {
		rows = append(rows, v.row())
	}
	return output.Rows{Headers: projectHeaders, Rows: rows}
}
