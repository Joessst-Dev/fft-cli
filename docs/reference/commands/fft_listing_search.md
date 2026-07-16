---
title: fft listing search
---

# fft listing search

Search listings with a JSON query

Search listings tenant-wide with a query from a JSON file.

'fft listing list' covers the common filters, but is scoped to one facility.
This is POST /api/listings/search unfiltered: it spans every facility, and it
reaches the filters the flags do not — price ranges, tags, category refs, custom
attributes, and and/or trees.

  {
    "query": {
      "and": [
        { "status": { "eq": "ACTIVE" } },
        { "price": { "gte": 100 } }
      ]
    },
    "sort": [ { "tenantArticleId": "ASC" } ]
  }

fft checks the query against the API's schema before sending it, so a misspelled
field is a message that names the field rather than a 200 that quietly did not
filter.

--size, --total and --all override whatever the file said.

## Usage

```
fft listing search --file <file> [flags]
```

## Flags

```
      --all             Page to the end and return every match, not just the first page
      --example         Print a sample request body and exit
      --file string     JSON file holding the search payload ('-' for stdin)
      --max-items int   With --all, stop after this many listings (default 10000)
      --size int        Listings per page, 1–250 (default 20)
      --total           Also count the matches, and report the total on stderr
```

## See also

- [fft listing](./fft_listing.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
