---
title: fft skill
---

# fft skill

Install the agent skill that teaches an AI to use fft

Install the fft agent skill, so an AI coding assistant can drive fft.

A skill is documentation an agent loads when it needs it: what the commands are, how
to discover the API's 557 operations, how to read an exit code — and the things no
--help can teach it, like the fact that stdout is data, that a POST is not
necessarily a write, and that it must ask before it changes anything.

  fft skill install              # ~/.claude/skills/fft   (Claude Code, personal)
  fft skill install --local      # ./.claude/skills/fft   (this project only)
  fft skill show >> AGENTS.md    # any other agent

The skill ships inside this binary, so it is always the one that describes the
commands you actually have. Every fft invocation in it is resolved against the real
command tree by a spec, so a renamed flag fails fft's build rather than quietly
making the skill lie to your agent.

## Usage

```
fft skill
```

## Subcommands

- [fft skill install](./fft_skill_install.md) — Install the skill for an AI assistant to read
- [fft skill show](./fft_skill_show.md) — Print the skill's markdown to stdout

## See also

- [fft](./fft.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
