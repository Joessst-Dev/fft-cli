---
title: fft template remove
---

# fft template remove

Delete a saved template

Delete a saved template.

Your own templates are removed by default; --local removes the project's. The two
are kept apart on purpose here, unlike everywhere else: deleting a template the
whole team committed should take saying so.

Nothing here reaches the tenant — the entities the template addressed are untouched.

## Usage

```
fft template remove <name> [flags]
```

## Flags

```
      --local   Remove from ./.fft/templates, the directory this repository commits
```

## See also

- [fft template](./fft_template.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
