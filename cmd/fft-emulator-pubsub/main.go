// Command fft-emulator-pubsub delivers fft emulator events to a local Google Cloud
// Pub/Sub emulator.
//
// It is a transport component: the fft emulator starts it and speaks
// [transportproto] to it over stdin and stdout. It is not run by hand — there is
// nothing useful to type at it — and it makes no request to any tenant.
//
// The host comes from PUBSUB_EMULATOR_HOST, the variable every Pub/Sub client
// already reads, which the emulator sets from its own --pubsub-emulator-host. Every
// connection is pinned to that host with authentication disabled, so this can only
// ever reach a local emulator and never real Google Cloud.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Joessst-Dev/fft-cli/pkg/transportproto"
)

// EnvHost is where this transport reads the emulator it delivers to.
//
// It is the standard name a Pub/Sub client already looks for, and that is the point:
// a component should keep reading the variable its own ecosystem defines rather than
// learn an fft-specific spelling of it.
const EnvHost = "PUBSUB_EMULATOR_HOST"

// targetGoogleCloudPubSub is the subscription target type this transport delivers.
const targetGoogleCloudPubSub = "GOOGLE_CLOUD_PUB_SUB"

// main is a thin wrapper around run so that the single os.Exit does not skip run's
// deferred Close — a broker connection left un-drained on the way out.
func main() { os.Exit(run()) }

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	host := os.Getenv(EnvHost)
	if host == "" {
		// Refused at the handshake rather than at the first event: the emulator's startup
		// notice has to be able to say this target type will not be delivered, and finding
		// out when somebody's subscription silently does not fire is too late.
		fmt.Fprintf(os.Stderr, "no %s is set, so there is no Pub/Sub emulator to publish to\n", EnvHost)
		return 2
	}

	transport := newPubSubTransport(host)
	defer func() { _ = transport.Close() }()

	// stdin is the protocol and stdout is its answers; everything this process has to
	// say to a human goes to stderr, which the emulator prefixes and forwards.
	if err := transportproto.Serve(ctx, os.Stdin, os.Stdout, transport); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
