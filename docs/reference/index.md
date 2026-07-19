---
title: CLI reference
---

# CLI reference

Every curated `fft` command, generated from the binary itself — so it always
describes the commands you actually have. Pick one from the sidebar, or run
`fft <command> --help` for the same information at the terminal.

This reference covers the hand-written command surface. The rest of the API's 557
operations are reachable through the generated commands and the `fft api` escape
hatch — see [Discovery](/guide/discovery) for how to find any of them.

- [`fft`](/reference/commands/fft) — the root command and the global flags every
  command shares.
- [Overview](/guide/overview) and [Commands](/guide/commands) — start here if you
  want the narrative rather than the flag-by-flag reference.
- [Emulator](/guide/emulator) — run a local, offline API server (with Pub/Sub eventing)
  to try commands without a tenant.
