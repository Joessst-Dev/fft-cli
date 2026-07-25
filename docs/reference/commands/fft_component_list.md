---
title: fft component list
---

# fft component list

List installed components

List the components fft can see.

STATUS is "installed" when the binary is there and "available" when fft knows
about the component but it has not been installed — which only ever applies to a
component fft ships itself, since those are registered whether or not they are
present. ORIGIN says whether fft ships it or somebody else does.

A directory under the component root that fft cannot read as a component is
reported on stderr and left out of the list.

## Usage

```
fft component list
```

## See also

- [fft component](./fft_component.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
