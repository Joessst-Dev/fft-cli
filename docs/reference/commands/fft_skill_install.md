---
title: fft skill install
---

# fft skill install

Install the skill for an AI assistant to read

Copy the agent skill into a directory an AI assistant reads.

By default that is ~/.claude/skills/fft, where Claude Code finds a personal skill —
one installed once, available in every project. --local installs it into
./.claude/skills/fft instead, where it belongs to this project alone and can be
committed with it. --dir puts it wherever you say, for an agent that reads skills
from somewhere else.

Installing twice is silent and changes nothing: fft compares what it ships against
what is there. A file you have edited is a CONFLICT, and fft asks before replacing
it — or refuses, with exit 2, if there is no terminal to ask on. --force answers
that question in advance.

The skill needs no project, no credentials and no network, so this is a reasonable
first thing to run on a new machine.

## Usage

```
fft skill install [flags]
```

## Flags

```
      --dir string   Install into this directory instead (the skill lands in DIR/fft)
      --force        Replace files that differ from the ones fft ships
      --local        Install into ./.claude/skills instead of ~/.claude/skills
```

## See also

- [fft skill](./fft_skill.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
