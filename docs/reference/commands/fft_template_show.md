---
title: fft template show
---

# fft template show

Describe a saved template

Describe a saved template: what it is for, what it sends, and what you can change.

This prints the whole template — the envelope, its declared parameters and the body
as saved. 'fft template render' prints only the body, with the parameters applied;
this is the one to read before you type a --set.

-o json and -o yaml print the file's own contents, so 'fft template show x -o json'
round-trips through 'fft template save x --file -'.

## Usage

```
fft template show <name>
```

## See also

- [fft template](./fft_template.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
