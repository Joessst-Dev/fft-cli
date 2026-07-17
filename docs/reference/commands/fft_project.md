---
title: fft project
---

# fft project

Manage projects

Manage the fulfillmenttools projects fft can talk to.

A project is one tenant plus the Firebase project that authenticates against it.
Its non-secret settings live in ~/.config/fft/config.yaml; its password and
tokens live in your OS keychain, one entry per secret.

The base URL is stored, never derived from the project id — the official docs
disagree with themselves about the host format, so fft refuses to guess.

## Usage

```
fft project
```

## Subcommands

- [fft project add](./fft_project_add.md) — Configure a project
- [fft project current](./fft_project_current.md) — Show the active project
- [fft project list](./fft_project_list.md) — List the configured projects
- [fft project read-only](./fft_project_read-only.md) — Refuse every request that would change a project
- [fft project remove](./fft_project_remove.md) — Remove a project and its stored credentials
- [fft project use](./fft_project_use.md) — Set the active project

## See also

- [fft](./fft.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
