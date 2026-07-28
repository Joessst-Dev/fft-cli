---
title: fft routing strategy activate
---

# fft routing strategy activate

Make a routing strategy live

Make a routing strategy live.

Only one strategy is ever 'inUse'. Activating this one takes over from whichever was
live before, and the engine starts routing with it immediately.

  fft routing strategy activate 3f9c1e77-2b4a-4f0e-9d61-8a2c5b7e4d10

Activation is versioned: fft reads the strategy to learn its current version, sends
it with the request, and retries once if somebody wrote in between. --if-version
skips that read — fft sends the version you name and the API rejects it with a 409 if
it is stale.

## Usage

```
fft routing strategy activate <id> [flags]
```

## Flags

```
      --if-version int   Send this version instead of reading the current one (fails with 409 if it is stale)
```

## See also

- [fft routing strategy](./fft_routing_strategy.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
