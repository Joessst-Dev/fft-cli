---
title: fft routing strategy get
---

# fft routing strategy get

Show one routing strategy

Show one routing strategy.

  fft routing strategy get 3f9c1e77-2b4a-4f0e-9d61-8a2c5b7e4d10

The table is a one-line summary — name, revision, whether it is live, version. -o json
prints the whole thing: the root node, every fence and rating, the conditions that
branch to further nodes, and the global configuration. That JSON is also what 'update'
expects back, so this is where a round-trip edit starts.

## Usage

```
fft routing strategy get <id>
```

## See also

- [fft routing strategy](./fft_routing_strategy.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
