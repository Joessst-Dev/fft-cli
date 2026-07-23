---
title: fft project add
---

# fft project add

Configure a project

Configure a fulfillmenttools project.

Run without flags on a terminal and fft asks for each value in turn, masking the
password. Pass every flag and --password-stdin and it runs unattended, which is
what a provisioning script wants.

There is deliberately no --password flag: a password on the command line is
recorded in your shell history and visible in the process list to every other
user on the machine. Pipe it in instead:

  fft project add prd \
    --base-url https://acme.api.fulfillmenttools.com \
    --api-key AIza... \
    --project-id acme --env prd --username warehouse-bot \
    --password-stdin &lt; password.txt

The base URL is stored exactly as you give it. fft never derives it from the
project id, because the official documentation disagrees with itself about
whether the host is "{projectId}.api…" or "ocff-{projectId}.api…".

You may give either --email (used verbatim) or --username, from which fft builds
the synthetic address fulfillmenttools issues, {username}@ocff-{projectId}-{env}.com.

The fulfillmenttools API key (a Firebase Web API key) is treated as sensitive: it
goes to the keychain alongside the password and tokens, each under its own entry,
and is never written to the config file. It grants nothing on its own and is sent
only to Google's identity endpoints — never to fulfillmenttools.

## Usage

```
fft project add [name] [flags]
```

## Flags

```
      --api-key string      fulfillmenttools API key
      --base-url string     API root, e.g. https://acme.api.fulfillmenttools.com
      --email string        Email address to sign in with (use instead of --username)
      --env string          Environment, e.g. pre or prd
      --force               Overwrite an existing project of the same name
      --password-stdin      Read the password from stdin
      --project-id string   fulfillmenttools project id
      --read-only           Refuse every request that would change this project
      --tenant string       Tenant name (informational)
      --username string     Login name; the email is derived from it
```

## See also

- [fft project](./fft_project.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
