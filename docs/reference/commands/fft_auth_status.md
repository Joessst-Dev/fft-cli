---
title: fft auth status
editLink: false
---

# fft auth status

Show the stored credentials' state, offline

Show what fft has stored to sign in as the current project, without
signing in.

Nothing is sent anywhere and no token is minted: this reads the credential store
and reports what it finds — which store it is, whether a password, a refresh
token and an id token are stored, and when that id token expires. It is the
cheap question to ask before a command that would have to sign in first.

TOKEN is one of:
  valid      the cached id token will be used as it is
  expiring   it has less than five minutes left
  expired    its expiry has passed
  unknown    there is an id token but nothing readable says when it expires
  none       no id token is cached

With a stored password (SIGN-IN "password"), the next command signs in again
when the token is expiring, expired, unknown or none, and fails with exit 4 if
it cannot.

A project running from the environment (FFT_BASE_URL and friends) reports the
"env" store. With FFT_ID_TOKEN and no password, SIGN-IN is "id token": that token
is used as it is and nothing renews it, so once it has expired every command
fails with exit 4. Its expiry is FFT_ID_TOKEN_EXPIRES_AT, or else the token's
own exp claim.

No credential is ever printed, in any output format.

## Usage

```
fft auth status
```

## See also

- [fft auth](./fft_auth.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
