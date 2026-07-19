---
title: fft emulator
---

# fft emulator

Run a local offline fulfillmenttools API emulator

Run a local server that mimics the fulfillmenttools API.

Every operation the API has is addressable on the emulator. The top-level
collections (facilities, listings, stocks, orders, …) are stateful: a POST is
remembered, a GET reflects it, versions and pagination work. Everything else is
answered from a response synthesized from the spec — reachable, but not remembered.

The emulator makes no request to any tenant and holds all state in memory, so it
dies with the process. Point fft at it with the FFT_* recipe it prints on startup;
'fft project add' does not work against it, because signing in reaches Google's
identity service, which a local server cannot stand in for.

## Usage

```
fft emulator [flags]
```

## Flags

```
      --host string                   Interface to bind; the emulator has no auth, so it stays on loopback unless you widen it (0.0.0.0 for a container) (default "127.0.0.1")
      --port int                      Port to listen on (default 8080)
      --pubsub-emulator-host string   Local Pub/Sub emulator (host:port) to publish events to; defaults to $PUBSUB_EMULATOR_HOST, empty disables eventing
      --seed string                   Directory of JSON fixtures to preload, one <collection>.json per collection
      --verbose                       Log every request to stderr
```

## Subcommands

- [fft emulator emit](./fft_emulator_emit.md) — Publish an event to a running emulator's subscriptions

## See also

- [fft](./fft.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
