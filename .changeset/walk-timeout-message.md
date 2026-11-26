---
"fft": patch
---

`--all` list commands now say what happened when `--timeout` runs out mid-walk: the error names `--timeout`, how many pages and items were fetched, and suggests raising it, instead of a bare "context deadline exceeded". `--timeout` bounds the whole command, so large tenants need a larger value; the `--all` help now says so.
