---
title: fft network check
editLink: false
---

# fft network check

Find the routing gaps in a network snapshot

Find the routing gaps in a network snapshot.

It reads the snapshot 'fft network snapshot' wrote and sends no request, so it
runs in CI, offline, and as often as you like against the same data.

  fft network check
  fft network check --rule no-source --rule peer-coverage
  fft network check -o json | jq -r '.[] | select(.rule == "peer-coverage") | .subject'

The rules:

  no-source       a store no online source reaches, for some kind of order
  peer-coverage   a store no hub (or supplier) reaches, although one reaches at
                  least 90% of the stores in its country (groups of 10 or more)
  dangling-ref    a target, exclusion or context value that names a facility,
                  group or tag value the network does not have
  offline-ref     an offline facility that connections still lead to
  duplicate-edge  connections of one source that are the same connection

A kind of order is the tags it carries, as far as any connection reads them,
taken from the orders the snapshot's decision logs routed. Hubs are the facilities
whose connections reach far more facilities than anybody else's. What the check
decided about both is printed on stderr, with how many of each country's stores a
hub and a supplier reach: a gap most of a country shares is no outlier, and only
that line shows it.

stdout carries the findings and nothing else; -o json adds each one's evidence
and next step. Exit 0 means no problems; exit 11 means the check ran and found
some. Fixing a gap is a change to the network, and that is its owner's decision.

## Usage

```
fft network check [flags]
```

## Flags

```
      --config string      The config file; when it is the default and does not exist, the check infers everything (default "fft-network.yaml")
      --rule stringArray   Run only this rule (repeatable): no-source, peer-coverage, expect, dangling-ref, offline-ref, duplicate-edge, config-drift
      --snapshot string    Read this snapshot instead of the project's file in the cache directory
```

## See also

- [fft network](./fft_network.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
