---
title: fft sourcing get
---

# fft sourcing get

Read a sourcing run back

Read a sourcing run back.

&lt;id> is the id 'fft sourcing simulate' reported: the run is kept, so the same
answer can be read again without paying for the routing a second time.

  fft sourcing get 284f32cc-b106-487d-b633-f90d93d8c251
  fft sourcing get 284f32cc-b106-487d-b633-f90d93d8c251 -o json | jq '.result.options[0].transfers'

An option does not stay true forever — each carries a validUntil, and the table
shows it. A run whose options have expired is a record of what the router *would*
have done, not of what it would do now.

## Usage

```
fft sourcing get <id>
```

## See also

- [fft sourcing](./fft_sourcing.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
