---
title: Interactive mode
---

# Interactive mode

`fft tui` opens a full-screen UI in the terminal. You can browse every operation the API
has, fill in a request, send it and read the response without leaving the terminal:

```sh
fft tui                       # on the active project
fft tui --project staging     # start on another one
fft tui --read-only           # refuse every write for the whole session
```

There is no separate client behind it. **Every action in the UI is an fft command line**,
run through the same command tree as in a shell. The read-only gate, validation,
confirmations and exit codes all apply unchanged. The status bar always shows the command
the focused action stands for, and `y` copies it, so anything you work out in the UI can go
straight into a script.

## It needs a terminal

The UI reads keys from stdin and **draws on stderr**. It needs a terminal on both, and
prints nothing to stdout, so `fft tui > out.txt` still shows the UI and leaves `out.txt`
empty. Without a terminal it does not start: it exits `2` and tells you to run the fft
command itself. In a pipe or a CI job, use the typed commands.

It uses the terminal's alternate screen, so your scrollback is untouched when you quit.
`--no-color`, `FFT_NO_COLOR` and `NO_COLOR` turn colour off, and no part of the UI depends
on colour to be understood.

Some of the global flags you give `fft tui` also apply to the requests it sends:

| | |
|---|---|
| `--project` | the project the session starts on |
| `--read-only` | refuses every write in the session; a request's own `--read-only=false` cannot loosen it |
| `--timeout` | the default timeout for every request; a request's own `--timeout` replaces it |
| `--no-keyring` | the credential store the whole session uses |

`--yes` and `--debug` do not apply to the requests. The UI asks its own questions, and a
trace of every request would bury each one's stderr.

In headless mode (`FFT_BASE_URL` and friends, see [CI & headless use](./ci.md)) the UI
runs against the environment's project. The Projects screen then only shows it: projects
cannot be added, switched, changed or removed until the `FFT_*` variables are unset.

## Around every screen

The tab bar lists seven screens. The status bar under them shows the project (marked
`RO` when writes are refused), the state of its token, how many commands are running and
how many are waiting for an answer, and the `$ fft …` command for the focused action.

Below the status bar, the key hint shows the keys that work right now. It takes as many
lines as it needs: first the keys of the screen, then the global keys, ending with `? all
keys`.

| Key | |
|---|---|
| `1`–`7`, `tab`, `shift+tab` | switch screen |
| `ctrl+p` | go to the Projects screen |
| `?` | open the key legend |
| `i` | open or close the panel of running commands |
| `y` | copy the focused action's `fft` command |
| `q`, `ctrl+c` | quit |

`?` opens the key legend in place of the screen. It lists every key of that screen, the
keys for each of its modes (typing in a field, searching, a form), the running commands
panel, questions, and the global keys. It also lists keys that do nothing right now, such
as `e` for an operation without a body, and says why they do nothing. `↑`/`↓` and
`pgup`/`pgdn` scroll it, and `?` or `esc` closes it. While it is open, no other key reaches
the screen underneath. `q` and `ctrl+c` still quit.

While a text field or a question has the keyboard, only `ctrl+c` still works: a `q` typed
into a field is a `q`, and so is a `?`. The hint then shows only that field's or
question's keys. Quitting while commands are still running asks first (`y` quits,
`n` stays), because a cancelled write may already have reached the tenant.

`y` copies through the terminal (OSC 52), so it also works over SSH. It refuses a command
holding a quote, a backslash or a control character, because shells do not all read those
the same way.

### Running commands

`i` opens a panel listing the session's commands, newest first. Each one is queued,
running, waiting for your answer, or finished with its exit code and duration. The panel
keeps the last 100.

| Key | |
|---|---|
| `↑`/`↓` | select |
| `enter` | open the command's response |
| `c` | cancel it |
| `esc` | close the panel |

A cancelled command that had already finished reports how it actually ended. A write
reported as interrupted may still have landed.

### Questions

A question about a write is answered with `y`. `n`, `esc` and `enter` all decline, so
pressing `enter` by accident sends nothing. For half a second after a question appears it
ignores keys, so a key you were already typing cannot answer it.

Taking a protection away, or doing something that cannot be undone, asks you to type a
word back and press `enter` (`esc` cancels). Examples are removing a project, allowing
writes to it again, or a `delete`. Every question about sending a body shows the start of
that body, and names the template it came from, if any.

## Projects (1)

The projects in your config file, with the active one marked `*`. On startup the UI checks
the current project's credentials and, if it signs in with a password and has no valid
token, signs in.

| Key | |
|---|---|
| `↑`/`↓` | select |
| `enter`, `u` | use this project (`fft project use`) |
| `r` | make it read-only, or allow writes again (`fft project read-only`) |
| `d` | remove it and its stored credentials (`fft project remove`) |
| `R` | sign in again now (`fft auth refresh`) |
| `a` | add a project |
| `ctrl+r` | read the list again |

Making a project read-only asks `y`/`n`. Allowing writes again, and removing a project,
want its name typed back.

`a` opens a form for `fft project add`. `tab`/`↓`/`enter` and `shift+tab`/`↑` move
between fields, `space` switches a toggle, `ctrl+s` adds the project (so does `enter` on
the last field) and `esc` cancels. The API key
and the password go to the command on stdin (`--api-key-stdin --password-stdin`), never on
its command line, so the command the UI shows holds neither.

## Operations (2)

Every operation of the API, grouped by the tag it is documented under. Each row shows the
tag, the operation id, its summary and the `fft` command that sends it. A `W` marks a write,
and a 🔒 next to it means the current project or session refuses that write. `★3` means the
current project has sent it three times (see [History](#history-6)).

| Key | |
|---|---|
| `↑`/`↓` | select |
| `←`/`→`, `pgup`/`pgdn` | previous or next page |
| `g`/`G`, `home`/`end` | first or last operation |
| `/` | search by operation id, summary or command; `enter` keeps the search, `esc` drops it |
| `esc` | clear the search you kept |
| `enter` | open the operation's request form |
| `D` | describe it: method and path, read or write, permissions, body, arguments and flags |

In the description, `enter` opens the request form and `esc` goes back to the list.

An operation that a curated command sends opens that command's form, so you get its typed
flags and table. A write that several commands share, such as the order actions, opens the
form for `fft api <operationId>` instead, because each of those commands sends only one use
of the operation. So does an operation a component claims. The description names the
other curated commands that send an operation, and a search finds it by them too.

Operations your roles do not seem to allow are shown dimmed. See [Roles](#roles-7).

## Request (3)

A form built from the command's arguments and flags. Required ones are marked, and an
empty field shows what the command does without it.

| Key | |
|---|---|
| `↑`/`↓` | select a field |
| `enter` | edit the field, or cycle an on/off flag like `space` |
| `space` | cycle an on/off flag through on, off and unset |
| `x` | clear the field |
| `e` | edit the body in your editor |
| `t` | save the body as a template |
| `s`, `ctrl+s` | send |
| `esc` | back to Operations |

While you type in a field, `enter` finishes, `tab`/`shift+tab` (or `↓`/`↑`) move to the
next or previous field, `esc` undoes the change and `ctrl+s` sends. A list flag takes comma-separated values.

`e` opens the body in `$VISUAL`, else `$EDITOR`, else `vi` (`notepad` on Windows). The
first time, it starts from the command's own `--example`, or else the sample body from the
API's schema. The file lives in a private temporary directory, which is removed when the
editor closes or the UI quits. The body goes to the command on stdin as `--file -`.

Before anything runs, the form checks for missing required values, numbers that are not
numbers and a body that is not JSON. The command checks them again.

Sending a **write** asks first, naming the project, the method and path, and the command it
will run. The project is fixed when you press `s`, so switching projects while the question
is open does not change where the request goes. A command that asks its own question, such
as `fft facility delete`, is sent without a question from the UI. Its own question then
appears once it has looked up what it is about to change, and a `delete`, `purge`,
`cancel` or `run` wants that word typed back.

`t` saves the body under a name you choose as a user template (`fft template save
--operation <id> --file -`). Only the body is saved, not the form's arguments and flags.

## Response (4)

Opens as soon as a request is sent. The header shows the exit code and what it means, the
HTTP status, how long the request took and the project it went to, followed by the command
that ran.

| Key | |
|---|---|
| `←`/`→` | switch between JSON, Table and Stderr |
| `↑`/`↓` (`j`/`k`), `h`/`l` | scroll |
| `pgup`/`pgdn` (`b`/`f`), `u`/`d` | scroll a page, or half a page |
| `r` | send the same request again |
| `s` | save the response body to a file |
| `c` | cancel, while it runs |
| `esc` | back to Request |

**JSON** is the API's document, indented. **Table** is only there for a curated command
that prints a table in a shell, and it is that same table. **Stderr** is everything else
the command said: notices, warnings and the error.

`r` sends the request to the project it went to the first time, and asks again if it is a
write. `s` asks for a path (it suggests `response-<n>.json`), writes the file with mode
`0600` and asks before it replaces an existing file. It does not create directories or
write through a link.

A response is kept up to 8 MiB, and the session keeps the newest 64 MiB of output across
all its commands. A response that was cut short says so, and cannot be saved from here:
run the command in a shell and redirect its output instead.

## Templates (5)

Your saved [templates](./templates.md), user and project ones, read with `fft template
list` when you first open the screen.

| Key | |
|---|---|
| `↑`/`↓` | select |
| `enter` | open it: its parameters, its body and the file it was read from |
| `p` | fill in its parameters |
| `R` | render it (`fft template render`) |
| `S` | render it and send the body with the operation's command |
| `x` | remove it (`fft template remove`, which asks its own question) |
| `ctrl+r` | read the list again |
| `esc` | back to the list, from an open template |

In the parameter form, `enter` edits a value, `x` clears it, `R` and `S` work as above and
`esc` closes the form. While you type, `enter` finishes, `tab` moves on and `esc` undoes.
The values you enter are kept for the rest of the session.

`S` does not send the body directly. It opens the Request form holding the body and sends
it from there, so a write is still asked about. The form waits if the command still needs
an argument. Any warning from the render, such as a template saved under another project,
is shown first. If the template's file changed after you opened it, nothing is sent and the
template is read again. If the Request form holds work you have not sent, `S` asks before
replacing it.

## History (6)

The requests sent to the current project, newest first, from the
[request history](#what-history-records). With no project selected, it lists every
project's requests.

| Key | |
|---|---|
| `↑`/`↓` | select |
| `enter` | fill in the Request form the way that request was sent |
| `t` | switch between recent requests and the operations used most |
| `c` | clear the history (`fft history clear`, which asks its own question) |
| `ctrl+r` | read it again |

`enter` sends nothing. It fills the form in and waits. The form is the one for the command
the request was sent through, even where the operation opens another: a request sent as
`fft facility list` reopens that command's form, not the one for `fft facility search`, and
one sent through `fft api` reopens the `fft api` form. Only a command this fft no longer has
gives an empty form. History keeps no bodies and no redacted values, so those fields are
left empty and the form lists what it could not fill in. It asks before replacing a form
that holds work you have not sent.

The most used operations are counted per project, which is also where the stars on the
Operations screen come from. When requests are not being recorded, the screen says why.

## Roles (7)

Who you are signed in as on the current project, your roles and their permissions, and the
operations that none of your roles allow (`fft auth whoami`).

| Key | |
|---|---|
| `↑`/`↓` | scroll |
| `r` | read them again |

The roles are read the first time you open Operations, Request or Roles, and again after
you switch projects. If the read fails, nothing is dimmed, and it is tried again once the
credentials work.

### Dimmed operations are only a hint

An operation is dimmed when none of your roles holds any of the permissions the API
documents for it. **A dimmed operation can still be sent.** A role can be limited to
certain facilities, which fft cannot see, so an operation you seem to lack may work for the
facility you are sending it for. Only the tenant can decide.

The UI therefore never blocks a dimmed operation. The Request form warns that you appear to
lack the permission. The question before sending says the tenant may refuse the request
with `403`, and that your roles may still allow it for some facilities. The same note
appears when you send the request again from Response and in a command's own question. An
operation whose permission the API does not document is never dimmed.

## Read-only projects

The UI goes through the same [read-only gate](./read-only.md) as the shell. On a read-only
project, or in a session started with `--read-only` or under `FFT_READ_ONLY`, every write
on the Operations screen shows a 🔒 and the status bar shows `RO`. The Request form and
the question before sending both say that fft will refuse the write. If you send it anyway,
the command exits `10` and **nothing is sent**: no token is minted and no request is made.
A command that asks its own question is refused before it gets to ask.

## What history records

Every command that sent an API operation is recorded, whether it was typed in a shell or
sent from the UI. That is what `fft history` and the History screen show. An entry holds
**metadata only**: the time, the project name, the operation, the command with its flags,
the HTTP status, the exit code and the duration.

- **No request or response body** is ever stored.
- **Redacted** as `<redacted>`: inline `--data` bodies (`--data -` and `--data @file` are
  kept, since they only say where the body came from), header values, template values
  (`--set`), the values of `--param` and `--require` pairs, any flag or pair whose name
  looks like a credential, and any value that is a JSON document.
- **Not recorded**: requests the UI makes on its own, such as reading your roles or signing
  in at startup. They would otherwise count as uses of those operations. Pressing `r` on
  the Roles screen is your own request, so it is recorded.

The file is `$XDG_STATE_HOME/fft/history.jsonl` (by default
`~/.local/state/fft/history.jsonl`), mode `0600`. It never leaves the machine.

Recording is switched off by `settings.noHistory: true` in the config file, or by
`FFT_HISTORY=off`. It is also off in headless mode unless `FFT_HISTORY=on`. The details are
in [Setting up a project](./configuration.md#request-history). With history off, the History
screen says why and the Operations screen shows no stars. Everything else works the same.
