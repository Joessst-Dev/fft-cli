---
"fft": minor
---

Network failures and timeouts now exit 9 (upstream unavailable) instead of 1, so scripts can retry them: a refused or reset connection, a DNS or TLS failure, a proxy that is down (a 5xx on `CONNECT`), and `--timeout` expiring. The message still names the URL. Ctrl-C still exits 130, and a proxy refusing with 403 or 407 still exits 1, because retrying will not change it.
