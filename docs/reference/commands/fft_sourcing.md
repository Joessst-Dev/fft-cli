---
title: fft sourcing
---

# fft sourcing

Simulate how an order would be routed

Ask the routing engine where an order would be fulfilled from.

A sourcing option is a simulation, not a booking. You describe an order — who it
goes to and what is in it — and the router answers with the ways it could be
fulfilled: which facilities would pick it, which connections it would travel
along, what that would cost, and when it would arrive. Nothing is reserved, no
order is created, and no stock moves. It is safe on a read-only project, and fft
knows that: 'fft sourcing simulate' is a POST that does not write.

  fft sourcing simulate --example > order.json
  $EDITOR order.json
  fft sourcing simulate --file order.json --results 3

Every option names the connections it would use, so this is the other half of
'fft connection': the router tells you which edge it chose, and 'fft connection
get' tells you what that edge is.

Two things to read carefully. An empty answer does not mean "no matches" — it
means the order cannot be routed at all. And the penalty is a penalty: the
option at the top of the table is the one the router likes best.

## Usage

```
fft sourcing
```

## Subcommands

- [fft sourcing get](./fft_sourcing_get.md) — Read a sourcing run back
- [fft sourcing simulate](./fft_sourcing_simulate.md) — Ask the router how an order would be fulfilled

## See also

- [fft](./fft.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
