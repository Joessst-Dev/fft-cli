---
title: fft history top
editLink: false
---

# fft history top

List the operations used most

List the operations used most, most used first.

The count is per project, and covers the current project — --project, or the
active one — unless --all-projects is given. Under -o json each operation is an
object with project, operationId, command, count and lastUsed.

## Usage

```
fft history top [flags]
```

## Flags

```
      --all-projects   Count every project's requests, each project separately
      --limit int      Show at most this many operations (0 for all) (default 10)
```

## See also

- [fft history](./fft_history.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
