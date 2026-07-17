---
title: Troubleshooting
---

# When something goes wrong

Branch on the exit code. Do not grep the message: the codes are fft's contract with scripts,
and the wording is not.

| code | what happened | what to do |
|------|---------------|------------|
| 0 | it worked | — |
| 1 | unclassified failure | read stderr; it says what broke |
| 2 | bad flags or arguments | **your** mistake, not the API's. Re-read `--help`. Do not retry |
| 3 | no active project, or the config is unusable | `fft project list`. Ask the user; do not invent a project |
| 4 | authentication failed | the credentials are wrong or expired. Tell the user. Do not re-prompt for a password |
| 5 | authenticated, but not permitted | the user's role lacks the permission. `fft api describe <op>` names it; `fft auth whoami` says what they have |
| 6 | not found | the id does not exist. Check it before assuming the endpoint is wrong |
| 7 | version conflict (409) | the object changed under you. **Re-read it, re-apply, re-send.** Never retry the same body |
| 8 | partial bulk write | some items landed. Read the per-item results; re-send only the `FAILED` ones, never the `UNKNOWN` ones |
| 9 | upstream unreachable or erroring | fft retried it **only if it was safe to repeat** — see below. Back off, and tell the user |
| 10 | read-only: fft refused a write | **nothing was sent.** Not an auth problem, and not routable around. Ask the user |
| 130 | interrupted | — |

## Exit 9 on a create: it may have landed anyway

fft retries only what is safe to send twice — GET, PUT, DELETE. A **POST or a PATCH is sent
once**, because a 500 on `POST /api/facilities` does not mean the facility was not created;
it means nobody told you whether it was.

So a create that exits 9 is not a create that failed. It is a create with an **unknown**
outcome, and re-running it is how you end up with two of something:

```sh
fft facility list --tenant-facility-id BER-01
```

Go and look before you send it again. (This is the same fact the bulk writers report as
`UNKNOWN` — see [recipes.md](./recipes.md).) Note also that the API's *searches* are POSTs, so
a failed list is not retried either — but a list is safe to simply run again.

## The three refusals you will be tempted to "fix" wrongly

**Exit 2 after a destructive command was refused.** Your shell is not a terminal, so fft
would not prompt, so it refused. Do not reach for `--yes` to make the error go away. `--yes`
is the *user's* consent, not yours: go and ask them, and let them decide.

**Exit 10, read-only.** Do not add `--read-only=false`. It is refused on purpose — the flag
can tighten and never loosen, and trying is itself a usage error. The project is protected
because somebody protected it. Ask.

**Exit 4, authentication.** Do not ask the user to type a password into the chat, and do not
re-run `fft project add` to "refresh" it. `fft auth refresh` is the command; if it fails, the
credentials themselves are the problem and the user has to fix them.

## Diagnosing

```sh
fft ping
fft auth whoami
fft project current
```

- `fft ping` needs no credentials. It tells "the tenant is down" (exit 9) apart from "my
  token is wrong" (exit 4).
- `fft auth whoami` prints the permissions the current credentials actually have. It is the
  answer to any exit 5.
- `--debug` on any command dumps the request and the response to stderr, secrets redacted.

## A 400 that says nothing useful

Usually a body missing a required field. Do not guess at it:

```sh
fft api describe addPickJob
fft api addPickJob --example
```

The sample body carries every required field, and `describe` lists every parameter.

## "there is no operation X"

Exit 2, with a suggestion. The operationId is wrong — find the right one:

```sh
fft api list --search pick
```
