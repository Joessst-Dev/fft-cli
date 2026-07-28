---
title: fft routing category update
---

# fft routing category update

Replace a routing category (PUT)

Replace a routing node config category (PUT).

The category becomes what the file says; there is no PATCH. Read it, edit it, send it
back:

  fft routing category get 3f9c... -o json > c.json
  $EDITOR c.json
  fft routing category update 3f9c... --file c.json

fft supplies the version: it reads the category first to learn the current one and
retries once on a conflict. --if-version skips that read and fails cleanly with a 409
if the version you name is stale.

## Usage

```
fft routing category update <id> --file <file> [flags]
```

## Flags

```
      --file string      JSON file holding the whole category ('-' for stdin)
      --if-version int   Send this version instead of reading the current one (fails with 409 if it is stale)
```

## See also

- [fft routing category](./fft_routing_category.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
