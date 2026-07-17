---
title: fft auth token
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

## Usage

```
fft auth token [flags]
```

## Flags

```
      --raw   Print the token even when stdout is a terminal
```

## See also

- [fft auth](./fft_auth.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
