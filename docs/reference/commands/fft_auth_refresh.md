---
title: fft auth refresh
---

# fft auth refresh

Mint a new id token now

Mint a new id token now, without waiting for the current one to expire.

fft refreshes on its own — a token with less than five minutes left is replaced
before it is used — so this command exists for the times you want to see it
happen: after rotating a password, or when diagnosing a 401.

The refresh token is used if it still works, and your stored password if it does
not. If neither does, the command exits 4 and tells you to sign in again.

The token itself is never printed. Use 'fft auth token --raw' for that.

## Usage

```
fft auth refresh
```

## See also

- [fft auth](./fft_auth.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
