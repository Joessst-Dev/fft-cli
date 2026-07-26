# Components

A component is a building block somebody else wrote, installed alongside `fft`: a binary
plus a manifest that adds commands to the CLI, or teaches the emulator to deliver events
to a broker `fft` has never heard of. Once installed, its commands appear in `fft --help`
like any other.

## What is installed here

```sh
fft component list
fft component info weather
```

`STATUS` is `installed` when the binary is present. It is `available` when `fft` knows
about the component but it has not been installed — which only ever applies to a component
`fft` ships itself, because those are registered whether or not they are present. So a
command can exist in `--help`, and in this document, and still need one install before it
runs.

`ORIGIN` says who wrote it: `first-party` for one `fft` ships, `community` for anything
else. It is the column that decides how much the others are worth trusting.

The **emulator** is the reference first-party component: it used to be built into `fft`
and is now installed separately, which is what took a third off the CLI's download. Its
two broker transports — `emulator-pubsub` and `emulator-servicebus` — are components too.
See [the emulator](emulator.md).

## Installing

```sh
fft component install emulator
fft component install acme/fft-weather
fft component install acme/fft-weather@v1.2.0
fft component install --path ./my-component
```

`fft` fetches the release, checks the archive against that release's `checksums.txt`, and
unpacks it into a staging directory. **Nothing is installed until the user says yes**, and
the question shows where it came from, whether the download matched, and what the component
will be allowed to do.

Do not answer that question for the user. `-y` exists, and it is theirs to pass: a
component runs as whoever ran `fft`, with whatever access they have. Installing one is a
decision about trust, and it is not yours to make on their behalf.

```sh
fft component upgrade weather
fft component remove weather
```

## What a component is given

`fft` builds the environment its components run with. What lands in it is decided by the
`session` each command declares, and nothing else:

| session | what the component gets |
| --- | --- |
| `none` | no tenant credential at all — not even the base URL |
| `read` | the base URL and a short-lived id token, with `FFT_READ_ONLY` forced on |
| `write` | the same, without the forced read-only |

Every `FFT_*` variable in the caller's environment is stripped before this is built, so a
component never inherits a password or an API key that happened to be exported. The
Firebase API key is never handed over at any level: a component gets a credential that
expires, never the one that mints more.

`fft component info <name>` prints all of it — which commands, which session each one asks
for, whether it declares that it changes data. That is the whole of what `fft` knows about
a component before running it, so it is the whole of what there is to review.

## The read-only gate still applies

A component command that declares `mutates` is refused against a read-only project, with
exit code 10, **before the component is started**. So is one that claims an operation the
API spec says is a write, whatever its manifest declares about itself.

What the gate cannot catch is a component that writes while declaring nothing and claiming
nothing. Say so plainly if the user asks: the manifest is a declaration `fft` enforces, not
a sandbox it imposes.

## Reporting on one

Two things are worth saying when a user asks about a component:

```sh
fft component info weather -o json
```

- Whether it is first-party. A community component is somebody else's code running with
  the user's credentials.
- Which session its commands ask for. A `write` session on a component nobody recognises is
  worth raising before it is installed, not after.

## When a command is missing

If a component's command reports that it is not installed, the fix is the command in the
error itself — install the component it names:

```sh
fft component install emulator
```

If `fft --help` warns that a component declared a command `fft` already has, the built-in
one wins and the component's is dropped. That is deliberate — a component cannot silently
change what an existing command does — and the component's author is the one who has to
rename it.

## Writing a transport component in Go

A transport component is a separate process the emulator talks to over stdin and stdout:
newline-delimited JSON, one `hello`/`plan`/`send` request per line, stderr for logs. Any
language can implement that wire format directly. Go authors do not have to — the protocol
types and the loop that drives them are a public package,
`github.com/Joessst-Dev/fft-cli/pkg/transportproto`.

Implement `transportproto.Handler` and hand it to `transportproto.Serve`, which owns the
read/answer/write loop and returns nil at EOF (the emulator closing stdin is how it says
it is done):

```go
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Joessst-Dev/fft-cli/pkg/transportproto"
)

type myTransport struct{ /* a broker client */ }

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

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := transportproto.Serve(ctx, os.Stdin, os.Stdout, &myTransport{}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```

The first-party `emulator-pubsub` and `emulator-servicebus` transports are built on this
same package, so they are the working reference for a real broker client, target
validation, and graceful shutdown. `transportproto.Version` (handed to the child in the
`FFT_TRANSPORT_API` environment variable) gates wire compatibility, so a component can
refuse a host it does not understand rather than fail further in.
