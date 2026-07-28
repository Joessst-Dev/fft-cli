---
title: fft routing
---

# fft routing

Configure order routing

Configure how orders are routed.

The routing engine decides which facility fulfils an order. This group reaches the
pieces of that decision you can shape:

  fft routing strategy list        the strategies, one of which is 'inUse'
  fft routing category list        the node config categories a strategy references
  fft routing decision-logs        why the router chose what it chose

A strategy is a tree of nodes, each with fences (which facilities are eligible) and
ratings (which is preferred). Only one strategy is live at a time — 'fft routing
strategy activate' is how you switch — and 'fft routing strategy evaluate' dry-runs
one against an order without touching a thing.

'fft sourcing simulate' is the neighbouring tool: it shows the answer the live
strategy produces for an order, named down to each connection it would use.

## Usage

```
fft routing
```

## Subcommands

- [fft routing category](./fft_routing_category.md) — Manage routing node config categories
- [fft routing decision-logs](./fft_routing_decision-logs.md) — Show routing decision logs
- [fft routing strategy](./fft_routing_strategy.md) — Manage routing strategies

## See also

- [fft](./fft.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
