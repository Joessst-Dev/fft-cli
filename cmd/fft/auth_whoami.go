package main

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/api"
	"github.com/Joessst-Dev/fft-cli/internal/client"
	"github.com/Joessst-Dev/fft-cli/internal/output"
)

const authWhoamiLong = `Show who fft is authenticated as, and what that account may do.

This calls GET /api/users/me/effectivepermissions with the current id token, so
it is also the shortest proof that authentication works end to end: it signs in
if it has to, and it fails with exit code 4 if it cannot.

A 403 from another command usually means a role is missing here.`

// roleView is one of the caller's roles. It is the table row and the JSON object
// alike, so the two cannot drift apart.
type roleView struct {
	Name        string   `json:"name" yaml:"name"`
	Permissions []string `json:"permissions" yaml:"permissions"`
}

// whoamiView is what `fft auth whoami` renders.
type whoamiView struct {
	UserID string     `json:"userId" yaml:"userId"`
	Email  string     `json:"email" yaml:"email"`
	Roles  []roleView `json:"roles" yaml:"roles"`
}

func newAuthWhoamiCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:         "whoami",
		Short:       "Show the authenticated user and their permissions",
		Long:        authWhoamiLong,
		Args:        usageArgs(cobra.NoArgs),
		Annotations: map[string]string{annotationOperationID: "getEffectivePermissions"},

		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAuthWhoami(cmd, deps)
		},
	}
}

func runAuthWhoami(cmd *cobra.Command, deps *Deps) error {
	project, src, err := deps.tokenSource()
	if err != nil {
		return err
	}

	c, err := deps.apiClient(project, src)
	if err != nil {
		return err
	}

	ctx, cancel := deps.Context(cmd)
	defer cancel()

	// Through Do: whoami is the command a user runs *because* something is wrong
	// with their credentials, so it is the one that most needs the 401 refresh.
	res, err := client.Fetch[api.EffectivePermissionsResponse](ctx, c,
		"get the effective permissions",
		func(ctx context.Context) (*http.Response, error) {
			return c.API().GetEffectivePermissions(ctx)
		})
	if err != nil {
		return err
	}

	view := newWhoamiView(project.Email, res)

	// The identity goes to stderr, so that `-o json | jq` receives the permissions
	// and nothing else — while a human still sees who they are.
	deps.Printer.Notef("Signed in as %s (user %s) on %s.", view.Email, view.UserID, project.BaseURL)

	return deps.Printer.Render(whoamiRows(view), view)
}

func newWhoamiView(email string, res api.EffectivePermissionsResponse) whoamiView {
	roles := make([]roleView, 0, len(res.Roles))
	for _, role := range res.Roles {
		roles = append(roles, roleView{Name: role.Name, Permissions: permissionNames(role)})
	}

	return whoamiView{UserID: res.UserId, Email: email, Roles: roles}
}

// permissionNames flattens the generated enum slice into plain strings, sorted so
// that two runs of the same command produce the same output — the API does not
// promise an order, and a table that reshuffles itself is one a user cannot diff.
func permissionNames(role api.UserRoleWithPermissions) []string {
	if role.Permissions == nil {
		return nil
	}

	names := make([]string, 0, len(*role.Permissions))
	for _, p := range *role.Permissions {
		names = append(names, string(p))
	}
	slices.Sort(names)
	return names
}

var whoamiHeaders = []string{"ROLE", "PERMISSIONS"}

func whoamiRows(v whoamiView) output.Rows {
	rows := make([][]string, 0, len(v.Roles))
	for _, role := range v.Roles {
		rows = append(rows, []string{role.Name, strings.Join(role.Permissions, ", ")})
	}
	return output.Rows{Headers: whoamiHeaders, Rows: rows}
}
