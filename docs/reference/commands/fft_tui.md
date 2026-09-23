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
project, is shown first, and a write is still asked about before it goes. Every
question about sending a body shows its start, and for a body from a template names
the template, its scope and, for a project template, its file. What is rendered is
the template on screen: if its file changed after it was opened, nothing is sent and
the template is read again. S asks before it replaces a Request form holding work
you have not sent. x removes a template.

The History screen lists the requests sent to the current project, newest first;
t switches to the operations used most, and enter fills in the Request form the way
a request was sent. Values history does not keep, such as header values and
request bodies, are left empty. c clears the history, after asking. When requests
are not being recorded, the screen says why. The Operations screen stars each
operation with how often the current project sent it.

The Roles screen shows your roles on the current project, their permissions, and
the operations none of them permits. The Operations screen greys those, and the
question before a request warns that you appear to lack the permission. It is only
a hint: a role can be limited to some facilities, so a greyed operation can still
be sent, and the tenant decides.

The Components screen lists the components fft can see: what each one is, whether
it is installed, and who ships it. enter shows everything its manifest says, a
installs one — by name, by owner/repo[@version], or from a directory — u upgrades
the selected one and d removes it. Those three ask their own question, in their own
words, before anything is unpacked or deleted. A component installed here is listed,
and its commands can be run, without restarting the UI.

The emulator is on the Projects screen: it lists as a row beside your projects,
because it is a tenant to work against rather than a seventh thing to send requests
with. enter on that row starts it if it is not running and sends every request in the
session to it once it is listening; enter on a configured project goes back. It is
never written to the config file, so r and d refuse it. e opens the pane behind the
row: s runs the emulator and s again stops it, its output fills the pane as it is
written, and c copies the FFT_* recipe that points another shell at it. Stopping it
leaves the session pointed at it and says so, rather than moving the session for you.

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
