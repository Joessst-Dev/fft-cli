---
title: fft order
---

# fft order

Manage orders

Manage the orders of your tenant.

An order is the demand the platform fulfills: what a consumer asked for, where it
goes, and how. It is the object every fulfillment flow starts from — the platform
sources and routes an order into the pickjobs, packjobs and handovers that carry
it out. You do not create those directly; they arise from the order.

An order moves through OPEN, PROMISED, LOCKED, CANCELLED and OBSOLETE. Reading is
cheap; every write is versioned. A mutation reads the order first to learn its
current version and sends that version back — the API rejects a write that carries
a stale one. Pass --if-version to skip that read when you already know the
version: you get a clean 409 instead of a silent overwrite if you were wrong.

&lt;id> is the order's platform id, the one 'fft order list' and 'fft order get'
print. There is no tenantFacilityId-style shorthand for orders.

## Usage

```
fft order [flags]
```

## Subcommands

- [fft order cancel](./fft_order_cancel.md) — Cancel an order
- [fft order create](./fft_order_create.md) — Create an order
- [fft order get](./fft_order_get.md) — Show one order
- [fft order list](./fft_order_list.md) — List orders
- [fft order search](./fft_order_search.md) — Search orders (BETA)
- [fft order unlock](./fft_order_unlock.md) — Unlock a locked order
- [fft order update](./fft_order_update.md) — Update an order (PATCH)

## See also

- [fft](./fft.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
