---
title: fft stock get
---

# fft stock get

Show one stock

Show one stock, by its platform id.

A stock is addressed by the id the platform gave it — unlike a listing, which is
addressed by your own tenantArticleId. 'fft stock list' is where you find the id:

  fft stock list --tenant-article-id 4711 -o json | jq -r '.[].id'
  fft stock get 019c41f1-8f14-7000-9575-25a1b5c8b3a1

-o json prints the API's own JSON, in full. The table is a summary.

## Usage

```
fft stock get <stockId>
```

## See also

- [fft stock](./fft_stock.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
