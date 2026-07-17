---
title: fft stock update
---

# fft stock update

Replace a stock (PUT)

Replace a stock with the contents of a JSON file.

This is a PUT: the stock becomes what the file says and loses anything the file
omits.

  fft stock get 019c41f1-… -o json > stock.json
  $EDITOR stock.json
  fft stock update 019c41f1-… --file stock.json

The API has no If-Match header — optimistic locking travels in the body as a
"version" field — so fft reads the stock first to learn its current version,
sends that version back, and retries once if someone wrote in between. Your file
does not need a version; fft supplies it.

--if-version skips the read: fft sends the version you name and the API answers
409 if it is stale. That is one request instead of two, and a clean failure
instead of a silent overwrite — which is what a CI job wants. (It is
--if-version and never --version: cobra owns --version on the root command.)

To change a quantity across many stocks at once, 'fft stock upsert' is one
request rather than one per stock.

## Usage

```
fft stock update <stockId> --file <file> [flags]
```

## Flags

```
      --example          Print a sample request body and exit
      --file string      JSON file holding the whole stock ('-' for stdin)
      --if-version int   Send this version instead of reading the current one (fails with 409 if it is stale)
```

## See also

- [fft stock](./fft_stock.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
