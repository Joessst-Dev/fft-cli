---
title: fft emulator emit
---

# fft emulator emit

Publish an event to a running emulator's subscriptions

Publish a fulfillmenttools event to a running emulator.

The emulator publishes lifecycle events on its own when you create, update or delete
an entity (POST /api/orders emits ORDER_CREATED, and so on). This reaches the events
that no such mutation does — a picking or routing state change — by asking the
emulator to publish one, with a payload you supply, to every subscription that
matches its name and contexts.

It talks to a running emulator over HTTP; it makes no request to any tenant. Point it
with --url, or let it read $FFT_BASE_URL — the same value the emulator's startup
recipe exports.

## Usage

```
fft emulator emit <EVENT> [flags]
```

## Flags

```
      --payload-file string   File (or - for stdin) with the event payload JSON; defaults to an empty object
      --url string            Base URL of the running emulator; defaults to $FFT_BASE_URL or http://localhost:8080 (default "http://localhost:8080")
```

## See also

- [fft emulator](./fft_emulator.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
