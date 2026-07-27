package component

import (
	"fmt"
	"strings"
)

// The skeleton bodies. Each language has a command variant that prints its arguments
// and the FFT_ environment fft handed it — so the author sees the process contract
// working before writing any logic — and a transport variant that answers hello,
// refuses plan and acks send, the smallest thing that speaks the protocol.
//
// The `{{name}}`, `{{exec}}` and `{{target}}` tokens are substituted by [Scaffold.render].

// scaffoldSecretEnv are the FFT_ environment variables a scaffold masks before it
// echoes the process environment. It is the set that carries a credential: a live
// token (id or refresh), the sign-in password, or the Firebase key. Everything
// else fft exports — FFT_ID_TOKEN_EXPIRES_AT, the descriptive FFT_PROJECT_ID and
// so on — is not a secret and is printed as-is.
//
// The four templates hardcode these names because each language spells the mask
// differently (a sed script, a Python set, a JS Set, a Go map). scaffold_test.go
// asserts every template masks exactly this list, so the two cannot drift — the
// same failure class as the FFT_ID_TOKEN-to-stdout leak this scaffold once had.
var scaffoldSecretEnv = []string{
	"FFT_ID_TOKEN",
	"FFT_REFRESH_TOKEN",
	"FFT_PASSWORD",
	"FFT_FIREBASE_API_KEY",
}

func (s Scaffold) shellTemplate() string {
	if s.Kind == KindTransport {
		return shellTransport
	}
	return shellCommand
}

func (s Scaffold) pythonTemplate() string {
	if s.Kind == KindTransport {
		return pythonTransport
	}
	return pythonCommand
}

func (s Scaffold) nodeTemplate() string {
	if s.Kind == KindTransport {
		return nodeTransport
	}
	return nodeCommand
}

func (s Scaffold) goTemplate() string {
	if s.Kind == KindTransport {
		return goTransport
	}
	return goCommand
}

const shellCommand = `#!/bin/sh
# fft command component: {{name}}
#
# fft runs this with the arguments after ` + "`fft {{name}}`" + ` and an environment it
# builds from the session this component's manifest declares. Replace the body with
# your own logic; what is here proves the contract by echoing both.
#
# The credential-bearing FFT_ variables are masked: a read or write session hands over
# a live tenant token, and stdout is the stream fft's output contract promises is safe to pipe.
set -eu

printf 'args:'
for arg in "$@"; do printf ' %s' "$arg"; done
printf '\n'

printf 'fft environment:\n'
# One -e per name, not one with \| alternation: BSD sed (macOS) has no alternation in a
# basic regex, and a scaffold has to redact on the author's machine as well as on Linux.
env | grep '^FFT_' | sort \
	| sed -e 's/^FFT_ID_TOKEN=.*/FFT_ID_TOKEN=<redacted>/' \
	      -e 's/^FFT_REFRESH_TOKEN=.*/FFT_REFRESH_TOKEN=<redacted>/' \
	      -e 's/^FFT_PASSWORD=.*/FFT_PASSWORD=<redacted>/' \
	      -e 's/^FFT_FIREBASE_API_KEY=.*/FFT_FIREBASE_API_KEY=<redacted>/' || true
`

const pythonCommand = `#!/usr/bin/env python3
"""fft command component: {{name}}.

fft runs this with the arguments after ` + "`fft {{name}}`" + ` and an environment it builds
from the session this component's manifest declares. Replace the body with your own
logic; what is here proves the contract by echoing both.

The credential-bearing FFT_ variables are masked: a read or write session hands over a
live tenant token, and stdout is the stream fft's output contract promises is safe to pipe.
"""
import os
import sys

SECRET = {"FFT_ID_TOKEN", "FFT_REFRESH_TOKEN", "FFT_PASSWORD", "FFT_FIREBASE_API_KEY"}

print("args:", " ".join(sys.argv[1:]))
print("fft environment:")
for key in sorted(k for k in os.environ if k.startswith("FFT_")):
    value = "<redacted>" if key in SECRET else os.environ[key]
    print(f"  {key}={value}")
`

const nodeCommand = `#!/usr/bin/env node
// fft command component: {{name}}
//
// fft runs this with the arguments after ` + "`fft {{name}}`" + ` and an environment it builds
// from the session this component's manifest declares. Replace the body with your own
// logic; what is here proves the contract by echoing both.
//
// The credential-bearing FFT_ variables are masked: a read or write session hands over a
// live tenant token, and stdout is the stream fft's output contract promises is safe to pipe.
"use strict";

const SECRET = new Set(["FFT_ID_TOKEN", "FFT_REFRESH_TOKEN", "FFT_PASSWORD", "FFT_FIREBASE_API_KEY"]);

console.log("args:", process.argv.slice(2).join(" "));
console.log("fft environment:");
Object.keys(process.env)
  .filter((k) => k.startsWith("FFT_"))
  .sort()
  .forEach((k) => console.log("  " + k + "=" + (SECRET.has(k) ? "<redacted>" : process.env[k])));
`

const goCommand = `// Command {{exec}} is a fft command component.
//
// fft runs it with the arguments after ` + "`fft {{name}}`" + ` and an environment it builds
// from the session this component's manifest declares. Replace the body with your own
// logic; what is here proves the contract by echoing both.
//
// The credential-bearing FFT_ variables are masked: a read or write session hands over a
// live tenant token, and stdout is the stream fft's output contract promises is safe to pipe.
package main

import (
	"fmt"
	"os"
	"slices"
	"strings"
)

func main() {
	fmt.Println("args:", strings.Join(os.Args[1:], " "))

	secret := map[string]bool{
		"FFT_ID_TOKEN":         true,
		"FFT_REFRESH_TOKEN":    true,
		"FFT_PASSWORD":         true,
		"FFT_FIREBASE_API_KEY": true,
	}

	var fft []string
	for _, e := range os.Environ() {
		name, _, _ := strings.Cut(e, "=")
		if !strings.HasPrefix(name, "FFT_") {
			continue
		}
		if secret[name] {
			e = name + "=<redacted>"
		}
		fft = append(fft, e)
	}
	slices.Sort(fft)

	fmt.Println("fft environment:")
	for _, e := range fft {
		fmt.Println("  " + e)
	}
}
`

// The transport skeletons speak the wire protocol described in
// internal/skill/assets/references/components.md: newline-delimited JSON on stdin and
// stdout, one hello/plan/send request per line. The Go one uses transportproto, which
// owns the loop; the others reimplement it, which any language can.

const shellTransport = `#!/bin/sh
# fft transport component: {{name}}
#
# The emulator starts this and speaks the transport wire protocol to it: one JSON
# request per line on stdin, one JSON response per line on stdout, stderr for logs.
# This scaffold answers hello, refuses plan and acks send. It reimplements the protocol
# by hand — Go authors can use github.com/Joessst-Dev/fft-cli/pkg/transportproto instead.
#
# It reads the "op" and "id" out of each frame with sed rather than a JSON parser, which
# is enough for a scaffold; a real transport in shell would want jq. The matches are
# anchored to the start of the frame — the encoder always emits "id" then "op" first, so
# a nested "id" or "op" inside a send frame's target or data is not what they pick up.
set -eu

while IFS= read -r line; do
	[ -n "$line" ] || continue
	op=$(printf '%s' "$line" | sed -n 's/^{"id":[0-9]*,"op":"\([a-z]*\)".*/\1/p')
	id=$(printf '%s' "$line" | sed -n 's/^{"id":\([0-9]*\).*/\1/p')
	id=${id:-0}
	case "$op" in
	hello)
		printf '{"id":%s,"ok":true,"targets":["{{target}}"],"status":"describe where this transport delivers"}\n' "$id"
		;;
	plan)
		printf '{"id":%s,"ok":false,"reason":"this transport is a scaffold and resolves no targets yet"}\n' "$id"
		;;
	send)
		printf '{"id":%s,"ok":true}\n' "$id"
		;;
	*)
		printf '{"id":%s,"ok":false,"reason":"unknown operation"}\n' "$id"
		;;
	esac
done
`

const pythonTransport = `#!/usr/bin/env python3
"""fft transport component: {{name}}.

The emulator starts this and speaks the transport wire protocol to it: one JSON
request per line on stdin, one JSON response per line on stdout, stderr for logs. This
scaffold answers hello, refuses plan and acks send. It reimplements the protocol by
hand — Go authors can use github.com/Joessst-Dev/fft-cli/pkg/transportproto instead.
"""
import json
import sys


def answer(req):
    res = {"id": req.get("id", 0), "ok": True}
    op = req.get("op")
    if op == "hello":
        res["targets"] = ["{{target}}"]
        res["status"] = "describe where this transport delivers"
    elif op == "plan":
        res["ok"] = False
        res["reason"] = "this transport is a scaffold and resolves no targets yet"
    elif op == "send":
        pass  # deliver req["data"] to req["target"] here
    else:
        res["ok"] = False
        res["reason"] = f"unknown operation {op!r}"
    return res


def main():
    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        print(json.dumps(answer(json.loads(line))), flush=True)


if __name__ == "__main__":
    main()
`

const nodeTransport = `#!/usr/bin/env node
// fft transport component: {{name}}
//
// The emulator starts this and speaks the transport wire protocol to it: one JSON
// request per line on stdin, one JSON response per line on stdout, stderr for logs.
// This scaffold answers hello, refuses plan and acks send. It reimplements the protocol
// by hand — Go authors can use github.com/Joessst-Dev/fft-cli/pkg/transportproto instead.
"use strict";

const readline = require("readline");
const rl = readline.createInterface({ input: process.stdin });

rl.on("line", (line) => {
  if (!line.trim()) return;
  const req = JSON.parse(line);
  const res = { id: req.id || 0, ok: true };
  switch (req.op) {
    case "hello":
      res.targets = ["{{target}}"];
      res.status = "describe where this transport delivers";
      break;
    case "plan":
      res.ok = false;
      res.reason = "this transport is a scaffold and resolves no targets yet";
      break;
    case "send":
      break; // deliver req.data to req.target here
    default:
      res.ok = false;
      res.reason = "unknown operation " + JSON.stringify(req.op);
  }
  process.stdout.write(JSON.stringify(res) + "\n");
});
`

const goTransport = `// Command {{exec}} is a fft transport component.
//
// The emulator starts it and speaks the transport protocol to it over stdin and
// stdout. transportproto.Serve owns the read/answer/write loop and returns nil at EOF,
// which is how the emulator says it is done. This scaffold answers hello, refuses plan
// and acks send; replace the body of each with a real broker client.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/Joessst-Dev/fft-cli/pkg/transportproto"
)

// transport is the handler transportproto.Serve drives.
type transport struct{}

// Hello reports the target types this transport delivers, and one line for the
// emulator's startup notice saying where.
func (transport) Hello() (targets []string, status string, err error) {
	return []string{"{{target}}"}, "describe where this transport delivers", nil
}

// Plan resolves one subscription target into the label its deliveries are reported
// under, or returns the reason the emulator should skip it. This scaffold refuses
// every target until you teach it one.
func (transport) Plan(target map[string]any) (label string, err error) {
	return "", errors.New("this transport is a scaffold and resolves no targets yet")
}

// Send delivers one event to a target Plan accepted.
func (transport) Send(ctx context.Context, target map[string]any, event string, data []byte) error {
	return nil
}

func main() {
	// fft does not enforce the protocol version — it only reports the one it speaks in
	// FFT_TRANSPORT_API — so refusing a host this build does not understand is our job.
	if got := os.Getenv(transportproto.EnvVersion); got != "" && got != strconv.Itoa(transportproto.Version) {
		fmt.Fprintf(os.Stderr, "the emulator speaks transport protocol %s, this component speaks %d\n", got, transportproto.Version)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := transportproto.Serve(ctx, os.Stdin, os.Stdout, transport{}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
`

// readme is the one document a scaffold ships: the two things — the release-archive
// layout and the install flow — that the docs otherwise make an author work out.
func (s Scaffold) readme() string {
	var b strings.Builder

	fmt.Fprintf(&b, "# %s\n\n", s.Name)
	fmt.Fprintf(&b, "A fft %s component, scaffolded by `fft component init`.\n\n", s.Kind)

	b.WriteString("## Build and install locally\n\n")
	b.WriteString("```sh\n")
	switch s.Lang {
	case LangGo:
		b.WriteString("go mod tidy\n")
		fmt.Fprintf(&b, "go build -o bin/%s .\n", s.execName())
	case LangPython:
		b.WriteString("# needs python3 on PATH\n")
	case LangNode:
		b.WriteString("# needs node on PATH\n")
	}
	b.WriteString("fft component install --path .\n")
	b.WriteString("```\n\n")

	if s.Kind == KindCommand {
		fmt.Fprintf(&b, "Then `fft %s` runs it. Replace the executable's body with your own logic.\n\n", s.Name)
	} else {
		fmt.Fprintf(&b, "The fft emulator starts it to deliver the `%s` target type. Replace the\n", placeholderTarget)
		b.WriteString("placeholder target and the `hello`/`plan`/`send` bodies with a real broker client.\n\n")
	}

	b.WriteString("## Publishing a release\n\n")
	b.WriteString("`fft component install owner/repo` installs from a GitHub release. Each release\n")
	b.WriteString("archive is one component for one platform, named\n\n")
	fmt.Fprintf(&b, "    fft-component-%s_<version>_<os>_<arch>\n\n", s.Name)
	fmt.Fprintf(&b, "with `component.yaml` at its root and the executable under `bin/%s`. Publish a\n", s.execName())
	b.WriteString("`checksums.txt` alongside the archives — fft refuses to install a release without\n")
	b.WriteString("one. GoReleaser produces this layout; see the fft repository's `.goreleaser.yaml`.\n")

	return b.String()
}
