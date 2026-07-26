package transportproto_test

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/Joessst-Dev/fft-cli/pkg/transportproto"
)

// myTransport is a minimal [transportproto.Handler]. A real one would hold a broker
// client and resolve targets against it; this one answers with constants so the example
// stays about the wiring, not the broker. It is named and shaped to match the copy in
// the emulator component guide, so the two read as one example.
type myTransport struct{}

// Hello reports the target types this transport delivers, and one line for the
// emulator's startup notice saying where.
func (t *myTransport) Hello() (targets []string, status string, err error) {
	return []string{"MY_BROKER"}, "publishing to my-broker at localhost:9000", nil
}

// Plan resolves one subscription target into the label its deliveries are reported
// under, or returns an error the emulator logs as the reason it skips that subscription.
func (t *myTransport) Plan(target map[string]any) (label string, err error) {
	return "my-broker/orders", nil
}

// Send delivers one event to a target Plan accepted.
func (t *myTransport) Send(ctx context.Context, target map[string]any, event string, data []byte) error {
	return nil
}

// This is the whole of a transport component: refuse an emulator speaking a protocol
// version this build does not, then hand a [transportproto.Handler] to
// [transportproto.Serve], which owns the read/answer/write loop until stdin closes.
func Example() {
	if got := os.Getenv(transportproto.EnvVersion); got != "" && got != strconv.Itoa(transportproto.Version) {
		fmt.Fprintf(os.Stderr, "the emulator speaks transport protocol %s, this component speaks %d\n", got, transportproto.Version)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := transportproto.Serve(ctx, os.Stdin, os.Stdout, &myTransport{}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
