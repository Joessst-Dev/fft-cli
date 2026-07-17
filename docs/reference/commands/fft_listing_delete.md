---
title: fft listing delete
---

# fft listing delete

Delete one listing from a facility

Delete ONE listing from a facility.

This removes the article from that facility's catalog. It does not touch the
article's listings at other facilities, and it does not touch its stock.

To delete every listing of a facility, there is a separate command — 'fft
listing purge'. It is separate on purpose: a destructive verb should not be one
forgotten argument away from a merely careless one.

  fft listing delete --facility BER-01 4711

fft asks first; -y/--yes answers for you. On a non-interactive terminal there is
nobody to ask, and fft refuses rather than assuming yes.

## Usage

```
fft listing delete --facility <id> <tenantArticleId> [flags]
```

## Flags

```
      --facility string   The facility, by tenantFacilityId or platform UUID (required)
```

## See also

- [fft listing](./fft_listing.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
