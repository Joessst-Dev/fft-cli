package client

import (
	"net/http"
	"net/url"
)

// PinRedirect exposes the tenant client's CheckRedirect policy so its origin-pinning
// edge cases — host case, default-port normalization — can be exercised directly.
// httptest servers always listen on 127.0.0.1 with an explicit non-default port, so
// they cannot reproduce a same-origin redirect that differs only in host case or an
// implicit :443/:80.
var PinRedirect = pinRedirect

// TunnelTransport is the tenant transport pointed at a specific proxy. The real one
// reads HTTPS_PROXY through a sync.Once-cached lookup, which a spec cannot reset, and
// skips loopback targets besides.
func TunnelTransport(proxy *url.URL) http.RoundTripper {
	t := transport().(*http.Transport)
	t.Proxy = http.ProxyURL(proxy)
	return t
}
