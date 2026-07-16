---
title: fft listing
---

# fft listing

Manage listings (the article-at-facility catalog entry)

Manage the listings of your tenant.

A listing is the CATALOG entry for one article at one facility: whether that
facility offers the article at all (status ACTIVE or INACTIVE), what it is
called, what it costs, what it weighs.

A listing is NOT the quantity. The quantity is a stock — 'fft stock'. The two
were split in 2023 and are separate entities: a listing can be ACTIVE with no
stock at all (nothing to sell yet), and a stock can exist for an article whose
listing is INACTIVE (goods on the shelf, not offered). If you are asking "how
many are there", you want 'fft stock'.

Listings are addressed by your own tenantArticleId, not by a platform UUID —
alone among the entities in this API. --facility takes either the platform's
UUID or your own tenantFacilityId.

  fft listing list  --facility BER-01
  fft listing get   --facility BER-01 4711
  fft listing patch --facility BER-01 4711 --status INACTIVE

## Usage

```
fft listing [flags]
```

## Subcommands

- [fft listing delete](./fft_listing_delete.md) — Delete one listing from a facility
- [fft listing get](./fft_listing_get.md) — Show one listing
- [fft listing list](./fft_listing_list.md) — List the listings of a facility
- [fft listing patch](./fft_listing_patch.md) — Change some fields of one listing
- [fft listing purge](./fft_listing_purge.md) — Delete every listing of a facility (destructive)
- [fft listing search](./fft_listing_search.md) — Search listings with a JSON query
- [fft listing set](./fft_listing_set.md) — Put a set of listings into a facility (PUT, all-or-nothing)
- [fft listing upsert](./fft_listing_upsert.md) — Upsert listings across facilities (chunked, per-item result)

## See also

- [fft](./fft.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
