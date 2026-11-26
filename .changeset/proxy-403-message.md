---
"fft": patch
---

A refusal from a proxy, firewall or gateway between fft and the tenant is now reported as such, instead of sending you to `fft auth whoami`. That covers a TLS-intercepting proxy that answers a 401, 403 or 407 inside the tunnel (with the proxy's own message), and an `HTTPS_PROXY` proxy that refuses to open the tunnel at all, which used to print only `Forbidden` or `Proxy Authentication Required`. Exit codes are unchanged.
