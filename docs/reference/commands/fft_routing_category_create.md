---
title: fft routing category create
---

# fft routing category create

Create a routing category

Create a routing node config category.

The body needs a name (nameLocalized) and a colour.

  fft routing category create --example > c.json
  $EDITOR c.json
  fft routing category create --file c.json

--file - reads the body from stdin. A create is never retried.

## Usage

```
fft routing category create --file <file> [flags]
```

## Flags

```
      --example       Print a sample request body and exit
      --file string   JSON file holding the category ('-' for stdin)
```

## See also

- [fft routing category](./fft_routing_category.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
