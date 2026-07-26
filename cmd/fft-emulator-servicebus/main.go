// Command fft-emulator-servicebus delivers fft emulator events to a local Azure
// Service Bus emulator.
//
// It is a transport component: the fft emulator starts it and speaks
// [transportproto] to it over stdin and stdout. It is not run by hand — there is
// nothing useful to type at it — and it makes no request to any tenant.
//
// The host comes from SERVICEBUS_EMULATOR_HOST, which the emulator sets from its own
// --servicebus-emulator-host. It is a host and never a connection string, which is
// what pins the credentials to the emulator's published development key: no real
// Azure namespace will accept them.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Joessst-Dev/fft-cli/pkg/transportproto"
)

// EnvHost is where this transport reads the emulator it delivers to. It is the name
// the component's manifest declares, and the emulator sets it from the flag the user
// typed.
const EnvHost = "SERVICEBUS_EMULATOR_HOST"

// targetAzureServiceBus is the subscription target type this transport delivers.
const targetAzureServiceBus = "MICROSOFT_AZURE_SERVICE_BUS"

// main is a thin wrapper around run so that the single os.Exit does not skip run's
// deferred Close — an AMQP connection left un-drained on the way out.
func main() { os.Exit(run()) }

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	host := os.Getenv(EnvHost)
	if host == "" {
		// Refused at the handshake rather than at the first event: the emulator's startup
		// notice has to be able to say this target type will not be delivered, and finding
		// out when somebody's subscription silently does not fire is too late.
		fmt.Fprintf(os.Stderr, "no %s is set, so there is no Service Bus emulator to deliver to\n", EnvHost)
		return 2
	}

	transport := newServiceBusTransport(host)
	defer func() { _ = transport.Close() }()

	// stdin is the protocol and stdout is its answers; everything this process has to
	// say to a human goes to stderr, which the emulator prefixes and forwards.
	if err := transportproto.Serve(ctx, os.Stdin, os.Stdout, transport); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
