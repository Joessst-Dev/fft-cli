---
"fft": minor
---

Generated list commands that page with `startAfterId` (for example `fft category get-categories`) now take `--all` and `--max-items`, with the same bound and the same warning on stderr when a list is cut short. `--size` is now checked against the maximum the API declares, so `--size 250` on a list capped at 100 fails with a usage error instead of an HTTP 400 — including through `fft api`.
