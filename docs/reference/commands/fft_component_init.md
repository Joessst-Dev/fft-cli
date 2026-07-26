---
title: fft component init
---

# fft component init

Scaffold a new component

Scaffold a new component.

Stamps a runnable skeleton — a valid component.yaml and an executable that does the
trivially-right thing — into &lt;dir>/&lt;name>, ready to install with:

  fft component install --path &lt;dir>/&lt;name>

Two kinds:

  command    adds 'fft &lt;name>'; the skeleton prints its arguments and the FFT_
             environment fft handed it — with the tenant token masked — so the
             contract is visible before you write any logic.
  transport  delivers emulator events to a broker; the skeleton answers the
             protocol's hello, refuses plan, and acks send.

Four languages (--lang): shell, python and node are interpreter scripts that run the
moment they are installed; go compiles to bin/, so it needs 'go build' first. The
default is shell for a command and go for a transport.

The emitted manifest is validated the same way 'fft component install' validates one,
so anything init produces is a manifest fft accepts.

## Usage

```
fft component init <name> [flags]
```

## Flags

```
      --dir string       Parent directory to create the component under (default ".")
      --kind string      What the component extends: command or transport (default "command")
      --lang string      Language of the executable: shell, go, python or node (default depends on --kind)
      --session string   For a command, the tenant session it receives: none, read or write (default "none")
```

## See also

- [fft component](./fft_component.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
