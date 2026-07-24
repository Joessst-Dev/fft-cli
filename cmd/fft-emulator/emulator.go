package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/component"
	"github.com/Joessst-Dev/fft-cli/internal/config"
	"github.com/Joessst-Dev/fft-cli/internal/emulator"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
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

// newRootCmd builds the emulator's command tree.
//
// It is `fft emulator` when fft dispatches to it, and `fft-emulator` when it is run
// directly. The Use string says the former, because that is what a user types and
// what every piece of documentation shows.
func newRootCmd() *cobra.Command {
	var (
		host        string
		port        int
		seed        string
		verbose     bool
		pubsubHost  string
		sbHost      string
		allowRemote bool
	)

	cmd := &cobra.Command{
		Use:   "emulator",
		Short: "Run a local offline fulfillmenttools API emulator",
		Long:  emulatorLong,
		Args:  usageArgs(cobra.NoArgs),

		SilenceUsage:  true,
		SilenceErrors: true,

		RunE: func(cmd *cobra.Command, _ []string) error {
			srv, err := emulator.New(emulator.Config{
				Host:    host,
				Port:    port,
				Seed:    seed,
				Verbose: verbose,
				Log:     cmd.ErrOrStderr(),

				// The transports are components of this component. They are found in the same
				// root fft installs everything else into, which fft passes down in the
				// environment — so a transport installed with `fft component install` is one
				// the emulator finds without being told where to look.
				Components: openComponents(),

				// Each transport reads the variable its own ecosystem defines, and gets it
				// only if its manifest declared it. So --pubsub-emulator-host still means
				// exactly what it always did, and reaches only the transport that asked for it.
				TransportEnv: map[string]string{
					"PUBSUB_EMULATOR_HOST":     pubsubHost,
					"SERVICEBUS_EMULATOR_HOST": sbHost,
				},

				WebhookAllowRemote: allowRemote,
			})
			if err != nil {
				return err
			}

			// The recipe is a notice, so it goes to stderr: stdout stays the data
			// contract even for a command that never prints data. It prints only once
			// the port is actually bound, so a taken-port failure doesn't follow a
			// recipe that looks like it worked.
			ready := func() { printEmulatorRecipe(cmd.ErrOrStderr(), port, srv.Eventing()) }
			return srv.Listen(cmd.Context(), ready)
		},
	}

	cmd.Flags().StringVar(&host, "host", "127.0.0.1",
		"Interface to bind; the emulator has no auth, so it stays on loopback unless you widen it (0.0.0.0 for a container)")
	cmd.Flags().IntVar(&port, "port", 8080, "Port to listen on")
	cmd.Flags().StringVar(&seed, "seed", "",
		"Directory of JSON fixtures to preload, one <collection>.json per collection")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "Log every request to stderr")
	cmd.Flags().StringVar(&pubsubHost, "pubsub-emulator-host", os.Getenv("PUBSUB_EMULATOR_HOST"),
		"Local Pub/Sub emulator (host:port) to publish GOOGLE_CLOUD_PUB_SUB targets to; defaults to $PUBSUB_EMULATOR_HOST, empty skips them")
	cmd.Flags().StringVar(&sbHost, "servicebus-emulator-host", "",
		"Local Azure Service Bus emulator (host:port) to send MICROSOFT_AZURE_SERVICE_BUS targets to; empty skips them")
	cmd.Flags().BoolVar(&allowRemote, "webhook-allow-remote", false,
		"Call WEBHOOK callbackUrls outside the local network; off by default, so a fixture naming a real endpoint is skipped rather than called")

	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return exitcode.UsageError{Err: err}
	})

	cmd.AddCommand(newEmulatorEmitCmd())
	return cmd
}

// openComponents finds the transport components. A root that cannot be resolved is an
// emulator with no broker transports, which is a perfectly ordinary way to run it —
// not a reason to refuse to start.
func openComponents() *component.Registry {
	root, enabled, err := component.Root(os.LookupEnv)
	if err != nil || !enabled {
		return component.Open("")
	}
	return component.Open(root)
}

// printEmulatorRecipe prints the environment that points fft at the emulator. It uses
// the headless FFT_ID_TOKEN path (config.FromEnv), which hands fft a static token and
// never contacts Firebase — the only way in, since the emulator cannot stand in for
// Google's sign-in.
func printEmulatorRecipe(w io.Writer, port int, eventing []emulator.TargetStatus) {
	base := fmt.Sprintf("http://localhost:%d", port)

	fmt.Fprintf(w, "fft emulator listening on %s\n\n", base)
	fmt.Fprintln(w, "Point fft at it from another shell:")
	fmt.Fprintf(w, "  export %s=%s\n", config.EnvBaseURL, base)
	fmt.Fprintf(w, "  export %s=emulator\n", config.EnvFirebaseAPIKey)
	fmt.Fprintf(w, "  export %s=dev@localhost\n", config.EnvEmail)
	fmt.Fprintf(w, "  export %s=emulator-token\n", config.EnvIDToken)
	fmt.Fprintln(w, "\nThen: fft facility create --file facility.json && fft facility list")

	printEmulatorEventing(w, eventing)
	fmt.Fprintln(w, "\nPress Ctrl-C to stop.")
}

// printEmulatorEventing reports each subscription target type separately, because they
// are enabled separately — and now, for the broker targets, installed separately too.
//
// The line for a live target is the transport's own words, from the handshake. That is
// what lets this notice say where a broker actually is without the emulator knowing
// anything about brokers, and what will keep it honest about a transport nobody here
// has heard of.
func printEmulatorEventing(w io.Writer, eventing []emulator.TargetStatus) {
	fmt.Fprintln(w, "\nEventing, by subscription target:")

	for _, t := range eventing {
		fmt.Fprintf(w, "  %-28s %s\n", t.Target, t.Status)
	}

	fmt.Fprintln(w, "\nRegister a subscription, then mutations deliver to its target:")
	fmt.Fprintln(w, `  fft api addSubscription --data '{"name":"orders","event":"ORDER_CREATED",`+
		`"target":{"type":"WEBHOOK","callbackUrl":"http://localhost:3000/hook"}}'`)
	fmt.Fprintln(w, "Emit an event that no CRUD triggers:")
	fmt.Fprintln(w, "  fft emulator emit PICK_JOB_PICKING_COMMENCED --payload-file pickjob.json")
}

const emulatorEmitLong = `Publish a fulfillmenttools event to a running emulator.

The emulator publishes lifecycle events on its own when you create, update or delete
an entity (POST /api/orders emits ORDER_CREATED, and so on). This reaches the events
that no such mutation does — a picking or routing state change — by asking the
emulator to publish one, with a payload you supply, to every subscription that
matches its name and contexts.

It talks to a running emulator over HTTP; it makes no request to any tenant. Point it
with --url, or let it read $FFT_BASE_URL — the same value the emulator's startup
recipe exports.`

// newEmulatorEmitCmd asks a running emulator to publish one event. It is a thin HTTP
// client for POST /_emulator/emit, so the emulator does the subscription matching and
// the publish; nothing here reaches a tenant.
func newEmulatorEmitCmd() *cobra.Command {
	var (
		file string
		url  string
	)

	cmd := &cobra.Command{
		Use:   "emit <EVENT>",
		Short: "Publish an event to a running emulator's subscriptions",
		Long:  emulatorEmitLong,
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := json.RawMessage("{}")
			if file != "" {
				raw, err := readBody(cmd.InOrStdin(), file)
				if err != nil {
					return err
				}
				payload = raw
			}

			body, err := json.Marshal(map[string]any{"event": args[0], "payload": payload})
			if err != nil {
				return err
			}
			return emitEvent(cmd.Context(), cmd.ErrOrStderr(), url, body)
		},
	}

	cmd.Flags().StringVar(&file, "payload-file", "",
		"File (or - for stdin) with the event payload JSON; defaults to an empty object")
	cmd.Flags().StringVar(&url, "url", emulatorURL(),
		"Base URL of the running emulator; defaults to $FFT_BASE_URL or http://localhost:8080")
	return cmd
}

// emitEvent POSTs the event to the emulator and reports the outcome on stderr — the
// command produces no stdout data, so the summary is a notice like every other. A zero
// count has two causes with different fixes: eventing off entirely, or on but nothing
// matched, so it names the one that applies rather than always pointing at a
// subscription.
func emitEvent(ctx context.Context, w io.Writer, base string, body []byte) error {
	endpoint := strings.TrimRight(base, "/") + "/_emulator/emit"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("reach the emulator at %s: %w", base, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("emulator returned %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}

	var result struct {
		Enabled   bool     `json:"enabled"`
		Published int      `json:"published"`
		Targets   []string `json:"targets"`
		Topics    []string `json:"topics"` // the name targets went by before the other two targets existed
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("decode emulator response: %w", err)
	}
	// An emulator old enough to predate the other two targets answers with topics only,
	// which is the same list under its old name.
	targets := result.Targets
	if len(targets) == 0 {
		targets = result.Topics
	}

	switch {
	case !result.Enabled:
		fmt.Fprintln(w, "published 0 — the emulator has no delivery transport configured")
	case result.Published == 0:
		fmt.Fprintln(w, "published 0 — nothing matched and could be delivered "+
			"(register a subscription with 'fft api addSubscription'; the emulator's log says why one was skipped)")
	default:
		fmt.Fprintf(w, "published %d to %s\n", result.Published, strings.Join(targets, ", "))
	}
	return nil
}

// emulatorURL is where the emit command looks for a running emulator: the base URL
// the startup recipe exports, falling back to the default listen address.
func emulatorURL() string {
	if v := os.Getenv(config.EnvBaseURL); v != "" {
		return v
	}
	return "http://localhost:8080"
}

// readBody reads a JSON body from a file, or from stdin for "-".
func readBody(stdin io.Reader, path string) ([]byte, error) {
	var (
		raw []byte
		err error
	)

	if path == "-" {
		raw, err = io.ReadAll(stdin)
	} else {
		raw, err = os.ReadFile(path) //nolint:gosec // a payload file the user named
	}
	if err != nil {
		return nil, exitcode.UsageError{Err: fmt.Errorf("read %s: %w", path, err)}
	}

	if !json.Valid(raw) {
		return nil, exitcode.UsageError{Err: fmt.Errorf("%s does not contain valid JSON", path)}
	}
	return raw, nil
}

// usageArgs tags a positional-argument validator's failures as usage errors, so that
// "wrong number of arguments" exits 2 rather than 1 — the same contract fft has.
func usageArgs(validator cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := validator(cmd, args); err != nil {
			return exitcode.UsageError{Err: err}
		}
		return nil
	}
}
