---
title: fft routing strategy evaluate
---

# fft routing strategy evaluate

Dry-run a strategy against an order

Dry-run a routing strategy against an order.

This walks the strategy for the order in the file and reports the path the engine
would take — each node it entered and whether that node's config passed. It reserves
nothing and creates nothing; it is a what-if, safe to run against the live strategy.

  fft routing strategy evaluate 3f9c... --file order.json
  fft routing strategy evaluate 3f9c... --example > order.json

The body is an order (the same shape 'fft order create' takes). The table lists the
evaluated path; -o json carries the full result, including the evaluated config.

--file - reads the order from stdin.

## Usage

```
fft routing strategy evaluate <id> --file <order.json> [flags]
```

## Flags

```
      --example       Print a sample order body and exit
      --file string   JSON file holding the order to evaluate ('-' for stdin)
```

## See also

- [fft routing strategy](./fft_routing_strategy.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
