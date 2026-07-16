---
title: fft listing get
---

# fft listing get

Show one listing

Show one listing.

A listing is addressed by the tenantArticleId you gave the article — not by a
platform UUID. That is unlike every other entity in this API, and it is why the
article id is the positional argument while the facility is a flag:

  fft listing get --facility BER-01 4711
  fft listing get --facility BER-01 4711 -o json | jq .price

-o json prints the API's own JSON, in full. The table is a summary.

## Usage

```
fft listing get --facility <id> <tenantArticleId> [flags]
```

## Flags

```
      --facility string   The facility, by tenantFacilityId or platform UUID (required)
```

## See also

- [fft listing](./fft_listing.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
