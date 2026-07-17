---
title: fft order cancel
---

# fft order cancel

Cancel an order

Cancel an order.

This is the CANCEL order action. It is versioned like every write: fft reads the
order to learn its version, sends that back, and retries once on a 409. Pass
--reason-id to record a configured cancelation reason.

--force sends FORCE_CANCEL instead, which cancels an order past the point normal
cancellation allows. It only works if the tenant has enabled forced cancellation;
otherwise the API refuses it. FORCE_CANCEL takes no reason, so --force and
--reason-id cannot be combined.

Cancelling cannot be undone, so fft asks first. -y/--yes answers for you; on a
non-interactive terminal fft refuses rather than assuming yes.

  fft order cancel 8f14e45f-ceea-467a-9575-25a1b5c8b3a1 --reason-id out-of-stock
  fft order cancel 8f14e45f-ceea-467a-9575-25a1b5c8b3a1 --force --yes

## Usage

```
fft order cancel <id> [flags]
```

## Flags

```
      --force              Force-cancel past the point normal cancellation allows (needs the tenant to permit it)
      --if-version int     Send this version instead of reading the current one (fails with 409 if it is stale)
      --reason-id string   The id of a configured cancelation reason
```

## See also

- [fft order](./fft_order.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
