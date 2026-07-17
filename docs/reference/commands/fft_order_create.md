---
title: fft order create
---

# fft order create

Create an order

Create an order from a JSON file.

The body is an OrderForCreation: it needs an orderDate, a consumer, and at least
one orderLineItem with an article and a quantity. Most orders carry much more — a
delivery address, delivery preferences, a source, tags — but those are the three
the API insists on.

--example prints a minimal body you can edit and send straight back:

  fft order create --example > order.json
  $EDITOR order.json
  fft order create --file order.json

--file - reads the body from stdin.

A create is never retried. If the API answers 500 the order may still have been
created, and sending the request again would risk creating a second one; fft tells
you instead of guessing.

## Usage

```
fft order create [flags]
```

## Flags

```
      --example       Print a sample request body and exit
      --file string   JSON file holding the order ('-' for stdin)
```

## See also

- [fft order](./fft_order.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
