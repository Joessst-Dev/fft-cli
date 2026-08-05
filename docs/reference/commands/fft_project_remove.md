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
keychain behind, and this is where that gets cleared up.

fft only claims to have removed the credentials when it actually did. If the
other store refuses — a locked keychain, an unreadable file — it names it and
what it holds, because clearing that is yours to do and not fft's. If it cannot
be opened at all, fft says only that: on a machine that has never had a keychain
there was nothing in it to begin with, and fft cannot tell that apart from a
keychain that is merely out of reach this run.

Removing the active project leaves no project active; run 'fft project use' to
pick another.

## Usage

```
fft project remove <name>
```

## See also

- [fft project](./fft_project.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
