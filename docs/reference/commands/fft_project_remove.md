---
title: fft project remove
---

# fft project remove

Remove a project and its stored credentials

Remove a project.

Its entry in the config file goes, and so does every one of its keychain entries
— the password, the refresh token, the cached id token and its expiry. Nothing is
left behind for a later project of the same name to inherit.

Removing the active project leaves no project active; run 'fft project use' to
pick another.

## Usage

```
fft project remove <name>
```

## See also

- [fft project](./fft_project.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
