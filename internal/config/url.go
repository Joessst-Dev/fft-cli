package config

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// NormalizeBaseURL validates and canonicalises a project's API root.
//
// A bare host is assumed to be https, and a trailing slash is dropped so that
// joining a path onto the result never produces a double slash. Plain http is
// refused unless the host is a loopback address: a bearer token sent in the
// clear to a real fulfillmenttools tenant is a credential leak, and the only
// legitimate reason to point fft at http is a mock server on localhost.
func NormalizeBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("the base URL is empty")
	}

	// "acme.api.fulfillmenttools.com" parses as a *path*, not a host, so give a
	// scheme-less value the scheme it obviously meant.
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse the base URL %q: %w", raw, err)
	}

	switch u.Scheme {
	case "https":
	case "http":
		if !isLoopback(u.Hostname()) {
			return "", fmt.Errorf("the base URL %q uses http: fft would send your bearer token in the clear (http is allowed only for localhost)", raw)
		}
	default:
		return "", fmt.Errorf("the base URL %q uses scheme %q: want https", raw, u.Scheme)
	}

	if u.Host == "" {
		return "", fmt.Errorf("the base URL %q has no host", raw)
	}

	// Query strings and fragments on an API root are always a paste accident,
	// and silently keeping them would corrupt every request built from it.
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("the base URL %q has a query or fragment: give the API root only, for example https://acme.api.fulfillmenttools.com", raw)
	}

	u.Path = strings.TrimSuffix(u.Path, "/")
	return u.String(), nil
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
