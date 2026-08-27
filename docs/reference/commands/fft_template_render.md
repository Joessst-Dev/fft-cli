---
title: fft template render
---

# fft template render

Print a saved body with its parameters filled in

Print a saved body with its parameters filled in.

The body goes to stdout and everything else — warnings, the picker, a project
mismatch — goes to stderr, so this composes:

    fft template render rush-order --set email=a@b.de | fft order create --file -

--set takes either a parameter the template declares or a path into the body:

    --set email=a@b.de                      a declared parameter
    --set order.consumer.email=a@b.de       the same place, spelled out
    --set order.items.0.quantity=3          an array element
    --set order.items.1.quantity=1          one past the end appends

A value that parses as JSON is that JSON, so 3 is a number, true is a boolean and
{"a":1} is an object. Anything else is a string. --set-string skips that reading
entirely, which is what an id made only of digits needs: --set-string id=12345
sends "12345" where --set id=12345 would send 12345.

Rendering makes no request. It needs no project, no credentials and no network,
and it always prints JSON — even under -o yaml — because the body is going into a
command that reads JSON.

Called with no name on a terminal, it asks which template to render.

## Usage

```
fft template render [name] [flags]
```

## Flags

```
      --set stringArray          Set a parameter or a path: --set email=a@b.de (repeatable)
      --set-string stringArray   Set a value as a string, whatever it looks like: --set-string id=12345 (repeatable)
```

## See also

- [fft template](./fft_template.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
