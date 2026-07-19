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
func newEmulatorCmd(deps *Deps) *cobra.Command {
	var (
		host       string
		port       int
		seed       string
		verbose    bool
		pubsubHost string
	)

	cmd := &cobra.Command{
		Use:   "emulator",
		Short: "Run a local offline fulfillmenttools API emulator",
		Long:  emulatorLong,
		Args:  usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			srv, err := emulator.New(emulator.Config{
				Host:       host,
				Port:       port,
				Seed:       seed,
				Verbose:    verbose,
				Log:        cmd.ErrOrStderr(),
				PubSubHost: pubsubHost,
			})
			if err != nil {
				return err
			}

			// The recipe is a notice, so it goes to stderr: stdout stays the data
			// contract even for a command that never prints data. It prints only once
			// the port is actually bound, so a taken-port failure doesn't follow a
			// recipe that looks like it worked.
			ready := func() { printEmulatorRecipe(cmd.ErrOrStderr(), port, pubsubHost) }
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
		"Local Pub/Sub emulator (host:port) to publish events to; defaults to $PUBSUB_EMULATOR_HOST, empty disables eventing")

	cmd.AddCommand(newEmulatorEmitCmd(deps))
	return cmd
}

// printEmulatorRecipe prints the environment that points fft at the emulator. It uses
// the headless FFT_ID_TOKEN path (config.FromEnv), which hands fft a static token and
// never contacts Firebase — the only way in, since the emulator cannot stand in for
// Google's sign-in.
func printEmulatorRecipe(w io.Writer, port int, pubsubHost string) {
	base := fmt.Sprintf("http://localhost:%d", port)

	fmt.Fprintf(w, "fft emulator listening on %s\n\n", base)
	fmt.Fprintln(w, "Point fft at it from another shell:")
	fmt.Fprintf(w, "  export %s=%s\n", config.EnvBaseURL, base)
	fmt.Fprintf(w, "  export %s=emulator\n", config.EnvFirebaseAPIKey)
	fmt.Fprintf(w, "  export %s=dev@localhost\n", config.EnvEmail)
	fmt.Fprintf(w, "  export %s=emulator-token\n", config.EnvIDToken)
	fmt.Fprintln(w, "\nThen: fft facility create --file facility.json && fft facility list")

	printEmulatorEventing(w, pubsubHost)
	fmt.Fprintln(w, "\nPress Ctrl-C to stop.")
}

// printEmulatorEventing tells the user whether events will be published and, when
// they will, how to point a subscription at a topic. Eventing is off unless a
// Pub/Sub emulator host is configured — the emulator never publishes to real Google
// Cloud.
func printEmulatorEventing(w io.Writer, pubsubHost string) {
	if pubsubHost == "" {
		fmt.Fprintf(w, "\nEventing: off (set --pubsub-emulator-host or %s to publish events).\n",
			"PUBSUB_EMULATOR_HOST")
		return
	}

	fmt.Fprintf(w, "\nEventing: publishing to the Pub/Sub emulator at %s.\n", pubsubHost)
	fmt.Fprintln(w, "Register a subscription, then mutations publish to its topic:")
	fmt.Fprintln(w, `  fft api addSubscription --data '{"name":"orders","event":"ORDER_CREATED",`+
		`"target":{"type":"GOOGLE_CLOUD_PUB_SUB","projectId":"local","topicId":"orders"}}'`)
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
// the publish; nothing here reaches a tenant, which is why it is excused in
// readonly_test.go's commandsWithoutOperation.
func newEmulatorEmitCmd(deps *Deps) *cobra.Command {
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
			ctx, cancel := deps.Context(cmd)
			defer cancel()

			payload := json.RawMessage("{}")
			if file != "" {
				raw, err := readBody(deps, file)
				if err != nil {
					return err
				}
				payload = raw
			}

			body, err := json.Marshal(map[string]any{"event": args[0], "payload": payload})
			if err != nil {
				return err
			}
			return emitEvent(ctx, cmd.ErrOrStderr(), url, body)
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
// count has two causes with different fixes: eventing off entirely, or on but no
// subscription matched, so it names the one that applies rather than always pointing at
// a subscription.
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
		Topics    []string `json:"topics"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("decode emulator response: %w", err)
	}

	switch {
	case !result.Enabled:
		fmt.Fprintf(w, "published 0 — eventing is off (set --pubsub-emulator-host or %s to publish)\n",
			"PUBSUB_EMULATOR_HOST")
	case result.Published == 0:
		fmt.Fprintln(w, "published 0 — no subscription matched (register one with 'fft api addSubscription')")
	default:
		fmt.Fprintf(w, "published %d to %s\n", result.Published, strings.Join(result.Topics, ", "))
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
