---
title: fft template save
---

# fft template save

Save a request body as a template

Save a request body so you can send it again.

The body comes from --file, --data or --from:

    fft template save rush --file body.json
    fft order get ORDER-1 -o json | fft template save rush --file -
    fft template save rush --from createOrder        seeds from the spec's example

--param declares a parameter: a short name for a path inside the body, so that
'--set email=…' works instead of '--set order.consumer.email=…'. Give it a default
with a second '=', and mark it required with --require:

    --param qty=order.items.0.quantity=1
    --require email=order.consumer.email

A top-level "version" is dropped on the way in. A version is only true of the entity
at the moment it was read, and replaying a stale one is a guaranteed 409 — a saved
body that still carries one is a trap for whoever finds it next. Put one back at
render time with --set version=N if you really want that.

--local writes ./.fft/templates instead of your own directory. That file is meant to
be committed, so read it first: a body captured from real work carries real facility
ids, order ids and consumer emails, and git history is not something you can quietly
edit later.

## Usage

```
fft template save <name> [flags]
```

## Flags

```
      --data string           Request body: inline JSON, @file, or '-' for stdin
      --description string    What this template is for
      --file string           JSON file holding the request body ('-' for stdin)
      --force                 Replace a template of the same name
      --from string           Seed the body from this operation's example
      --local                 Write to ./.fft/templates, the directory this repository commits
      --param stringArray     Declare a parameter: --param name=path[=default] (repeatable)
      --require stringArray   Declare a required parameter: --require name=path (repeatable)
```

## See also

- [fft template](./fft_template.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
