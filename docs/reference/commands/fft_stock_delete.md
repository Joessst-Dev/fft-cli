---
title: fft stock delete
---

# fft stock delete

Delete one stock

Delete one stock.

The quantity is removed; the article's listing at that facility is untouched, and
so is its stock at other facilities and other locations.

  fft stock delete 019c41f1-8f14-7000-9575-25a1b5c8b3a1

fft asks first; -y/--yes answers for you. On a non-interactive terminal there is
nobody to ask, and fft refuses rather than assuming yes.

To delete many stocks at once — every stock at a location, or every stock of a
set of articles — use 'fft stock actions', which does it in one request.

## Usage

```
fft stock delete <stockId>
```

## See also

- [fft stock](./fft_stock.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
