---
title: Templates
---

# Templates: saving a body and sending it again

A template is a saved request body plus the parameters that change between sends. It is a
JSON file — nothing about it is a database — and `fft template render` prints the finished
body on stdout, so it composes with every command that takes `--file`:

```sh
fft template render rush-order --set email=a@b.de | fft order create --file -
```

Rendering makes **no request**. It needs no project, no credentials and no network, and a
read-only project cannot refuse it: the command on the right of the pipe is still gated
exactly as it always was. That is the whole design — templates add a way to build a body,
not a new way to reach the tenant.

## Saving one

```sh
fft template save rush-order --file body.json --description "Rush order for the flagship"
fft order get ORDER-1 -o json | fft template save from-order --file -
fft template save facility --from addFacility
```

- `--from <operationId>` seeds the body from the spec's own example and records which
  operation it is for, which is the fastest way to start one from nothing.
- A top-level `version` is **dropped on the way in**, and fft says so. A version is only
  true of the entity at the moment it was read; replaying a saved one is a guaranteed 409.
  Put one back at render time with `--set version=N` if you really mean to.
- `--local` writes `./.fft/templates`, which is meant to be committed and shared. Without
  it templates go to `$XDG_DATA_HOME/fft/templates`, which is yours alone. A project
  template of the same name wins.

## Declaring parameters

`--param` gives a path a short name; `--require` makes it mandatory:

```sh
fft template save rush-order --file body.json --require email=order.consumer.email --param qty=order.orderLineItems.0.quantity=1
```

`fft template show rush-order` then prints what the template wants before you type a
`--set`, and rendering without a required parameter fails with exit 2 naming **all** of the
missing ones at once.

## Changing values

`--set` takes either a declared parameter or a path into the body, and the two are the same
namespace — a declared name can never contain a dot, so there is nothing to disambiguate:

```sh
fft template render rush-order --set email=a@b.de --set order.orderLineItems.0.quantity=3
fft template render rush-order --set email=a@b.de --set order.orderLineItems.1.quantity=1
```

- A missing object on the way is **created**. `--set order.address.city=Berlin` works
  against a body with no `address`.
- A numeric segment indexes an array, and one index past the end **appends** — which is how
  you build a list up one `--set` at a time. Further past the end is exit 2 naming the
  length, not a body padded with nulls the API will reject with an opaque 400.
- A path that would bury a scalar (`--set name.first=x` where `name` is a string) is exit 2.
  fft will not silently delete a field you still have.
- A value that parses as JSON **is** that JSON: `3` is a number, `true` a boolean, `null` a
  null, `{"a":1}` an object.

**Ids made only of digits need `--set-string`.** `--set tenantOrderId=12345` sends the
number `12345`; the API wants the string `"12345"`. This is the same trap as everywhere
else in fft — see [Ids are not numbers](./recipes.md) — arriving from the other direction:

```sh
fft template render rush-order --set-string order.tenantOrderId=12345
```

## Tenants do not share ids

A template records the project it was saved under. Rendering it while another project is
active still works — promoting a body from staging to production is a real thing people do
— but fft warns on stderr, because facility ids, consumer ids and order ids are exactly
what does not survive the move.

**Read a project template before you commit it.** A body captured from real work carries
real ids and real consumer emails, and git history is not something you can quietly edit
later.

## The commands

```sh
fft template list
fft template show rush-order
fft template render rush-order --set email=a@b.de
fft template save rush-order --file body.json
fft template remove rush-order
```

- `fft template list` prints one row per name — exactly the set `render` would resolve. A
  personal template hidden by a project one of the same name is reported on stderr instead.
- `fft template render` always prints JSON, even under `-o yaml`: the body is going into a
  command that reads JSON.
- Exit 6 is a template nobody saved; the error names the ones that exist.
