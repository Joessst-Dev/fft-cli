---
title: fft version
---

# fft version

Print the fft version

Print the version, commit and build date of this fft binary.

Honours --output, so a script can read the version as JSON:

  fft version -o json | jq -r .version

## Usage

```
fft version
```

## See also

- [fft](./fft.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
