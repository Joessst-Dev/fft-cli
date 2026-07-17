---
title: fft api list
---

# fft api list

List the API's operations

List the operations of the fulfillmenttools API.

The list comes from the spec, not from the network: it works offline and needs no
project. 557 operations is more than a screen, so filter.

  fft api list --tag picking
  fft api list --search pickjob
  fft api list --tag "Stocks (Inventory)" -o json | jq -r '.[].id'

--tag matches a tag by substring, case-insensitively, so "picking" finds
"Picking (Operations)". --search matches the operationId, the path and the
summary.

## Usage

```
fft api list [flags]
```

## Flags

```
      --search string   Only operations whose id, path or summary contains this
      --tag string      Only operations with this tag (substring, case-insensitive)
```

## See also

- [fft api](./fft_api.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
