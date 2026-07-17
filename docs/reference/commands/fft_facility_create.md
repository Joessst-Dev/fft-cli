---
title: fft facility create
---

# fft facility create

Create a facility

Create a facility from a JSON file.

The body must carry a "type": MANAGED_FACILITY or SUPPLIER. It is the
discriminator the API matches the rest of the body against, and it cannot be
changed afterwards — there is no action that turns a supplier into a managed
facility.

--example prints a body you can edit and send straight back:

  fft facility create --example > facility.json
  $EDITOR facility.json
  fft facility create --file facility.json

--file - reads the body from stdin.

A create is never retried. If the API answers 500 the facility may still have
been created, and sending the request again would risk creating a second one;
fft tells you instead of guessing.

## Usage

```
fft facility create [flags]
```

## Flags

```
      --example       Print a sample request body and exit
      --file string   JSON file holding the facility ('-' for stdin)
```

## See also

- [fft facility](./fft_facility.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
