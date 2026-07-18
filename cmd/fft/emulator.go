package main

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/config"
	"github.com/Joessst-Dev/fft-cli/internal/emulator"
)

const emulatorLong = `Run a local server that mimics the fulfillmenttools API.

Every operation the API has is addressable on the emulator. The top-level
collections (facilities, listings, stocks, orders, …) are stateful: a POST is
remembered, a GET reflects it, versions and pagination work. Everything else is
answered from a response synthesized from the spec — reachable, but not remembered.

The emulator makes no request to any tenant and holds all state in memory, so it
dies with the process. Point fft at it with the FFT_* recipe it prints on startup;
'fft project add' does not work against it, because signing in reaches Google's
identity service, which a local server cannot stand in for.`

// newEmulatorCmd runs the offline API emulator. It makes no tenant request, so it
// carries no operationId and is excused in readonly_test.go's commandsWithoutOperation.
func newEmulatorCmd(_ *Deps) *cobra.Command {
	var (
		host    string
		port    int
		seed    string
		verbose bool
	)

	cmd := &cobra.Command{
		Use:   "emulator",
		Short: "Run a local offline fulfillmenttools API emulator",
		Long:  emulatorLong,
		Args:  usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			srv, err := emulator.New(emulator.Config{
				Host:    host,
				Port:    port,
				Seed:    seed,
				Verbose: verbose,
				Log:     cmd.ErrOrStderr(),
			})
			if err != nil {
				return err
			}

			// The recipe is a notice, so it goes to stderr: stdout stays the data
			// contract even for a command that never prints data.
			printEmulatorRecipe(cmd.ErrOrStderr(), port)
			return srv.Listen(cmd.Context())
		},
	}

	cmd.Flags().StringVar(&host, "host", "127.0.0.1",
		"Interface to bind; the emulator has no auth, so it stays on loopback unless you widen it (0.0.0.0 for a container)")
	cmd.Flags().IntVar(&port, "port", 8080, "Port to listen on")
	cmd.Flags().StringVar(&seed, "seed", "",
		"Directory of JSON fixtures to preload, one <collection>.json per collection")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "Log every request to stderr")
	return cmd
}

// printEmulatorRecipe prints the environment that points fft at the emulator. It uses
// the headless FFT_ID_TOKEN path (config.FromEnv), which hands fft a static token and
// never contacts Firebase — the only way in, since the emulator cannot stand in for
// Google's sign-in.
func printEmulatorRecipe(w io.Writer, port int) {
	base := fmt.Sprintf("http://localhost:%d", port)

	fmt.Fprintf(w, "fft emulator listening on %s\n\n", base)
	fmt.Fprintln(w, "Point fft at it from another shell:")
	fmt.Fprintf(w, "  export %s=%s\n", config.EnvBaseURL, base)
	fmt.Fprintf(w, "  export %s=emulator\n", config.EnvFirebaseAPIKey)
	fmt.Fprintf(w, "  export %s=dev@localhost\n", config.EnvEmail)
	fmt.Fprintf(w, "  export %s=emulator-token\n", config.EnvIDToken)
	fmt.Fprintln(w, "\nThen: fft facility create --file facility.json && fft facility list")
	fmt.Fprintln(w, "\nPress Ctrl-C to stop.")
}
