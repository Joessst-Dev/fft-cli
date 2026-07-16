---
title: fft listing set
---

# fft listing set

Put a set of listings into a facility (PUT, all-or-nothing)

Put a set of listings into one facility's catalog.

This is PUT /api/facilities/{id}/listings. It creates the listings that do not
exist and replaces the ones that do — for the articles named in the file, and
only those. Listings the file does not mention are left alone; this is not a
replacement of the whole catalog. (To empty a catalog, use 'fft listing purge'.)

  fft listing set --example > listings.json
  $EDITOR listings.json
  fft listing set --facility BER-01 --file listings.json

The API calls this endpoint legacy for one reason worth knowing: it is
all-or-nothing. If a single listing in the file is rejected, the whole PUT is
rejected. fft therefore does NOT split the file into chunks — doing so would
turn one atomic write into several, which is not what you asked for. To write
across facilities, or to write a catalog too large for one request, use
'fft listing upsert', which chunks on purpose and reports per item.

The answer is a per-item result table. Any FAILED item exits 8.

Only tenantArticleId is required per listing. --file - reads from stdin.

## Usage

```
fft listing set --facility <id> --file <file> [flags]
```

## Flags

```
      --example           Print a sample request body and exit
      --facility string   The facility, by tenantFacilityId or platform UUID (required)
      --file string       JSON file holding {"listings": [...]} ('-' for stdin)
```

## See also

- [fft listing](./fft_listing.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
