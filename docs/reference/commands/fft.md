---
title: fft
---

# fft

Command-line client for the fulfillmenttools API

fft is a command-line client for the fulfillmenttools API.

It replaces hand-rolled curl requests and Postman collections: set up your
projects once, switch between them freely, and let fft obtain and refresh
access tokens for you.

Every command's --help explains what the underlying endpoint does, which
permission it needs, and shows a sample request body.

Configuration lives in ~/.config/fft/config.yaml (mode 0600); credentials live in
your OS keychain. In CI, set FFT_BASE_URL, FFT_FIREBASE_API_KEY, FFT_EMAIL and
FFT_PASSWORD (or FFT_ID_TOKEN): fft then runs entirely from the environment and
touches neither the config file nor the keychain.

Every global flag can also be given as an environment variable: --output becomes
FFT_OUTPUT, --project becomes FFT_PROJECT, and so on.

## Usage

```
fft
```

## Flags

```
      --debug              Log requests and responses to stderr
      --no-color           Disable coloured output
      --no-keyring         Store credentials in a 0600 file instead of the OS keychain
  -o, --output string      Output format: table, json, yaml (default "table")
      --project string     Project to act on (default: the active project)
      --read-only          Refuse any request that would change data (can only tighten, never loosen)
      --timeout duration   Timeout for a single command (default 30s)
  -y, --yes                Assume yes for every confirmation prompt
```

## Subcommands

- [fft api](./fft_api.md) — Call any API operation by its operationId
- [fft auth](./fft_auth.md) — Inspect and renew credentials
- [fft connection](./fft_connection.md) — Manage interfacility connections
- [fft emulator](./fft_emulator.md) — Run a local offline fulfillmenttools API emulator
- [fft facility](./fft_facility.md) — Manage facilities
- [fft listing](./fft_listing.md) — Manage listings (the article-at-facility catalog entry)
- [fft order](./fft_order.md) — Manage orders
- [fft ping](./fft_ping.md) — Check that the tenant is reachable
- [fft project](./fft_project.md) — Manage projects
- [fft skill](./fft_skill.md) — Install the agent skill that teaches an AI to use fft
- [fft sourcing](./fft_sourcing.md) — Simulate how an order would be routed
- [fft stock](./fft_stock.md) — Manage stocks (the quantity of an article at a facility)
- [fft update](./fft_update.md) — Check whether a newer fft release is available
- [fft version](./fft_version.md) — Print the fft version

