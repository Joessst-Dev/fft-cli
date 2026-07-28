---
title: fft routing strategy actions
---

# fft routing strategy actions

Run one action against a routing strategy

Run one action against a routing strategy.

This is the escape hatch for the four structural actions that have no verb of their
own: 'activate' is the fifth, curated separately because it is the one you reach for
by far the most.

  fft routing strategy actions &lt;id> --example > action.json
  $EDITOR action.json
  fft routing strategy actions &lt;id> --file action.json

The five actions, all discriminated on "name":

  ACTIVATE                       make this strategy the live one (see 'activate')
  COPY                           create a new draft from this strategy
  REPLACE_GLOBAL_CONFIGURATION   swap the strategy's global configuration
  REPLACE_NODE                   replace one node in the tree, by id
  REPLACE_CONDITION              replace one condition in the tree, by id

REPLACE_NODE and REPLACE_CONDITION are the id-addressed way to edit one part of the
tree without the whole-strategy PUT that 'update' does. Most carry the strategy's
"version" for optimistic locking, which the --example body already has.

## Usage

```
fft routing strategy actions <id> --file <file> [flags]
```

## Flags

```
      --example       Print a sample request body and exit
      --file string   JSON file holding the action ('-' for stdin)
```

## See also

- [fft routing strategy](./fft_routing_strategy.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
