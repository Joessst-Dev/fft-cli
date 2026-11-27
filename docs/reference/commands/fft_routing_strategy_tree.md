---
title: fft routing strategy tree
editLink: false
---

# fft routing strategy tree

Show a routing strategy as the tree the router walks

Show a routing strategy as the tree the router walks.

The API stores a strategy as a linked list of conditions; this draws it as the
tree it is. Each condition leads to a node when its rule holds, and the router
takes the first condition that holds, top to bottom. Each node shows the fences
and ratings it adds, which every node below it inherits.

  fft routing strategy tree
  fft routing strategy tree 3f9c1e77-2b4a-4f0e-9d61-8a2c5b7e4d10 --all
  fft routing strategy tree --order 0190d6e8-8c3a-7f1b-9d2e-4a6b8c0d1e2f

Without an id it shows the strategy in use. Inactive branches are folded into one
line; --all unfolds them.

--order marks the branch the router took for an order, read from the order's most
recent routing decision log: ▶ on what it took, ✗ on a condition it evaluated
and found false. A simulated routing ('fft sourcing simulate') leaves no decision
log, so it cannot be marked.

-o json prints the tree as fft draws it, marks included; 'fft routing strategy
get -o json' prints the API's own document. Every request is a read.

## Usage

```
fft routing strategy tree [<id>] [flags]
```

## Flags

```
      --all[=true|false]   Unfold inactive branches
      --order string       Mark the branch the router took for this order (its platform id)
```

## See also

- [fft routing strategy](./fft_routing_strategy.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
