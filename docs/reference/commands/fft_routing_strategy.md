---
title: fft routing strategy
---

# fft routing strategy

Manage routing strategies

Manage routing strategies.

A routing strategy is the decision tree the engine walks to source an order: a root
node with fences that reject ineligible facilities and ratings that rank the rest,
then conditions that branch to further nodes. Strategies are drafted, edited and then
made live one at a time — only one is ever 'inUse'.

  fft routing strategy list
  fft routing strategy get &lt;id>
  fft routing strategy activate &lt;id>

'get -o json' prints the whole tree; the table is a one-line summary. 'evaluate'
dry-runs a strategy against an order and reserves nothing.

## Usage

```
fft routing strategy
```

## Subcommands

- [fft routing strategy actions](./fft_routing_strategy_actions.md) — Run one action against a routing strategy
- [fft routing strategy activate](./fft_routing_strategy_activate.md) — Make a routing strategy live
- [fft routing strategy create](./fft_routing_strategy_create.md) — Create a routing strategy
- [fft routing strategy evaluate](./fft_routing_strategy_evaluate.md) — Dry-run a strategy against an order
- [fft routing strategy evaluate-node](./fft_routing_strategy_evaluate-node.md) — Dry-run one node of a strategy
- [fft routing strategy get](./fft_routing_strategy_get.md) — Show one routing strategy
- [fft routing strategy list](./fft_routing_strategy_list.md) — List the routing strategies
- [fft routing strategy update](./fft_routing_strategy_update.md) — Replace a routing strategy (PUT)

## See also

- [fft routing](./fft_routing.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
