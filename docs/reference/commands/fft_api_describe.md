---
title: fft api describe
---

# fft api describe

Show an operation's parameters, permissions and sample body

Describe an API operation: what it does, what it needs, and what to send it.

Everything comes from the spec, so this works offline and needs no project.

  fft api describe getPickJob
  fft api describe addPickJob -o json | jq -r .sampleBody

The EXAMPLE BODY is synthesized from the request schema — the spec ships 1,556
field-level examples and not one request-body example, so there was nothing to
copy. It is a body you can send: every required field is there, and the values
come from the spec's own examples wherever it has one.

To get just that body, use --example on the command itself:

  fft api addPickJob --example > job.json

## Usage

```
fft api describe <operationId>
```

## See also

- [fft api](./fft_api.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
