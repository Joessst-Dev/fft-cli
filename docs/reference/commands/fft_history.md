---
title: fft history
editLink: false
---

# fft history

Show the requests fft has sent

Show and clear the requests fft has sent.

fft keeps a local record of every command that addressed an API operation, typed
in a shell or sent from fft tui: when it ran, against which project, which
operation, the flags it was given, the HTTP status and the exit code. The requests
fft tui makes on its own, such as reading your roles, are not recorded. Request and
response bodies are never recorded, and so are no inline --data bodies, header
values, template values or credential-shaped flags: those are kept as &lt;redacted>.

The record is ~/.local/state/fft/history.jsonl (or under $XDG_STATE_HOME), mode
0600. It never leaves the machine.

Recording is off in headless mode (FFT_BASE_URL and friends) unless FFT_HISTORY=on
is set. FFT_HISTORY=off, or settings.noHistory: true in the config file, switches
it off everywhere.

## Usage

```
fft history
```

## Subcommands

- [fft history clear](./fft_history_clear.md) — Delete the request history
- [fft history list](./fft_history_list.md) — List the most recent requests
- [fft history top](./fft_history_top.md) — List the operations used most

## See also

- [fft](./fft.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
