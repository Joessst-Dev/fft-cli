---
title: fft project list
---

# fft project list

List the configured projects

List the configured projects.

The active project is marked with an asterisk. CREDENTIAL says where the
project's password or token is: "keyring" for the OS keychain, "file" for the
--no-keyring fallback, "env" for a project synthesized from FFT_* variables, and
"missing" for a project whose secrets have been removed from the keychain behind
fft's back — it is configured, but nothing can sign in as it.

In headless mode (FFT_BASE_URL and friends) this lists the environment's project
and does not read the config file.

## Usage

```
fft project list
```

## See also

- [fft project](./fft_project.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
