---
title: fft order get
---

# fft order get

Show one order

Show one order.

&lt;id> is the order's platform id, the one 'fft order list' and 'fft order search'
print. Unlike a facility, an order has no tenantFacilityId-style shorthand.

  fft order get 8f14e45f-ceea-467a-9575-25a1b5c8b3a1
  fft order get 8f14e45f-ceea-467a-9575-25a1b5c8b3a1 -o json | jq .status

-o json prints the API's own JSON, in full. The table is a summary.

## Usage

```
fft order get <id>
```

## See also

- [fft order](./fft_order.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
