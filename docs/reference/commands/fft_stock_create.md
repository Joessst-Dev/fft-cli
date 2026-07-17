---
title: fft stock create
---

# fft stock create

Create a stock

Create a stock: a quantity of one article at one facility.

The short form covers the common case:

  fft stock create --tenant-article-id 4711 --facility BER-01 --value 12

--facility takes your own tenantFacilityId or the platform's UUID, and fft sends
whichever of the two the API expects for the value you gave it.

For everything else — a location, an expiry date, scannable codes, stock
properties — use a file:

  fft stock create --example > stock.json
  $EDITOR stock.json
  fft stock create --file stock.json

The API requires only tenantArticleId and value. It targets a facility with one
of three fields — "facility", "facilityRef" or "tenantFacilityId" — and marks
NONE of them as required, so a body with none of them (or with two) is accepted
by the schema and rejected by the server with an error that does not say which.
fft therefore checks that exactly one is set, before sending anything.

A create is never retried. If the API answers 500 the stock may still have been
created, and sending the request again would risk creating a second one; fft
tells you instead of guessing.

## Usage

```
fft stock create [flags]
```

## Flags

```
      --example                    Print a sample request body and exit
      --facility string            The facility, by tenantFacilityId or platform UUID
      --file string                JSON file holding the stock ('-' for stdin)
      --tenant-article-id string   The article this is stock of
      --value int                  How many there are
```

## See also

- [fft stock](./fft_stock.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
