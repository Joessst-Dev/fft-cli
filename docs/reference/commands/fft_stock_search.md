---
title: fft stock search
---

# fft stock search

Search stocks with a JSON query

Search stocks with a query from a JSON file.

'fft stock list' covers the common filters. This is for the ones it does not:
value ranges, receipt and expiry dates, scannable codes, storage-location traits,
stock properties, custom attributes, and and/or trees.

  {
    "query": {
      "and": [
        { "facilityRef": { "eq": "8f14e45f-ceea-467a-9575-25a1b5c8b3a1" } },
        { "value": { "gt": 0 } }
      ]
    },
    "sort": [ { "value": "DESC" } ]
  }

fft checks the query against the API's schema before sending it, so a misspelled
field is a message that names the field rather than a 200 that quietly did not
filter.

--size, --total and --all override whatever the file said.

## Usage

```
fft stock search --file <file> [flags]
```

## Flags

```
      --all             Page to the end and return every match, not just the first page
      --example         Print a sample request body and exit
      --file string     JSON file holding the search payload ('-' for stdin)
      --max-items int   With --all, stop after this many stocks (default 10000)
      --size int        Stocks per page, 1–250 (default 20)
      --total           Also count the matches, and report the total on stderr
```

## See also

- [fft stock](./fft_stock.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
