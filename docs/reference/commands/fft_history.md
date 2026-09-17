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
values, template values, credential-shaped flags, or flags and query parameters
that name a person's data (email, phone, address, street, postal code, first and
last name, username, search term): those are kept as &lt;redacted>.

Everything else is kept as typed, because reopening a request needs it: the ids
and other arguments, the names of --file bodies, and filter values such as a
status or a date. Anything personal passed that way is in the record too.

The record is ~/.local/state/fft/history.jsonl (or under $XDG_STATE_HOME), mode
0600 in a 0700 directory: fft takes group and other access away from either
before it records. It never leaves the machine.

Recording is off in headless mode (FFT_BASE_URL and friends, with no --project
naming a configured project) unless FFT_HISTORY=on is set. FFT_HISTORY=off, or settings.noHistory: true in the config file, switches
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
