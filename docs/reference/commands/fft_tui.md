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

The first screen manages projects: switch, protect, remove and add them, and
refresh the current token. The Operations screen lists every operation of the
API by tag; press / to search it, D to describe one and enter to fill in its
request. The Request screen builds the command from its flags, opens the body in
$VISUAL (then $EDITOR) and asks before it sends a write. A command that asks its
own question, such as facility delete, asks it in the UI once it has looked up
what it is about to change; one that cannot be undone wants its verb typed back.
t saves the body as a template for the operation. The Response screen shows the exit code,
the HTTP status, the JSON, a table where the command prints one, and stderr; r
sends the request again and s saves the body to a file.

The Templates screen lists the saved templates. p fills in a template's
parameters, R renders it, and S renders it and hands the body to the operation's
command: anything the render warns about, such as a template saved under another
project, is shown first, and a write is still asked about before it goes. x
removes a template.

Press ? for every key, i for the commands that are running, and y to copy the fft
command the focused action stands for. Secrets you type and request bodies are
passed to that command on stdin, never on its command line.

The UI is drawn on stderr and needs a terminal on both stdin and stderr. It
writes nothing to stdout.

## Usage

```
fft tui
```

## See also

- [fft](./fft.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
