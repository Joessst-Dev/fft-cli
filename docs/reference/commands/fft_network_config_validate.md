---
title: fft network config validate
editLink: false
---

# fft network config validate

List every problem in fft-network.yaml

Check fft-network.yaml and list every problem in it.

  fft network config validate
  fft network config validate --config ops/fft-network.yaml -o json

It reports, each with its line:

  - what the format does not allow: an unknown field, a value out of range, an
    expectation named like a rule, a suppression without a reason or a date;
  - suppressions that have expired, which no longer suppress anything;
  - with a snapshot (the project's, or --snapshot), every pinned facility,
    selector and suppression that names nothing in the network.

stdout carries the problems and nothing else. Exit 0 means the file is fine; exit
3 means it has problems. It sends no request.

## Usage

```
fft network config validate [flags]
```

## Flags

```
      --config string     The config file to check (default "fft-network.yaml")
      --snapshot string   Check what the file names against this snapshot instead of the project's
```

## See also

- [fft network config](./fft_network_config.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
