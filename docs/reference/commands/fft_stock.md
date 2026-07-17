---
title: fft stock
---

# fft stock

Manage stocks (the quantity of an article at a facility)

Manage the stocks of your tenant.

A stock is a QUANTITY: how many of one article are at one location of one
facility. It is not the catalog entry — that is a listing, 'fft listing'. The
two were split in 2023 and are separate entities: an article can be listed as
ACTIVE with no stock at all, and can have stock while its listing is INACTIVE.
If you are asking "do we offer this", you want 'fft listing'.

  fft stock list --facility BER-01
  fft stock create --tenant-article-id 4711 --facility BER-01 --value 12
  fft stock summary --facility BER-01

Stocks are versioned. Every mutation reads the stock first to learn its current
version and sends that version back; --if-version skips the read when you
already know it.

A stock is created against exactly one facility, named as one of "facility",
"facilityRef" or "tenantFacilityId". The API marks none of the three as
required, so a body with none of them — or with two — fails server-side with an
error that does not say which. fft checks first.

## Usage

```
fft stock [flags]
```

## Subcommands

- [fft stock actions](./fft_stock_actions.md) — Run one action against the tenant's stocks (collection-level)
- [fft stock create](./fft_stock_create.md) — Create a stock
- [fft stock delete](./fft_stock_delete.md) — Delete one stock
- [fft stock get](./fft_stock_get.md) — Show one stock
- [fft stock list](./fft_stock_list.md) — List stocks
- [fft stock search](./fft_stock_search.md) — Search stocks with a JSON query
- [fft stock summary](./fft_stock_summary.md) — Show the accumulated stock of each article
- [fft stock update](./fft_stock_update.md) — Replace a stock (PUT)
- [fft stock upsert](./fft_stock_upsert.md) — Create and update many stocks at once (chunked, per-item result)

## See also

- [fft](./fft.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
