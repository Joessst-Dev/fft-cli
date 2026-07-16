---
title: fft stock summary
---

# fft stock summary

Show the accumulated stock of each article

Show the accumulated stock of each article.

This is GET /api/stocks/summaries: one row per article, with its stock added up
across the facilities you asked about — rather than one row per stock, which is
what 'fft stock list' gives you.

  fft stock summary --facility BER-01
  fft stock summary --tenant-article-id 4711 --tenant-article-id 4712
  fft stock summary --facility BER-01 -o json | jq '.[] | select(.details.availableOnStock == 0)'

ON HAND is what is physically there, RESERVED is what is promised to orders,
AVAILABLE is what is left to sell, and SAFETY is the buffer held back from
routing.

This endpoint has its own page size — default 25, maximum 100 — which is NOT the
search API's (default 20, maximum 250). They are different endpoints and the
numbers are not interchangeable.

## Usage

```
fft stock summary [flags]
```

## Flags

```
      --allow-stale                 Let the API answer from a cache: faster, and possibly out of date
      --facility strings            Only these facilities, by tenantFacilityId or platform UUID (repeatable)
      --size int                    Articles per page, 1–100 (default 25)
      --tenant-article-id strings   Only these articles (repeatable)
```

## See also

- [fft stock](./fft_stock.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
