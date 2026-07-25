---
title: fft component remove
---

# fft component remove

Remove an installed component

Remove an installed component.

The component's directory is deleted, and its commands stop appearing in
'fft --help'. A component fft ships stays listed as "available", because fft
knows about it whether or not it is installed.

fft refuses a directory that holds no component manifest, whatever the name says:
the name comes from a shell, where a typo is one keystroke, and fft will not
recursively delete a directory it cannot prove it created.

## Usage

```
fft component remove <name>
```

## See also

- [fft component](./fft_component.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
