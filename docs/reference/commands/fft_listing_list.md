---
title: fft listing list
---

# fft listing list

List the listings of a facility

List the listings of one facility.

This is POST /api/listings/search filtered to the facility, not the legacy
GET /api/facilities/{id}/listings: the search API returns whole listings and
pages on a cursor, while the legacy list returns a reduced projection.

  fft listing list --facility BER-01
  fft listing list --facility BER-01 --status ACTIVE --all
  fft listing list --facility BER-01 -o json | jq -r '.[].tenantArticleId'

--facility takes your own tenantFacilityId or the platform's UUID.

Remember what a listing is: the catalog entry, not the quantity. For quantities,
'fft stock list --facility BER-01'.

## Usage

```
fft listing list --facility <id> [flags]
```

## Flags

```
      --all                         Page to the end and return every match, not just the first page
      --facility string             The facility, by tenantFacilityId or platform UUID (required)
      --max-items int               With --all, stop after this many listings (default 10000)
      --size int                    Listings per page, 1–250 (default 20)
      --sort string                 Sort by one field, as field:asc or field:desc (tenantArticleId, lastModified)
      --status strings              Only listings in these states: ACTIVE, INACTIVE
      --tenant-article-id strings   Only the listings of these articles
      --total                       Also count the matches, and report the total on stderr
```

## See also

- [fft listing](./fft_listing.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
