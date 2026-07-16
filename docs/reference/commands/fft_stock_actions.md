---
title: fft stock actions
---

# fft stock actions

Run one action against the tenant's stocks (collection-level)

Run one action against the tenant's stocks.

This is POST /api/stocks/actions, and it is COLLECTION-level: there is no stock
id in the path. The action itself says which stocks it applies to — by location,
by article, or by a list of ids — and the API finds them. It is not a per-stock
action like a pickjob's, and there is no 'fft stock actions &lt;id>'.

That is what makes it useful: "delete every stock at this location" is one
request, not one request per stock plus a search to find them.

  fft stock actions --example > action.json
  $EDITOR action.json
  fft stock actions --file action.json

The five actions, all discriminated on "name":

  DELETE_BY_LOCATIONS   delete every stock at these locations
  DELETE_BY_PRODUCTS    delete every stock of these articles
  DELETE_BY_IDS         delete these stocks
  MOVE_TO_LOCATION      move stock to another location
  UPDATE_VERSIONLESS    upsert without an optimistic-locking version

Four of the five delete things, and none of them asks first — the file IS the
confirmation. Read it before you send it.

## Usage

```
fft stock actions --file <file> [flags]
```

## Flags

```
      --example       Print a sample request body and exit
      --file string   JSON file holding {"action": {...}} ('-' for stdin)
```

## See also

- [fft stock](./fft_stock.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
