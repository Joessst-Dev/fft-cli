---
title: fft skill status
editLink: false
---

# fft skill status

Report which fft version each installed skill describes

Report which version of fft each installed skill describes.

An installed skill is a copy, and it describes the fft that installed it — not the
one you are running now. With no flags this looks in both places Claude Code reads a
skill from, ~/.claude/skills/fft and ./.claude/skills/fft; --local and --dir look in
one.

  CURRENT     what this fft ships
  OUTDATED    installed by another fft version: run fft skill install
  MODIFIED    this version, with files edited or added since: --force puts it back
  MISSING     nothing installed there
  NOT_SKILL   the directory holds files that are not fft's skill
  UNREADABLE  fft could not look: the reason is on stderr, and in "error" under -o json

A build that did not come from a release tag has no version to compare, and
counts every skill as current.

It only reads the installed files, sends nothing, and exits 0 whatever it finds:
it is a report, and a skill that is outdated or cannot be read is not a failure of
this command.

## Usage

```
fft skill status [flags]
```

## Flags

```
      --dir string   Look only in this directory (the skill is DIR/fft)
      --local        Look only in ./.claude/skills
```

## See also

- [fft skill](./fft_skill.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
