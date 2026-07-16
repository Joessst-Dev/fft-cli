---
title: fft facility search
---

# fft facility search

Search facilities with a JSON query

Search facilities with a query from a JSON file.

'fft facility list' covers the common filters. This is for the ones it does not:
nested address and contact filters, custom attributes, regex on the name, and
and/or trees.

The file holds the request body of POST /api/facilities/search — the query, and
optionally sort, size and options:

  {
    "query": {
      "and": [
        { "status": { "eq": "ONLINE" } },
        { "address": { "city": { "eq": "Berlin" } } }
      ]
    },
    "sort": [ { "name": "ASC" } ]
  }

fft checks the query against the API's schema before sending it, so a misspelled
field is a message that names the field rather than a 400 that does not.

--size, --total and --all override whatever the file said.

## Usage

```
fft facility search --file <file> [flags]
```

## Flags

```
      --all             Page to the end and return every match, not just the first page
      --example         Print a sample request body and exit
      --file string     JSON file holding the search payload ('-' for stdin)
      --max-items int   With --all, stop after this many facilities (default 10000)
      --size int        Facilities per page, 1–250 (default 20)
      --total           Also count the matches, and report the total on stderr
```

## See also

- [fft facility](./fft_facility.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
