package auth

import (
	"fmt"
	"net/http"
)

// Transport adds `Authorization: Bearer …` to every request it carries.
//
// It carries nothing else. In particular the Firebase API key never appears here:
// the key is Google's, [Client] is the only thing that holds it, and a
// fulfillmenttools request that contained it would be handing a third party's
// credential to a tenant.
//
// # Where the 401 retry is not
//
// A reactive "on 401, refresh and retry" belongs in the client wrapper, not here.
// A RoundTripper is handed a request whose body is a one-shot io.ReadCloser: by
// the time the 401 comes back the body has been consumed, and replaying it is
// impossible in the general case. Retrying at this layer would work in the specs,
// where the body is a bytes.Reader, and silently send an empty POST in
// production.
type Transport struct {
	// Source mints the token. It is required.
	Source TokenSource

	// Base is the transport underneath. nil means http.DefaultTransport.
	Base http.RoundTripper
}

var _ http.RoundTripper = (*Transport)(nil)

// RoundTrip implements http.RoundTripper.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.Source == nil {
		return nil, fmt.Errorf("auth: the transport has no token source")
	}

	token, err := t.Source.Token(req.Context())
	if err != nil {
		return nil, err
	}

	// A RoundTripper must not modify the request it is given: the caller may still
	// hold it, and http.Client documents the contract explicitly.
	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", "Bearer "+token)

	return t.base().RoundTrip(clone)
}

func (t *Transport) base() http.RoundTripper {
	if t.Base != nil {
		return t.Base
	}
	return http.DefaultTransport
}
