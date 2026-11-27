---
"fft": minor
---

`fft auth token --refresh` mints a new id token first and prints that one, so a token you hand to a CI job, a sandbox or a long-running script has its full lifetime instead of whatever was left of the cached one (as little as five minutes). It replaces `fft auth refresh && fft auth token`, exits 4 when neither the refresh token nor the stored password works and 2 for a fixed `FFT_ID_TOKEN`, and reports how long the new token is valid on stderr; stdout is still just the token. `fft auth token --help` now says when the cached token may be close to expiry.
