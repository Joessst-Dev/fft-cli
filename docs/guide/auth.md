---
title: Authentication
---

# Authentication, honestly

fulfillmenttools does not authenticate you. **Google Identity Platform (Firebase)
does**, and fulfillmenttools accepts the resulting token. The swagger has no login
endpoint at all, which is why this is worth spelling out:

1. `fft` signs in against `identitytoolkit.googleapis.com` with your username and
   password, and receives an ID token and a refresh token.
2. It sends `Authorization: Bearer <idToken>` to `https://<tenant>.api.fulfillmenttools.com`.
3. When the token nears expiry it refreshes against `securetoken.googleapis.com`,
   transparently. You will not notice.

**The API key confers no authorization.** It is the Firebase *Web API key*: it identifies
the Firebase project and grants nothing on its own. It is sent only as `?key=` on those two
Google URLs and is **never sent to fulfillmenttools** — the token source owns a separate
HTTP client with a hardcoded allowlist of the two Google hosts, so the key is structurally
incapable of reaching your tenant. It is nonetheless treated as sensitive and kept in the
keychain, not the config file.

**Your username is not your email.** fulfillmenttools derives a synthetic one:
`{username}@ocff-{projectId}-{env}.com`. `fft` builds it for you; `project add` asks for
the parts.

Secrets (API key, password, refresh token, ID token) live in the **OS keychain** — Keychain
on macOS, Credential Manager on Windows, Secret Service on Linux. Each gets its own entry.
Non-secret project data lives in `~/.config/fft/config.yaml`, mode `0600`. An older config
that still holds the API key in cleartext is migrated into the keychain on the next run.

On a Linux box with no Secret Service (a headless server, a bare container), pass
`--no-keyring` or set `FFT_NO_KEYRING=1` to fall back to a `0600` file. `fft` **warns** (it
does not refuse) if that fallback file, or its directory, is readable by other users — a
restored backup, a shared `XDG_STATE_HOME`, or a stray `chmod -R` can loosen it after the
fact, and the file holds your refresh token in cleartext.

## WSL and headless Linux: there is no Secret Service

A WSL distribution is Linux, so `fft` builds with the D-Bus Secret Service backend — and a
stock distribution has nothing on the other end of it: no session bus, no `gnome-keyring`.
The keychain is not merely locked there, it does not exist.

`fft` says so. An unreachable keychain is a distinct condition, not a stray D-Bus error:
it exits **3** and tells you the three ways out.

```
$ fft project add prd --base-url https://acme.api.fulfillmenttools.com …
Error: store the password for "prd": no OS keychain is available on this machine: …

WSL ships no Secret Service, so there is no keychain for fft to use.

Pass --no-keyring (or set FFT_NO_KEYRING=1) to store credentials in a 0600 file
instead, or add "noKeyring: true" under "settings:" in ~/.config/fft/config.yaml to
make that permanent — that file holds your password and refresh token in cleartext.
To keep a real keychain, install gnome-keyring and run fft inside a session D-Bus.
```

On a terminal, `fft project add` asks instead of just failing, and writes
`settings.noKeyring: true` when you say yes — so you are asked exactly **once**, per
machine, and never again:

```
No OS keychain is available. Store credentials in a 0600 file, in cleartext, instead? [y/N]:
```

Three things this deliberately does **not** do. It never falls back on its own — the file
holds your password in cleartext, and landing there by accident is the thing the explicit
opt-in exists to prevent. `--yes` does not answer that question either: `-y` is consent to
the questions a command was always going to ask, not to downgrading where your credentials
live. And a keychain that is merely **locked**, or a prompt you denied, is not this — that
is a keychain that exists, and the answer there is to unlock it, not to write a file.

One caveat if you sync `config.yaml` between machines — it holds no secrets, so committing
it to a dotfiles repo is safe: `noKeyring` is a setting, not a fact about the machine. A
`true` that follows you to a laptop that *does* have a keychain keeps `fft` reading the
file store there. `--no-keyring=false` overrides it for one run.

If you would rather keep a real keychain under WSL, install `gnome-keyring` and give it a
session bus (`dbus-run-session -- fft …`, or a systemd user session via `[boot] systemd=true`
in `/etc/wsl.conf`). `fft` needs nothing special once `org.freedesktop.secrets` answers.

On macOS, a Keychain item `fft` stores is readable by **any process running as you**, with no
per-access prompt — that is how the `security` framework's default access control works, and
`fft` accepts it. The same-user threat is out of scope: a process running as you can already
read the config, the fallback file, and the token in flight. It is spelled out here so the
guarantee is not mistaken for a stronger one.

## On Windows, `--no-keyring` protects less than `0600` suggests

The default on Windows is the **Credential Manager**, and it is the right choice: the OS holds
each secret per-user, and `config.yaml` never contains a secret. None of what follows applies
to the default path.

`--no-keyring` is different. It writes `%USERPROFILE%\.local\state\fft\credentials.json`, and
that file holds your **password and refresh token in cleartext**, exactly as on Linux. What is
*not* the same is the protection around it:

- Windows has no POSIX mode bits. `fft` asks for mode `0600`, but Go's `os.Chmod` on Windows
  only toggles the read-only attribute and discards the rest. File security on Windows is an
  **ACL**, and `fft` sets no ACL of its own.
- The file is therefore protected by exactly one thing: **the ACL it inherits from its parent
  directory.** Under the default `%USERPROFILE%` that inheritance is sound — a stock Windows
  install grants your profile directory to you, `SYSTEM` and `Administrators`, and to no other
  standard user. There, the file is about as private as `0600` on Linux, where `root` can read
  it anyway.
- The weakness is that the protection is *inherited rather than asserted*. Point
  `XDG_STATE_HOME` (which `fft` honours on Windows too) at a shared directory, a second volume
  with default permissions, a network share, or a redirected/roaming profile, and the file
  inherits **that** ACL — which may let every user on the machine read it. On Linux, `0600`
  would still protect you in all of those places. On Windows, nothing does.

**So:** on Windows, prefer the Credential Manager. If you genuinely need `--no-keyring` — a
Windows CI container, say — leave `XDG_STATE_HOME` unset so the file stays inside your user
profile, and assume anyone with local Administrator can read your tenant password.

The specs that assert `0600` are **skipped** on Windows, with that reason printed in the CI
output, rather than deleted — so this gap stays visible instead of rotting quietly.
