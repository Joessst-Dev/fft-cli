---
title: fft routing strategy create
---

# fft routing strategy create

Create a routing strategy

Create a routing strategy.

The body needs only a name (nameLocalized); the API fills in a default root node and
global configuration, which you then shape with 'update'. A created strategy is a
draft — it is not live until you 'activate' it.

  fft routing strategy create --example > s.json
  $EDITOR s.json
  fft routing strategy create --file s.json

--file - reads the body from stdin.

A create is never retried: if the API answers 500 the strategy may still have been
created, and sending it again would leave you with two.

## Usage

```
fft routing strategy create --file <file> [flags]
```

## Flags

```
      --example       Print a sample request body and exit
      --file string   JSON file holding the strategy ('-' for stdin)
```

## See also

- [fft routing strategy](./fft_routing_strategy.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
