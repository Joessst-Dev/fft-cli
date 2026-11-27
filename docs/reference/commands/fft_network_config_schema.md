---
title: fft network config schema
editLink: false
---

# fft network config schema

Print the JSON Schema of fft-network.yaml

Print the JSON Schema of fft-network.yaml.

Point an editor's YAML support at it for completion and inline errors, or hand it
to an agent before it proposes a change to the file.

  fft network config schema > fft-network.schema.json

The schema checks the file's shape. 'fft network config validate' also checks
what only fft can: that expectation names are unique, that suppressions have not
expired, and that what the file names exists in the network.

## Usage

```
fft network config schema
```

## See also

- [fft network config](./fft_network_config.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
