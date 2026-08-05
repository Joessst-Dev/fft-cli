---
title: fft project remove
---

# fft project remove

Remove a project and its stored credentials

Remove a project.

Its entry in the config file goes, and so does every one of its stored secrets —
the password, the refresh token, the cached id token and its expiry. Nothing is
left behind for a later project of the same name to inherit.

Both stores are emptied, not just the one in use: a machine that switched to the
0600 file (--no-keyring, settings.noKeyring) left whatever was already in its
keychain behind, and this is where that gets cleared up. If the keychain is there
but will not give them up — locked, or access denied — fft says so and stops
claiming to have removed them.

Removing the active project leaves no project active; run 'fft project use' to
pick another.

## Usage

```
fft project remove <name>
```

## See also

- [fft project](./fft_project.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
