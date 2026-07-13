package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/auth"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/output"
)

const authRefreshLong = `Mint a new id token now, without waiting for the current one to expire.

fft refreshes on its own — a token with less than five minutes left is replaced
before it is used — so this command exists for the times you want to see it
happen: after rotating a password, or when diagnosing a 401.

The refresh token is used if it still works, and your stored password if it does
not. If neither does, the command exits 4 and tells you to sign in again.

The token itself is never printed. Use 'fft auth token --raw' for that.`

// tokenView is what `fft auth refresh` renders: everything about the new token
// except the token.
type tokenView struct {
	Project   string    `json:"project" yaml:"project"`
	Email     string    `json:"email" yaml:"email"`
	ExpiresAt time.Time `json:"expiresAt" yaml:"expiresAt"`
	ExpiresIn string    `json:"expiresIn" yaml:"expiresIn"`
}

func newAuthRefreshCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "refresh",
		Short: "Mint a new id token now",
		Long:  authRefreshLong,
		Args:  usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAuthRefresh(cmd, deps)
		},
	}
}

func runAuthRefresh(cmd *cobra.Command, deps *Deps) error {
	project, src, err := deps.tokenSource()
	if err != nil {
		return err
	}

	renewer, ok := src.(auth.Renewer)
	if !ok {
		// The only source that cannot renew is the static one: FFT_ID_TOKEN is a
		// fixed string with no password and no refresh token behind it.
		return exitcode.UsageError{Err: fmt.Errorf(
			"project %q authenticates with a fixed id token (%s), which cannot be refreshed",
			project.Name, "FFT_ID_TOKEN")}
	}

	ctx, cancel := deps.Context(cmd)
	defer cancel()

	token, err := renewer.Renew(ctx)
	if err != nil {
		return err
	}

	view := newTokenView(project.Name, token, deps.Clock())

	return deps.Printer.Render(tokenRows(view), view)
}

func newTokenView(project string, token auth.Token, now time.Time) tokenView {
	return tokenView{
		Project:   project,
		Email:     token.Email,
		ExpiresAt: token.ExpiresAt.UTC(),
		ExpiresIn: token.ExpiresAt.Sub(now).Round(time.Second).String(),
	}
}

var tokenHeaders = []string{"PROJECT", "EMAIL", "EXPIRES AT", "EXPIRES IN"}

func tokenRows(v tokenView) output.Rows {
	return output.Rows{
		Headers: tokenHeaders,
		Rows: [][]string{{
			v.Project,
			v.Email,
			v.ExpiresAt.Format(time.RFC3339),
			v.ExpiresIn,
		}},
	}
}
