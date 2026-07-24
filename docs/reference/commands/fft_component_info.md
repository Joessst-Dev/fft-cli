---
title: fft component info
---

# fft component info

Show a component's manifest

Show what a component is and what it adds.

Under -o json or -o yaml this prints the component's manifest as fft read it:
which commands it registers, which session each of them receives, whether it
declares that it changes the tenant, and which environment variables it consumes.

That is the whole of what fft knows about a component before running it, so it is
also the whole of what there is to review.

## Usage

```
fft component info <name>
```

## See also

- [fft component](./fft_component.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
