---
title: fft listing upsert
---

# fft listing upsert

Upsert listings across facilities (chunked, per-item result)

Upsert listings across facilities, from one file.

This is PUT /api/listings: unlike 'fft listing set' it is tenant-wide, so one
file can write the same article into fifty stores, and it reports per item
rather than failing as a block.

  fft listing upsert --example > bulk.json
  $EDITOR bulk.json
  fft listing upsert --file bulk.json

Every entry names its targets with a targetingStrategy:

  SINGLE_FACILITY  "facility": { "tenantFacilityId": "BER-01" }
  MULTI_SELECTOR   "selector": [ { "facility": { "tenantFacilityId": "BER-01" } },
                                 { "facility": { "facilityRef": "8f14e45f-..." } } ]

The API caps one request at 25 (entries × selectors). A real catalog import is
far larger than that, so fft SPLITS the file into as many requests as it takes
and reports the outcome of every entry — you write the file you mean, and fft
deals with the limit. Nothing is dropped: every entry of the file is sent, and
every entry appears in the result table.

Chunks are sent one after another, and a chunk that fails does not stop the
ones after it. If some entries land and others do not, the command exits 8 and
the FAILED rows say why — re-send only those.

## Usage

```
fft listing upsert --file <file> [flags]
```

## Flags

```
      --example       Print a sample request body and exit
      --file string   JSON file holding {"listings": [...]} ('-' for stdin)
```

## See also

- [fft listing](./fft_listing.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
