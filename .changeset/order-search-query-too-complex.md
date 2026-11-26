---
"fft": patch
---

`fft order search` now explains the API's "Query too complex" error. On a tenant with a very large order history the API rejects `--since`/`--until` and `--sort`, and shrinking `--size` does not help; fft now points to narrower filters (`--tenant-order-id`) and `fft routing decision-logs` (newest routings first). The exit code is unchanged, and `--help` documents the limit.
