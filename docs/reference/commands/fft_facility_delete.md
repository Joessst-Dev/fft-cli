---
title: fft facility delete
---

# fft facility delete

Delete a facility

Delete a facility.

This cannot be undone, so fft asks first. -y/--yes answers for you.

On a non-interactive terminal — a CI job, a pipe — there is nobody to ask, and
fft refuses rather than assuming yes. Pass --yes if you mean it.

## Usage

```
fft facility delete <id>
```

## See also

- [fft facility](./fft_facility.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
