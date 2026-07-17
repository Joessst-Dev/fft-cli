---
title: fft project read-only
---

# fft project read-only

Refuse every request that would change a project

Mark a project read-only, or allow writes to it again.

A read-only project refuses every request that would change the tenant — creates,
updates, patches, deletes and the action endpoints — before it signs in, so a
refused write costs no round trip and mints no token. It exits 10.

Reads keep working, and that includes the searches behind every list command: the
API runs those over POST, so being read-only is not the same as refusing to POST.

Marking a project read-only does not lock the config file. 'fft project use' and
'fft project remove' still work on it, because configuring fft is not changing
anything in the tenant.

Two other ways to say the same thing: FFT_READ_ONLY=1 protects whichever project
fft is about to use, which is what a CI job wants, and a --read-only flag on any
command protects that one invocation. Neither can be talked back down — the flag
and the variable can only tighten, never loosen.

## Usage

```
fft project read-only <name> [flags]
```

## Flags

```
      --off   Allow writes to the project again
```

## See also

- [fft project](./fft_project.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
