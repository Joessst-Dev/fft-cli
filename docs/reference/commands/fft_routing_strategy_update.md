---
title: fft routing strategy update
---

# fft routing strategy update

Replace a routing strategy (PUT)

Replace a routing strategy with the contents of a JSON file.

This is a PUT, and there is no PATCH: the strategy becomes what the file says and
loses anything the file omits. The body must carry the whole strategy — name, root
node and global configuration — so the way to change one fence is to read the whole
strategy, edit it, and send it back:

  fft routing strategy get 3f9c... -o json > s.json
  $EDITOR s.json
  fft routing strategy update 3f9c... --file s.json

fft supplies the version. The API locks optimistically and carries the version in the
body rather than in a header, so fft reads the strategy first to learn the current
one and retries once if somebody wrote in between.

--if-version skips the read: fft sends the version you name and the API rejects it if
it is stale — one request instead of two, and a clean 409 instead of a silent
overwrite.

## Usage

```
fft routing strategy update <id> --file <file> [flags]
```

## Flags

```
      --file string      JSON file holding the whole strategy ('-' for stdin)
      --if-version int   Send this version instead of reading the current one (fails with 409 if it is stale)
```

## See also

- [fft routing strategy](./fft_routing_strategy.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
