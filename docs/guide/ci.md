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
| `FFT_READ_ONLY` | optional; refuse every request that would change the tenant — see [Read-only projects](https://github.com/Joessst-Dev/fft-cli/blob/main/README.md#read-only-projects) |

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

Every global flag has an environment variable too: `--output` is `FFT_OUTPUT`,
`--project` is `FFT_PROJECT`, and so on.
