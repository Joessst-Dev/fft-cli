---
"fft": patch
---

`fft order search` now explains the API's "Query too complex" error. On a tenant with a very large order history the API rejects `--since`/`--until` and `--sort`, and shrinking `--size` does not help; fft now points to narrower filters (`--tenant-order-id`) and `fft routing decision-logs` (which in practice lists the newest routings first; the API does not guarantee an order). The exit code is unchanged, and `--help` documents the limit.
