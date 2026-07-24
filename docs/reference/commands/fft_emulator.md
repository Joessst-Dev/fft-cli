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

Provided by the emulator component (Joessst-Dev/fft-cli), which runs as you.
fft gives it no tenant credential.

It is not installed. Run 'fft component install emulator'.

## Usage

```
fft emulator [flags] [args]
```

## See also

- [fft](./fft.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
