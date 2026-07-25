---
title: fft component upgrade
---

# fft component upgrade

Upgrade an installed component

Reinstall a component from the latest release of wherever it came from.

The source recorded at install time is where fft looks; a component installed
with --path has no release to upgrade from and is refused rather than silently
reinstalled from somewhere else.

An upgrade is an install with the same confirmation: you see the new version and
what it asks for before anything is replaced. A failed upgrade leaves the working
version in place.

## Usage

```
fft component upgrade <name>
```

## See also

- [fft component](./fft_component.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
