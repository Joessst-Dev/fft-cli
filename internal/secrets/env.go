package secrets

import "os"

// envVars maps a secret kind to the environment variable that carries it in
// headless mode. The project part of the key is ignored: a CI job has exactly
// one project — the ephemeral one synthesized from these same variables.
var envVars = map[string]string{
	KindPassword:     "FFT_PASSWORD",
	KindRefreshToken: "FFT_REFRESH_TOKEN",
	KindIDToken:      "FFT_ID_TOKEN",
	KindIDTokenExp:   "FFT_ID_TOKEN_EXPIRES_AT",
}

// envStore reads secrets from the environment. It is read-only: a CI runner has
// nowhere durable to put a refreshed token, and pretending otherwise would hide
// the fact that the token is discarded when the process exits.
type envStore struct {
	lookup func(string) (string, bool)
}

// NewEnv returns a read-only Store backed by the FFT_* environment variables.
// lookup may be nil, in which case os.LookupEnv is used.
func NewEnv(lookup func(string) (string, bool)) Store {
	if lookup == nil {
		lookup = os.LookupEnv
	}
	return envStore{lookup: lookup}
}

func (envStore) Kind() string { return "env" }

func (s envStore) Get(key string) (string, error) {
	_, kind, ok := ParseKey(key)
	if !ok {
		return "", ErrNotFound
	}
	name, ok := envVars[kind]
	if !ok {
		return "", ErrNotFound
	}
	val, ok := s.lookup(name)
	if !ok || val == "" {
		return "", ErrNotFound
	}
	return val, nil
}

func (envStore) Set(string, string) error { return ErrReadOnly }

// Delete is a no-op rather than an error: tearing down a project that was never
// persisted should not fail.
func (envStore) Delete(string) error { return nil }
