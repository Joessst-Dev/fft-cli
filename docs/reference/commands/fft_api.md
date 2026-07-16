---
title: fft api
---

# fft api

Call any API operation by its operationId

Call any operation of the fulfillmenttools API by its operationId.

This is the escape hatch. fft curates the entities it knows well (facility,
listing, stock) and generates a command for every other operation, but when the
spec moves faster than fft does, this reaches whatever the spec says exists —
all 557 operations of it.

  fft api list --tag picking              # what is there?
  fft api describe getPickJob             # what does it take?
  fft api getPickJob --param pickJobId=abc123
  fft api queryPickJobs --query status=OPEN --query size=10
  fft api addPickJob --example > job.json && fft api addPickJob --file job.json

--param fills the path (/api/pickjobs/{pickJobId}); --query fills the query
string; --header adds a header. A missing required parameter is a usage error and
nothing is sent.

Array query parameters are encoded the way the spec says *that* parameter is
encoded — comma-joined for some, repeated for others. Pass several values either
as --query status=A,B or by repeating --query; both come out right.

The answer is printed as the API sent it. -o table has nothing to render an
arbitrary operation into, so it prints JSON too; -o yaml converts it.

## Usage

```
fft api <operationId> [flags]
```

## Flags

```
      --data string          Request body: inline JSON, @file, or '-' for stdin
      --example              Print a sample request body and exit
      --file string          JSON file holding the request body ('-' for stdin)
      --header stringArray   Request header, as name=value (repeatable)
      --param stringArray    Path parameter, as name=value (repeatable)
      --query stringArray    Query parameter, as name=value (repeatable; name=a,b for a list)
```

## Subcommands

- [fft api describe](./fft_api_describe.md) — Show an operation's parameters, permissions and sample body
- [fft api list](./fft_api_list.md) — List the API's operations

## See also

- [fft](./fft.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
