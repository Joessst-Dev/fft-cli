---
title: fft network
editLink: false
---

# fft network

Snapshot a fulfillment network and find its routing gaps

Find the gaps in a fulfillment network before an order does.

Orders reach the wrong facility mostly because the connection rules have gaps: a
store no hub reaches, a NOT filter that shuts out a kind of order, a connection to
a facility that has gone offline. Nothing in the API reports those. This group
reads the whole network once into a local snapshot, and works on that:

  fft network snapshot      read every facility and connection into a local file

Everything here only reads from the tenant. Fixing a gap is a change to the
network, and that is its owner's decision, not a linter's.

## Usage

```
fft network
```

## Subcommands

- [fft network snapshot](./fft_network_snapshot.md) — Read every facility and connection into a local snapshot

## See also

- [fft](./fft.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
