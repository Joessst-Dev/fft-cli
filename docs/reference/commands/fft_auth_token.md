---
title: fft auth token
editLink: false
---

# fft auth token

Print the current id token

Print the current id token, signing in or refreshing if necessary.

This is for scripts that need to call the API with something other than fft:

  curl -H "Authorization: Bearer $(fft auth token --raw)" \
    https://acme.api.fulfillmenttools.com/api/facilities

The token is a bearer credential: anything holding it can act as you until it
expires. fft therefore refuses to print it to a terminal unless you ask for it
with --raw — a token in your scrollback is a token in every screenshot and every
pasted log from then on. Piping it, as above, needs no flag.

Without --refresh the token is whatever is cached, and fft only replaces it when
under five minutes remain — so it may be close to expiry. That is right for a
command run straight away, as above, but not for something that runs for a
while and cannot renew it: a CI step, a sandbox, a long crawl. For those, pass
--refresh to mint a new token first, with the full hour ahead of it, and print
that one. The remaining lifetime is reported on stderr.

  (umask 077; fft auth token --refresh > token-file)

--refresh fails like 'fft auth refresh' does: exit 4 when neither the refresh
token nor the stored password works, exit 2 for a fixed FFT_ID_TOKEN, which
cannot be renewed.

## Usage

```
fft auth token [flags]
```

## Flags

```
      --raw[=true|false]       Print the token even when stdout is a terminal
      --refresh[=true|false]   Mint a new token first, so it has its full lifetime
```

## See also

- [fft auth](./fft_auth.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
