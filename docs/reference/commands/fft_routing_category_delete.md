---
title: fft routing category delete
---

# fft routing category delete

Delete a routing category

Delete a routing node config category.

A category is only a label, so deleting one removes no routing logic — but any node
that referenced it loses that grouping. This cannot be undone.

fft reads the category first so it can ask about it by name rather than by UUID.
-y/--yes answers for you. On a non-interactive terminal there is nobody to ask, and
fft refuses rather than assuming yes.

## Usage

```
fft routing category delete <id>
```

## See also

- [fft routing category](./fft_routing_category.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
