---
title: fft routing strategy list
---

# fft routing strategy list

List the routing strategies

List the routing strategies.

One of these is 'inUse' — the strategy the engine is routing with right now. The rest
are drafts and superseded revisions, kept so you can read, edit or re-activate them.

  fft routing strategy list
  fft routing strategy list --all -o json | jq -r '.[] | select(.inUse) | .id'

This endpoint pages by id rather than by cursor, so --all fetches every strategy and
stops at --max-items, saying so on stderr if it had to.

stdout carries the strategies and nothing else; the total and every notice go to
stderr.

## Usage

```
fft routing strategy list [flags]
```

## Flags

```
      --all             Page to the end and return every match, not just the first page
      --max-items int   With --all, stop after this many strategies (default 10000)
      --size int        Strategies per page, 1–250 (default 25)
      --total           Also count the matches, and report the total on stderr
```

## See also

- [fft routing strategy](./fft_routing_strategy.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
