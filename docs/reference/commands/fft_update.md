---
title: fft update
---

# fft update

Check whether a newer fft release is available

Check whether a newer fft release is available.

fft looks for a new release at most once a day, in the background, and mentions
it on stderr when there is one. The check can never delay or fail a command: it
is given 1.5 seconds, and whatever has not arrived by the time the command
finishes is dropped.

It is skipped entirely when the output is being piped or redirected, when -o json
or -o yaml is in effect, on a build that did not come from a release tag, when
FFT_NO_UPDATE_CHECK is set, and when settings.updateCheck is false in
~/.config/fft/config.yaml.

This command asks now, regardless of all of that, and ignores the once-a-day
cache.

## Usage

```
fft update
```

## Subcommands

- [fft update check](./fft_update_check.md) — Ask GitHub for the latest release now

## See also

- [fft](./fft.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
