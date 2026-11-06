---
title: fft order search
editLink: false
---

# fft order search

Search orders

Search the orders of your tenant.

This is POST /api/orders/search: unlike 'fft order list' it returns whole orders
and filters on status, a date range and a sort. It is guarded by the ORDER_READ
permission — the same permission 'fft order list' needs.

A narrow --since/--until is strongly recommended: the API pages far faster when
the orderDate range is bounded.

  fft order search --status OPEN
  fft order search --status LOCKED --status PROMISED --sort orderDate:desc
  fft order search --since 2026-07-01 --until 2026-07-15 --all -o json | jq -r '.[].id'

--since and --until are dates (2026-07-01) or full timestamps
(2026-07-01T00:00:00Z). stdout carries the orders and nothing else; the total and
the truncation notice go to stderr.

## Usage

```
fft order search [flags]
```

## Flags

```
      --all                      Page to the end and return every match, not just the first page
      --max-items int            With --all, stop after this many orders (default 10000)
      --since string             Only orders whose orderDate is on or after this date
      --size int                 Orders per page, 1–250 (default 20)
      --sort string              Sort by one field, as field:asc or field:desc (orderDate, status)
      --status strings           Only orders in these states: OPEN, PROMISED, LOCKED, CANCELLED, OBSOLETE
      --tenant-order-id string   Only the order with this tenantOrderId
      --total                    Also count the matches, and report the total on stderr
      --until string             Only orders whose orderDate is on or before this date
```

## See also

- [fft order](./fft_order.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
