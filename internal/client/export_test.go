package client

// PinRedirect exposes the tenant client's CheckRedirect policy so its origin-pinning
// edge cases — host case, default-port normalization — can be exercised directly.
// httptest servers always listen on 127.0.0.1 with an explicit non-default port, so
// they cannot reproduce a same-origin redirect that differs only in host case or an
// implicit :443/:80.
var PinRedirect = pinRedirect
