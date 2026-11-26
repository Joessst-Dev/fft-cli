---
"fft": patch
---

A 401 or 403 that comes from a proxy, firewall or gateway instead of the tenant is now reported as such, with the proxy's own message, instead of sending you to `fft auth whoami`. Exit codes are unchanged.
