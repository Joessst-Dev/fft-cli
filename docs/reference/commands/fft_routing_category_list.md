---
title: fft routing category list
---

# fft routing category list

List the routing categories

List the routing node config categories.

  fft routing category list
  fft routing category list --all -o json | jq -r '.[].id'

This endpoint pages by id, so --all fetches every category and stops at --max-items,
saying so on stderr if it had to. stdout carries the categories and nothing else.

## Usage

```
fft routing category list [flags]
```

## Flags

```
      --all             Page to the end and return every match, not just the first page
      --max-items int   With --all, stop after this many categories (default 10000)
      --size int        Categories per page, 1–250 (default 25)
      --total           Also count the matches, and report the total on stderr
```

## See also

- [fft routing category](./fft_routing_category.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
