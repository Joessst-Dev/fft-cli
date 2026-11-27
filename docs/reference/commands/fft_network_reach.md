---
title: fft network reach
editLink: false
---

# fft network reach

Show which connections lead to a facility, and why the others do not

Show which connections lead to one facility for one kind of order, and why the
others do not.

It answers the question behind a no-source or peer-coverage finding: which
connection should reach this store, and which context entry or exclusion stops
it? It reads the snapshot and sends no request.

  fft network reach --to BER-01
  fft network reach --to BER-01 --tag orderType=express
  fft network reach --to BER-01 --tag orderType=bulky --result excluded -o json

--tag gives the order's tags, as id=value, once per tag; without it the order
carries none. Every connection from an online source whose target could be the
facility is listed: one that names it, and every wildcard of its type. RESULT is
applies, conditional (it depends on an item's category or the recipient, which
tags do not say) or excluded, and REASON names the first thing that rules the
facility out, in the order the router meets it: the target, its exclusions, then
each context entry.

Sources are hubs, stores and suppliers as 'fft network check' decides them,
following fft-network.yaml when there is one. stdout carries the connections;
the counts go to stderr.

## Usage

```
fft network reach --to <facility> [flags]
```

## Flags

```
      --config string     The config file whose roles to use, when it exists (default "fft-network.yaml")
      --result string     Only connections with this result: applies, conditional, excluded
      --snapshot string   Read this snapshot instead of the project's file in the cache directory
      --tag stringArray   A tag the order carries, as id=value (repeatable)
      --to string         The facility, by tenant id or platform id
```

## See also

- [fft network](./fft_network.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
