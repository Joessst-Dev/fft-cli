package main

import (
	"errors"
	"fmt"

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
pasted log from then on. Piping it, as above, needs no flag.`

func newAuthTokenCmd(deps *Deps) *cobra.Command {
	var raw bool

	cmd := &cobra.Command{
		Use:   "token",
		Short: "Print the current id token",
		Long:  authTokenLong,
		Args:  usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAuthToken(cmd, deps, raw)
		},
	}

	cmd.Flags().BoolVar(&raw, "raw", false, "Print the token even when stdout is a terminal")

	return cmd
}

func runAuthToken(cmd *cobra.Command, deps *Deps, raw bool) error {
	if !raw && prompt.IsTerminal(cmd.OutOrStdout()) {
		return exitcode.UsageError{Err: errors.New(
			"printing your id token to a terminal leaves it in your scrollback: pipe it somewhere, or pass --raw if you meant it")}
	}

	_, src, err := deps.tokenSource()
	if err != nil {
		return err
	}

	ctx, cancel := deps.Context(cmd)
	defer cancel()

	token, err := src.Token(ctx)
	if err != nil {
		return err
	}

	// Printed bare, with a newline and nothing else: the point of this command is
	// $(fft auth token --raw), and a label would end up in the Authorization
	// header.
	fmt.Fprintln(deps.Printer.Out(), token)
	return nil
}
