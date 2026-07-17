---
title: fft order unlock
---

# fft order unlock

Unlock a locked order

Unlock a locked order.

This is the UNLOCK order action: it releases an order the platform has put in the
LOCKED state so it can be sourced and routed again. It is versioned like every
write — fft reads the order to learn its version, sends that back, and retries once
on a 409.

--target-time optionally sets a delivery targetTime while unlocking.

  fft order unlock 8f14e45f-ceea-467a-9575-25a1b5c8b3a1
  fft order unlock 8f14e45f-ceea-467a-9575-25a1b5c8b3a1 --target-time 2026-07-20T12:00:00Z

## Usage

```
fft order unlock <id> [flags]
```

## Flags

```
      --if-version int       Send this version instead of reading the current one (fails with 409 if it is stale)
      --target-time string   Set this delivery targetTime while unlocking (RFC 3339)
```

## See also

- [fft order](./fft_order.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
