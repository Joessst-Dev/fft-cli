---
title: fft component install
---

# fft component install

Install a component

Install a component.

Three ways to name one:

  fft component install emulator            a component fft ships
  fft component install owner/repo          somebody else's, latest release
  fft component install owner/repo@v1.2.0   somebody else's, pinned
  fft component install --path ./mine       one you built yourself

The archive is downloaded, checked against the release's checksums.txt, and
unpacked into a staging directory. Nothing is installed until you have seen what
it is and said yes; -y/--yes answers for you.

A component runs as you, with whatever access you have. fft decides which
credentials it receives — a component only gets a tenant token if its manifest
asks for one, and never gets the Firebase API key — but it cannot stop code you
installed from doing what code can do. Read 'fft component info' before you say
yes to something you did not write.

## Usage

```
fft component install [<name>|<owner>/<repo>[@<version>]] [flags]
```

## Flags

```
      --path string   Install from a local, unpacked component directory instead of a release
```

## See also

- [fft component](./fft_component.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
