---
title: Setting up a project
---

# Setting up a project

A *project* is one tenant plus the credentials to reach it. You can configure as many as
you like and switch between them; commands act on the **active** one unless you pass
`--project`.

## Interactively

```sh
$ fft project add prod
Project name: prod
Base URL (e.g. https://acme.api.fulfillmenttools.com): https://ocff-acme-pre.api.fulfillmenttools.com
Firebase Web API key: AIzaSy…
fulfillmenttools project id: acme
Environment (e.g. staging, prod): pre
Username or full email address: jane.doe
Password:
Project "prod" added and is now active.
```

The password is masked as you type, never passed as a flag, and never written to your
shell history. A value you already gave as a flag is not asked for again, so you can mix
the two freely.

## Non-interactively (scripts, provisioning)

There is deliberately **no `--password` flag** — it would land in your shell history and
in `ps` output. Use `--password-stdin`:

```sh
echo "$PASSWORD" | fft project add prod \
  --base-url   https://ocff-acme-pre.api.fulfillmenttools.com \
  --api-key    "$FIREBASE_WEB_API_KEY" \
  --project-id acme \
  --env        pre \
  --username   jane.doe \
  --password-stdin
```

Pass `--email jane.doe@ocff-acme-pre.com` instead of `--username`/`--project-id`/`--env`
if you already know the full address. `--force` overwrites an existing project of the
same name.

## Working with several projects

```sh
fft project list                    # * marks the active one
fft project use staging             # switch
fft project current                 # which am I on?
fft facility list --project prod    # one command against another project
fft project remove staging          # forgets it, and wipes its keychain entries
```

```
$ fft project list
NAME        BASE URL                                        EMAIL                        CREDENTIAL
* prod      https://ocff-acme-pre.api.fulfillmenttools.com  jane.doe@ocff-acme-pre.com   keyring
  staging   https://ocff-acme-stg.api.fulfillmenttools.com  jane.doe@ocff-acme-stg.com   keyring
```

## Where things are kept

- **Secrets** (password, refresh token, ID token) → your **OS keychain**, one entry each.
- **Everything else** (name, base URL, email, active project) → `~/.config/fft/config.yaml`,
  mode `0600`. Plain YAML; safe to read, edit, and commit to a dotfiles repo — it contains
  no secrets.

No keychain available (headless Linux, a container)? `--no-keyring` / `FFT_NO_KEYRING=1`
falls back to a `0600` file — on Windows that mode buys you less than it looks like, see
[On Windows, `--no-keyring` protects less than `0600` suggests](./auth.md#on-windows-no-keyring-protects-less-than-0600-suggests).
In CI, skip projects entirely — see [CI and headless use](./ci.md).

## Shell completion

```sh
fft completion zsh  > "${fpath[1]}/_fft"          # zsh
fft completion bash > /etc/bash_completion.d/fft  # bash
fft completion fish > ~/.config/fish/completions/fft.fish
```

---
