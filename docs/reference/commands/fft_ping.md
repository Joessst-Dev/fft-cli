---
title: fft ping
---

# fft ping

Check that the tenant is reachable

Check that the tenant is reachable.

This calls GET /api/status, the one endpoint that answers without a token. fft
therefore sends none: ping tests the base URL and the network, and nothing else,
so a green ping with a red 'fft auth whoami' tells you precisely where the
problem is.

Run 'fft auth whoami' to check the credentials.

## Usage

```
fft ping
```

## See also

- [fft](./fft.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
