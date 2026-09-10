---
title: AI agents
---

# Letting an AI agent drive fft

`fft` ships an **agent skill**: the documentation an AI coding assistant reads before it
runs `fft` on your behalf. It covers the command surface, how to discover the API's 561
operations, and the things no `--help` can teach it — that stdout is data and stderr is
everything else, that a POST is not necessarily a write, that exit 8 means *some* of a
bulk write landed, and that it must ask you before it changes anything.

```sh
fft skill install              # ~/.claude/skills/fft — Claude Code, every project
fft skill install --local      # ./.claude/skills/fft — this project, committable
fft skill install --dir DIR    # anywhere else
```

For an assistant that reads a single context file rather than a directory of skills:

```sh
fft skill show >> AGENTS.md
```

The skill needs no project, no credentials and no network, so it is a reasonable first
thing to run on a new machine. Installing twice changes nothing; a file you have edited is
never replaced without `--force`, or without asking.

## It cannot quietly go stale

The skill is compiled into the binary, so it always describes the commands you actually
have — and every `fft` invocation in it is resolved against the real command tree by a
spec. Rename a flag and `fft`'s own build fails, naming the file and line of the snippet
that has started lying.

## Reading it without installing it

The skill *is* the guide pages you are reading: [Overview](./overview.md) is its
`SKILL.md`, and [Commands](./commands.md), [Discovery](./discovery.md),
[Recipes](./recipes.md), [Templates](./templates.md), [Emulator](./emulator.md),
[Components](./components.md) and [Troubleshooting](./troubleshooting.md) are its
reference files. They are written for an agent, which makes them unusually direct — worth
reading yourself.

The source lives at
[`internal/skill/assets/`](https://github.com/Joessst-Dev/fft-cli/tree/main/internal/skill/assets).
