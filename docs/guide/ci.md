---
title: CI & headless use
---

# CI and headless use

Set these and `fft` runs entirely from the environment — it touches **neither the config
file nor the keychain**:

| Variable | |
|---|---|
| `FFT_BASE_URL` | `https://<tenant>.api.fulfillmenttools.com` |
| `FFT_FIREBASE_API_KEY` | the Firebase Web API key |
| `FFT_USERNAME` *or* `FFT_EMAIL` | the username, or the full synthetic email |
| `FFT_PASSWORD` | |
| `FFT_PROJECT_ID` | needed to derive the email from `FFT_USERNAME` |
| `FFT_ENV` | likewise |
| `FFT_READ_ONLY` | optional; refuse every request that would change the tenant — see [Read-only projects](./read-only.md) |
| `FFT_HISTORY` | optional; the request history is off in headless mode, and `on` records it — see [Request history](./configuration.md#request-history) |

`FFT_BASE_URL` is held to the same rule as `fft project add`: it must be `https`, or
`http` only to a loopback address (`localhost`/`127.0.0.1`). Plain `http` to any other
host — including a container service name like `http://emulator:8080` on a compose
network — is refused, so a misconfigured `FFT_BASE_URL` cannot send the bearer token in
the clear. Reach a sidecar over its published port on `localhost`, or over `https`.

```yaml
- run: fft facility list -o json | jq '.[].name'
  env:
    FFT_BASE_URL: ${{ secrets.FFT_BASE_URL }}
    FFT_FIREBASE_API_KEY: ${{ secrets.FFT_FIREBASE_API_KEY }}
    FFT_USERNAME: ${{ secrets.FFT_USERNAME }}
    FFT_PASSWORD: ${{ secrets.FFT_PASSWORD }}
    FFT_PROJECT_ID: ${{ vars.FFT_PROJECT_ID }}
    FFT_ENV: ${{ vars.FFT_ENV }}
```

## Using an id token instead of a password

If the job authenticated elsewhere, hand `fft` the token and nothing else:

```sh
FFT_BASE_URL=https://<tenant>.api.fulfillmenttools.com \
FFT_ID_TOKEN=… \
fft facility list
```

With `FFT_ID_TOKEN` set and no `FFT_PASSWORD`, only `FFT_BASE_URL` and the token are
required. The Firebase key and the email are never used: a static token cannot be
refreshed, so it is used as it is until it expires (`fft auth refresh` refuses). This is
the least-privilege way to give a CI job or sandbox access — a credential that expires
within the hour and cannot mint more. `fft auth status` shows an empty email unless you
set `FFT_EMAIL` (or `FFT_USERNAME`, `FFT_PROJECT_ID` and `FFT_ENV`, which build one).

The password path stays all-or-nothing: once `FFT_PASSWORD` is set, the key and an email
are required too, and a partial set is ignored rather than falling back to the config
file.

Every global flag has an environment variable too: `--output` is `FFT_OUTPUT`,
`--project` is `FFT_PROJECT`, and so on.
