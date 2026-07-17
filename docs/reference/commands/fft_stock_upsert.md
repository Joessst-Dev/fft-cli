---
title: fft stock upsert
---

# fft stock upsert

Create and update many stocks at once (chunked, per-item result)

Create and update many stocks at once, from one file.

This is PUT /api/stocks. It is how a nightly inventory sync writes: one file,
one pass, a result per stock.

  fft stock upsert --example > stocks.json
  $EDITOR stocks.json
  fft stock upsert --file stocks.json

An entry that carries an "id" updates that stock. An entry without one creates a
stock, and must name its article, its quantity and exactly one facility — the
same rule 'fft stock create' enforces, for the same reason.

The API accepts 500 stocks per request, so fft SPLITS a larger file into as many
requests as it takes. A chunk that fails does not stop the ones after it: the
entries of a failed chunk are reported FAILED with the API's own message, and
the command exits 8 — some of your stocks landed and some did not, and that is
worth an exit code of its own.

Do not name the same stock twice in one file. The API rejects the whole batch if
you do, and fft says so before sending.

## Usage

```
fft stock upsert --file <file> [flags]
```

## Flags

```
      --example       Print a sample request body and exit
      --file string   JSON file holding {"stocks": [...]} ('-' for stdin)
```

## See also

- [fft stock](./fft_stock.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
