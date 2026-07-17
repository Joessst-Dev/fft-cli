---
title: Getting started
---

# Getting started

```sh
fft project add prod     # answer the prompts
fft ping                 # is the tenant reachable?
fft facility list        # you're in
```

That's the whole setup. `fft project add` **authenticates before it saves anything**, so a
typo in the password fails at setup rather than becoming a mystery an hour later — and it
stores the email that actually worked, rather than the one it guessed.
