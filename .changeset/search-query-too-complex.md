---
"fft": patch
---

Every search now explains the API's "Query too complex" error, not only `fft order search`. On a tenant with a very large collection the API refuses a filter or a sort with HTTP 400, and its own advice (reduce the result set size) does not help: fft now says that a smaller `--size` will not help and to narrow the query with an exact-match filter, dropping any date range and the sort. This covers the curated `list` and `search` commands (with a sharper hint for `stock list`, `listing list` and `facility list`), every generated `search-*` command and `fft api search<Entity>`. An `--all` walk that is refused part-way now says how many pages and items it had read, and `fft network snapshot` stops at once instead of pausing and retrying. The exit code is unchanged.
