---
title: fft component
---

# fft component

Manage installed components

Manage the components installed alongside fft.

A component is a building block somebody else wrote: a binary plus a manifest,
installed under ~/.local/share/fft/components, that adds commands to fft or
teaches the emulator to deliver events somewhere it does not know about. Once
installed, its commands appear in 'fft --help' like any other.

fft runs a component as you, with the environment it builds for it — so a
component only receives a tenant credential if its manifest asks for one, and
never receives the Firebase API key. That is a boundary, not a sandbox: a
component is code you chose to trust, in the way a shell alias or a git subcommand
is. Install ones you would run by hand.

Every install is pinned to a release and checked against that release's
checksums.txt before a byte of it is unpacked. Use --path to install a component
you built yourself.

## Usage

```
fft component
```

## Subcommands

- [fft component info](./fft_component_info.md) — Show a component's manifest
- [fft component init](./fft_component_init.md) — Scaffold a new component
- [fft component install](./fft_component_install.md) — Install a component
- [fft component list](./fft_component_list.md) — List installed components
- [fft component remove](./fft_component_remove.md) — Remove an installed component
- [fft component upgrade](./fft_component_upgrade.md) — Upgrade an installed component

## See also

- [fft](./fft.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
