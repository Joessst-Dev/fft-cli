---
title: fft sourcing simulate
---

# fft sourcing simulate

Ask the router how an order would be fulfilled

Ask the router how it would fulfil an order.

This changes nothing. It creates no order, reserves no stock, and books no
carrier — it runs the routing engine against a hypothetical order and hands back
the options. It is a POST because the order does not fit in a query string, not
because it writes, and fft knows the difference: it works on a read-only project.

The body is an order: who it goes to, and what is in it.

  fft sourcing simulate --example > order.json
  $EDITOR order.json
  fft sourcing simulate --file order.json --results 3

--file - reads the body from stdin.

The API returns *one* option unless it is asked for more, which is rarely what
somebody asking to see their options wants — so --results defaults to 3. The
answer is kept: 'fft sourcing get &lt;id>' reads the same run back later, and the
id is on stderr.

An empty answer means the order cannot be routed at all. It is not an error and
it is not "no matches" — it is the router saying there is no way to fulfil this.

## Usage

```
fft sourcing simulate --file <file> [flags]
```

## Flags

```
      --example              Print a sample order and exit
      --file string          JSON file holding the order ('-' for stdin)
      --investment float     How hard the router should look, above 0 and up to 1. Higher is better and slower
      --listing-attributes   Include each listing's custom attributes in the answer (for debugging)
      --results int          How many alternative options to ask for, 1–20 (default 3)
```

## See also

- [fft sourcing](./fft_sourcing.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
