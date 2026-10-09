---
title: AI agents
---

# Letting an AI agent drive fft

`fft` ships an **agent skill**: the documentation an AI coding assistant reads before it
runs `fft` on your behalf. It covers the command surface, how to discover the API's 568
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

## Keeping an installed copy current

An installed skill is a copy, and a copy describes the `fft` that made it. So
`fft skill install` stamps its version into the skill's frontmatter:

```yaml
metadata:
  version: "1.4.0"
```

After an upgrade, `fft` notices the mismatch and says so on stderr, after any command
that succeeded:

```text
The fft skill in /home/you/.claude/skills/fft is from fft 1.3.0 (you have 1.4.0) — run fft skill install
```

The notice follows the [update check](./install.md#upgrading)'s rules: release builds
only, a terminal on stderr, table output, and `FFT_NO_UPDATE_CHECK` or
`settings.updateCheck: false` turn it off. It never appears under `fft skill` itself. A
skill installed before versions were stamped is reported as from "an older fft".

`fft skill status` reports both locations Claude Code reads from — `CURRENT`, `OUTDATED`,
`MODIFIED` (edited since), `MISSING`, `NOT_SKILL` or `UNREADABLE` — and `-o json` gives a
script the same answer. It only reads the installed files, sends nothing, and always
exits 0.

Reinstalling after an upgrade needs no `--force` as long as you have not edited the
skill. `fft skill install` records what it wrote in `.fft-skill.json` beside the skill,
and knows the text of every earlier release, so a file that still holds an older fft's
text is `UPDATED` without a question, and a file fft no longer ships is removed the same
way. A file you *have* edited is still yours: it is a `CONFLICT` (or `STALE`, if fft no
longer ships it), and fft asks before replacing it, or refuses without a terminal unless
you pass `--force`.

## Reading it without installing it

The skill *is* the guide pages you are reading: [Overview](./overview.md) is its
`SKILL.md`, and [Commands](./commands.md), [Discovery](./discovery.md),
[Recipes](./recipes.md), [Templates](./templates.md), [Emulator](./emulator.md),
[Components](./components.md) and [Troubleshooting](./troubleshooting.md) are its
reference files. They are written for an agent, which makes them unusually direct — worth
reading yourself.

The source lives at
[`internal/skill/assets/`](https://github.com/Joessst-Dev/fft-cli/tree/main/internal/skill/assets).
