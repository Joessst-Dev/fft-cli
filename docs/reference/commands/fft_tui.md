---
title: fft tui
editLink: false
---

# fft tui

Browse and send requests in an interactive terminal UI

Open fft's interactive mode: a full-screen UI in the terminal.

Every request the UI sends is an fft command line, run through the same command
tree as in a shell: the read-only gate, the exit codes and the validation all
apply unchanged.

The UI is drawn on stderr and needs a terminal on both stdin and stderr. It
writes nothing to stdout.

## Usage

```
fft tui
```

## See also

- [fft](./fft.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
