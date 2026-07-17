---
title: fft connection delete
---

# fft connection delete

Delete a connection

Delete a connection of a facility.

This removes an edge from the fulfillment graph. Orders stop being sourced along
it immediately, and if it was the only way to reach a target, that target stops
being reachable at all — so this is rather more destructive than it looks, and
it cannot be undone.

fft reads the connection first so that it can ask about it by name rather than
by UUID. -y/--yes answers for you. On a non-interactive terminal there is nobody
to ask, and fft refuses rather than assuming yes.

## Usage

```
fft connection delete <id> --facility <id> [flags]
```

## Flags

```
      --facility string   The facility the connection leaves, by tenantFacilityId or platform UUID (required)
```

## See also

- [fft connection](./fft_connection.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
