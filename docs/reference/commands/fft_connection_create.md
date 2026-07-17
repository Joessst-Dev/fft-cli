---
title: fft connection create
---

# fft connection create

Create a connection

Create a connection out of a facility.

The body must carry a "type" — SUPPLIER, MANAGED_FACILITY or CUSTOMER — and a
"target" that agrees with it. The type is the discriminator the API matches the
rest of the body against, and a SUPPLIER or MANAGED_FACILITY target must name
the facility the edge goes to. fft checks all of that before it sends anything,
because the API's answer to a body it dislikes is a 400 that names no field.

--example prints a body you can edit and send straight back, and --type chooses
which of the three it prints:

  fft connection create --facility BER-01 --example --type MANAGED_FACILITY > c.json
  $EDITOR c.json
  fft connection create --facility BER-01 --file c.json

--file - reads the body from stdin.

A create is never retried. If the API answers 500 the connection may still have
been created, and sending the request again would risk a second edge between the
same two facilities.

## Usage

```
fft connection create --facility <id> --file <file> [flags]
```

## Flags

```
      --example           Print a sample request body and exit
      --facility string   The facility the connection leaves, by tenantFacilityId or platform UUID (required)
      --file string       JSON file holding the connection ('-' for stdin)
      --type string       With --example, the kind of connection to print: SUPPLIER, MANAGED_FACILITY, CUSTOMER (default "SUPPLIER")
```

## See also

- [fft connection](./fft_connection.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
