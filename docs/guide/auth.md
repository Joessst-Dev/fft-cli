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
`--no-keyring` or set `FFT_NO_KEYRING=1` to fall back to a `0600` file.

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
