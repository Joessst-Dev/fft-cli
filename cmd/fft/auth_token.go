package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/prompt"
)

const authTokenLong = `Print the current id token, signing in or refreshing if necessary.

This is for scripts that need to call the API with something other than fft:

  curl -H "Authorization: Bearer $(fft auth token --raw)" \
    https://acme.api.fulfillmenttools.com/api/facilities

The token is a bearer credential: anything holding it can act as you until it
expires. fft therefore refuses to print it to a terminal unless you ask for it
with --raw — a token in your scrollback is a token in every screenshot and every
pasted log from then on. Piping it, as above, needs no flag.

Without --refresh the token is whatever is cached, and fft only replaces it when
under five minutes remain — so it may be close to expiry. That is right for a
command run straight away, as above, but not for something that runs for a
while and cannot renew it: a CI step, a sandbox, a long crawl. For those, pass
--refresh to mint a new token first, with the full hour ahead of it, and print
that one. The remaining lifetime is reported on stderr.

  (umask 077; fft auth token --refresh > token-file)

--refresh fails like 'fft auth refresh' does: exit 4 when neither the refresh
token nor the stored password works, exit 2 for a fixed FFT_ID_TOKEN, which
cannot be renewed.`

func newAuthTokenCmd(deps *Deps) *cobra.Command {
	var raw, refresh bool

	cmd := &cobra.Command{
		Use:   "token",
		Short: "Print the current id token",
		Long:  authTokenLong,
		Args:  usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAuthToken(cmd, deps, raw, refresh)
		},
	}

	cmd.Flags().BoolVar(&raw, "raw", false, "Print the token even when stdout is a terminal")
	cmd.Flags().BoolVar(&refresh, "refresh", false, "Mint a new token first, so it has its full lifetime")

	return cmd
}

func runAuthToken(cmd *cobra.Command, deps *Deps, raw, refresh bool) error {
	// Refused before anything is minted: a run that prints nothing should not
	// also spend a sign-in.
	if !raw && prompt.IsTerminal(cmd.OutOrStdout()) {
		return exitcode.UsageError{Err: errors.New(
			"printing your id token to a terminal leaves it in your scrollback: pipe it somewhere, or pass --raw if you meant it")}
	}

	var token string
	var expiresAt time.Time

	if refresh {
		_, minted, err := renewToken(cmd, deps)
		if err != nil {
			return err
		}
		token, expiresAt = minted.ID, minted.ExpiresAt
	} else {
		_, src, err := deps.tokenSource()
		if err != nil {
			return err
		}

		ctx, cancel := deps.Context(cmd)
		defer cancel()

		token, err = src.Token(ctx)
		if err != nil {
			return err
		}
	}

	// Printed bare, with a newline and nothing else: the point of this command is
	// $(fft auth token --raw), and a label would end up in the Authorization
	// header.
	fmt.Fprintln(deps.Printer.Out(), token)

	// Only a minted token has a known expiry: a TokenSource hands back the bare
	// string, and reading the cached token's lifetime would mean either widening
	// that seam or decoding the JWT here.
	if refresh {
		deps.Printer.Notef("token valid for %s", lifetime(expiresAt.Sub(deps.Clock())))
	}
	return nil
}

// lifetime renders what is left of a token for a person: whole minutes, because
// a consumer deciding whether the token outlasts its job does not care about the
// seconds.
func lifetime(d time.Duration) string {
	if d < time.Minute {
		return "under a minute"
	}
	d = d.Round(time.Minute)
	h, m := int(d/time.Hour), int(d%time.Hour/time.Minute)
	switch {
	case h == 0:
		return fmt.Sprintf("%dm", m)
	case m == 0:
		return fmt.Sprintf("%dh", h)
	default:
		return fmt.Sprintf("%dh%dm", h, m)
	}
}
