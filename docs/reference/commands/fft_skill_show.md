---
title: fft skill show
---

# fft skill show

Print the skill's markdown to stdout

Print the agent skill.

For an assistant that reads a single context file rather than a directory of
skills:

  fft skill show >> AGENTS.md

The markdown goes to stdout exactly as it is, --output and all: it is a document,
not a record, and the point of this command is to redirect it somewhere. Use
-o json to get the same thing wrapped with its name and description, for a tool
that wants to read those.

## Usage

```
fft skill show
```

## See also

- [fft skill](./fft_skill.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
