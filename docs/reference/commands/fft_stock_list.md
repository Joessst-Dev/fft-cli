---
title: fft stock list
---

# fft stock list

List stocks

List stocks.

This is POST /api/stocks/search with a cursor. The legacy GET /api/stocks is not
used: it has a different page size (default 25, max 100 — do not confuse the
two), and it cannot express these filters.

  fft stock list --facility BER-01
  fft stock list --tenant-article-id 4711 --all
  fft stock list --facility BER-01 -o json | jq '[.[].value] | add'

With no filter you get every stock in the tenant, which on a real tenant is a
lot; --facility or --tenant-article-id is almost always what you want.

VALUE is what is on the shelf, RESERVED is what is already promised to an order,
and AVAILABLE is what is left to sell.

## Usage

```
fft stock list [flags]
```

## Flags

```
      --all                         Page to the end and return every match, not just the first page
      --facility string             Only the stocks of this facility, by tenantFacilityId or platform UUID
      --max-items int               With --all, stop after this many stocks (default 10000)
      --size int                    Stocks per page, 1–250 (default 20)
      --sort string                 Sort by one field, as field:asc or field:desc (tenantArticleId, value, locationName, lastModified)
      --tenant-article-id strings   Only the stocks of these articles
      --total                       Also count the matches, and report the total on stderr
```

## See also

- [fft stock](./fft_stock.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
