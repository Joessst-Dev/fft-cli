---
title: fft network infer
editLink: false
---

# fft network infer

Write a starter fft-network.yaml from a network snapshot

Write a starter fft-network.yaml from what a check infers about the network.

Every value the check would otherwise guess goes into the file with the reason
it was chosen, as a comment above it: the hubs, how stores are grouped into
peers, where the kinds of order come from, and one expectation per country a
hub already covers.

  fft network infer
  fft network infer --out - > proposal.yaml
  fft network infer --snapshot network.json.gz --force

What you keep in the file is pinned. The check follows it, and reports
config-drift when the network moves away from it, so a changed result is a
reviewed change to the file. What you delete is inferred again on every run.
Commit the file; 'fft network config validate' checks it, and 'fft network
check' reads it from the working directory.

It reads the snapshot only and sends no request. It will not overwrite a file
without --force.

## Usage

```
fft network infer [flags]
```

## Flags

```
      --force[=true|false]   Replace the file if it exists
      --out string           Write the config here; '-' prints it on stdout (default "fft-network.yaml")
      --snapshot string      Read this snapshot instead of the project's file in the cache directory
```

## See also

- [fft network](./fft_network.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
