---
title: fft order update
---

# fft order update

Update an order (PATCH)

Update an order from a JSON file.

This is a PATCH with OrderForUpdate: the fields you include are changed and the
rest are left alone — with one trap. orderLineItems is a *full replacement*, not a
merge: if you send it, only the lines you include remain, and any you omit are
deleted. To change one line, send them all.

The API has no If-Match header — optimistic locking travels in the body as a
"version" field — so fft reads the order first to learn its current version, sends
that version back, and retries once if someone wrote in between. Your file does
not need a version; fft supplies it.

  fft order get 8f14e45f-ceea-467a-9575-25a1b5c8b3a1 -o json > order.json
  $EDITOR order.json
  fft order update 8f14e45f-ceea-467a-9575-25a1b5c8b3a1 --file order.json

--if-version skips the read: fft sends the version you name and the API answers
409 if it is stale. That is one request instead of two, and a clean failure
instead of a silent overwrite. (It is --if-version and never --version: cobra owns
--version on the root command.)

## Usage

```
fft order update <id> --file <file> [flags]
```

## Flags

```
      --file string      JSON file holding the order changes ('-' for stdin)
      --if-version int   Send this version instead of reading the current one (fails with 409 if it is stale)
```

## See also

- [fft order](./fft_order.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
