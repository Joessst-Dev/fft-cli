---
title: fft auth whoami
---

# fft auth whoami

Show the authenticated user and their permissions

Show who fft is authenticated as, and what that account may do.

This calls GET /api/users/me/effectivepermissions with the current id token, so
it is also the shortest proof that authentication works end to end: it signs in
if it has to, and it fails with exit code 4 if it cannot.

A 403 from another command usually means a role is missing here.

## Usage

```
fft auth whoami
```

## See also

- [fft auth](./fft_auth.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
