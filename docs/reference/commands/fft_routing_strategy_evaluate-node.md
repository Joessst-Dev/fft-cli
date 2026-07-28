---
title: fft routing strategy evaluate-node
---

# fft routing strategy evaluate-node

Dry-run one node of a strategy

Dry-run one node of a routing strategy.

Where 'evaluate' walks the whole strategy for an order, this evaluates a single node
and returns the path from it. It reserves nothing and creates nothing — a what-if,
safe against the live strategy.

  fft routing strategy evaluate-node 3f9c1e77-2b4a-4f0e-9d61-8a2c5b7e4d10 root-node-id

The node id is the one 'fft routing strategy get -o json' prints, and the one
'evaluate' names in its REF column. The table lists the evaluated path; -o json
carries the full result.

## Usage

```
fft routing strategy evaluate-node <id> <nodeId>
```

## See also

- [fft routing strategy](./fft_routing_strategy.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
