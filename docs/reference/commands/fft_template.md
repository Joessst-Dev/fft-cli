---
title: fft template
---

# fft template

Save request bodies and render them with parameters

Save a request body you send often, and change the parts that vary.

A template is a JSON file holding a body, the operation it was written for, and
the parameters that change between sends. `fft template render` prints the
finished body on stdout, so it composes with every command that takes --file:

    fft template render rush-order --set email=a@b.de | fft order create --file -

Nothing here reaches the tenant. Rendering makes no request, needs no project and
needs no credentials, which is why it is safe to put in the middle of a pipe and
why a read-only project cannot refuse it — the command that receives the body is
still gated exactly as it was.

Templates live in two places. `--local` writes ./.fft/templates, which is meant to
be committed and shared with whoever clones the repository; without it they go to
your own $XDG_DATA_HOME/fft/templates. A project template of the same name wins.

A body captured from real work carries real ids. Read a template before you commit
it — a facility id or a consumer email in git history is not something you can
quietly take back later.

## Usage

```
fft template
```

## Subcommands

- [fft template list](./fft_template_list.md) — List saved templates
- [fft template remove](./fft_template_remove.md) — Delete a saved template
- [fft template render](./fft_template_render.md) — Print a saved body with its parameters filled in
- [fft template save](./fft_template_save.md) — Save a request body as a template
- [fft template show](./fft_template_show.md) — Describe a saved template

## See also

- [fft](./fft.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
