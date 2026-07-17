---
title: fft order list
---

# fft order list

List orders

List the orders of your tenant.

This is GET /api/orders: it pages by startAfterId and returns a reduced projection
of each order — id, status, orderDate, line count and version. It filters only on
tenantOrderId and consumerId, both exact matches. For status, a date range or a
sort, use 'fft order search', which is the richer POST /api/orders/search.

By default you get the first page. --all pages to the end, and says so on stderr
if it stops early rather than pretending it reached it.

  fft order list --consumer-id C-4711
  fft order list --all -o json | jq -r '.[].id'
  fft order list --tenant-order-id ORD-2026-0001 --total

stdout carries the orders and nothing else. The total, the truncation notice and
every other remark go to stderr, so a pipe into jq is never contaminated.

## Usage

```
fft order list [flags]
```

## Flags

```
      --all                      Page to the end and return every match, not just the first page
      --consumer-id string       Only orders for this consumerId
      --max-items int            With --all, stop after this many orders (default 10000)
      --size int                 Orders per page, 1–250 (default 25)
      --tenant-order-id string   Only the order with this tenantOrderId
      --total                    Also count the matches, and report the total on stderr
```

## See also

- [fft order](./fft_order.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
