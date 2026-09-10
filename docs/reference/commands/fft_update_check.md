---
title: fft update check
editLink: false
---

# fft update check

Ask GitHub for the latest release now

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

The upgrade command it names is the one for how this fft was installed —
Homebrew, Scoop, WinGet, go install or the container image — worked out from
where the binary sits. When that cannot be told, it points at the install guide
instead. Set FFT_INSTALL_METHOD to homebrew, scoop, winget, go or docker to
override the guess.

The release endpoint is unauthenticated, and GitHub allows 60 unauthenticated
requests an hour per IP address — a budget shared with every other tool on the
machine. A check GitHub could not answer, that one included, exits 9.

## Usage

```
fft update check
```

## See also

- [fft update](./fft_update.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
