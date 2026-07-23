---
title: fft auth
---

# fft auth

Inspect and renew credentials

Inspect and renew the credentials fft signs in with.

fulfillmenttools has no login endpoint: authentication happens against Google
Identity Platform (Firebase), and only the resulting id token is ever sent to
your tenant. fft signs in with your stored password, caches the id token, and
refreshes it before it expires — so under normal use you never run these
commands at all.

The Firebase Web API key is kept in the keychain alongside your credentials and
never written to the config file. It grants nothing on its own, and fft will not
send it anywhere but Google.

## Usage

```
fft auth
```

## Subcommands

- [fft auth refresh](./fft_auth_refresh.md) — Mint a new id token now
- [fft auth token](./fft_auth_token.md) — Print the current id token
- [fft auth whoami](./fft_auth_whoami.md) — Show the authenticated user and their permissions

## See also

- [fft](./fft.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
