package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/auth"
	"github.com/Joessst-Dev/fft-cli/internal/client"
	"github.com/Joessst-Dev/fft-cli/internal/config"
	"github.com/Joessst-Dev/fft-cli/internal/secrets"
)

const authLong = `Inspect and renew the credentials fft signs in with.

fulfillmenttools has no login endpoint: authentication happens against Google
Identity Platform (Firebase), and only the resulting id token is ever sent to
your tenant. fft signs in with your stored password, caches the id token, and
refreshes it before it expires — so under normal use you never run these
commands at all.

The Firebase Web API key is kept in the keychain alongside your credentials and
never written to the config file. It grants nothing on its own, and fft will not
send it anywhere but Google.`

func newAuthCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Inspect and renew credentials",
		Long:  authLong,
		Args:  usageArgs(cobra.NoArgs),

		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}

	cmd.AddCommand(
		newAuthWhoamiCmd(deps),
		newAuthTokenCmd(deps),
		newAuthRefreshCmd(deps),
	)

	return cmd
}

// newTokenSource builds the TokenSource for a project.
//
// A project with a password signs in and refreshes for itself. A project with
// only an id token — FFT_ID_TOKEN, the CI job that authenticated elsewhere — uses
// it as it is: there is nothing behind it to renew from, and pretending otherwise
// would fail later and less clearly.
func newTokenSource(p config.Project, store secrets.Store, now func() time.Time, debug io.Writer) (auth.TokenSource, error) {
	password, err := lookupSecret(store, p.Name, secrets.KindPassword)
	if err != nil {
		return nil, err
	}

	if password == "" {
		idToken, err := lookupSecret(store, p.Name, secrets.KindIDToken)
		if err != nil {
			return nil, err
		}
		if idToken != "" {
			return auth.StaticTokenSource(idToken), nil
		}
		return nil, config.NewError(
			fmt.Errorf("no credential is stored for project %q", p.Name),
			fmt.Sprintf("Run 'fft project add %s --force' to store one.", p.Name),
		)
	}

	c, err := auth.NewClient(p.FirebaseAPIKey, debugOption(debug)...)
	if err != nil {
		return nil, config.NewError(
			fmt.Errorf("project %q: %w", p.Name, err),
			fmt.Sprintf("Run 'fft project add %s --force' and give the Firebase Web API key.", p.Name),
		)
	}
	return auth.NewFirebaseTokenSource(c, p, store, now), nil
}

// lookupSecret reads one secret, treating "there is none" as an empty string
// rather than as a failure: the caller is deciding *which* credential exists, and
// a missing one is an answer.
func lookupSecret(store secrets.Store, project, kind string) (string, error) {
	val, err := store.Get(secrets.Key(project, kind))
	switch {
	case errors.Is(err, secrets.ErrNotFound):
		return "", nil
	case err != nil:
		return "", fmt.Errorf("read the %s for project %q: %w", kind, project, err)
	}
	return val, nil
}

// verifyCredentials is the real [VerifyFunc]: it signs in against Google and
// returns the email address that actually worked.
//
// `fft project add` runs this before it writes anything, so a project that cannot
// authenticate is never persisted — and the address fft guessed at from the
// username is replaced by the one Google confirmed.
func verifyCredentials(ctx context.Context, p config.Project, password string, debug io.Writer) (string, error) {
	c, err := auth.NewClient(p.FirebaseAPIKey, debugOption(debug)...)
	if err != nil {
		return "", err
	}

	tok, err := c.SignIn(ctx, p.Email, password)
	if err != nil {
		return "", err
	}
	return tok.Email, nil
}

// apiClient builds the API client for a project. src may be nil, which yields an
// unauthenticated client — what `fft ping` uses.
func (d *Deps) apiClient(p config.Project, src auth.TokenSource) (*client.Client, error) {
	opts := []client.Option{client.WithRetry(d.Retry)}
	if src != nil {
		opts = append(opts, client.WithTokenSource(src))
	}
	if d.Debug != nil {
		opts = append(opts, client.WithDebug(d.Debug))
	}

	c, err := client.New(p.BaseURL, opts...)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// debugOption turns --debug into an auth option. Google's traffic is where the
// password and the API key actually travel, so it is the traffic most worth being
// able to see — and the traffic whose dump most needs redacting.
func debugOption(debug io.Writer) []auth.Option {
	if debug == nil {
		return nil
	}
	return []auth.Option{auth.WithDebug(debug)}
}

// tokenSource resolves the active project and its TokenSource together, which is
// the first thing every authenticated command needs.
func (d *Deps) tokenSource() (config.Project, auth.TokenSource, error) {
	p, err := d.ActiveProject()
	if err != nil {
		return config.Project{}, nil, err
	}

	src, err := d.NewTokenSource(p, d.Secrets, d.Clock, d.Debug)
	if err != nil {
		return config.Project{}, nil, err
	}
	return p, src, nil
}
