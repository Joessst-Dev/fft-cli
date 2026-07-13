package config

import (
	"os"
	"strings"
)

// EphemeralName is the name given to the project synthesized from the
// environment. It shows up in `fft project list` so that a confused CI log is
// still readable.
const EphemeralName = "env"

// Environment variables that describe a headless project.
const (
	EnvBaseURL        = "FFT_BASE_URL"
	EnvFirebaseAPIKey = "FFT_FIREBASE_API_KEY"
	EnvEmail          = "FFT_EMAIL"
	EnvPassword       = "FFT_PASSWORD"
	EnvIDToken        = "FFT_ID_TOKEN"
	EnvUsername       = "FFT_USERNAME"
	EnvProjectID      = "FFT_PROJECT_ID"
	EnvEnvironment    = "FFT_ENVIRONMENT"
	EnvTenant         = "FFT_TENANT"

	// EnvEnv is an alias for EnvEnvironment, matching the --env flag of
	// `fft project add`. A user who wrote --env and then exported the same value
	// should not have to discover that the variable is spelled differently.
	EnvEnv = "FFT_ENV"
)

// FromEnv synthesizes an ephemeral project from the environment, reporting
// whether one was found.
//
// This is how fft works in CI, and it is not a convenience. A GitHub Linux
// runner has no Secret Service, so the OS keychain is simply unavailable there;
// a headless project therefore touches neither the keychain nor the config file
// — everything it needs comes from the environment and dies with the process.
//
// A project is synthesized only when the base URL, the Firebase Web API key, an
// email and *some* credential (a password to sign in with, or an id token to use
// directly) are all present. A partial set is ignored rather than half-honoured:
// silently falling back to the config file when one variable is missing is how a
// CI job ends up running against the wrong tenant.
//
// FFT_EMAIL may be left out when FFT_USERNAME, FFT_PROJECT_ID and
// FFT_ENV/FFT_ENVIRONMENT are given: the synthetic address is then built from
// them exactly as `fft project add --username` does. It is only a candidate — the
// sign-in is what settles it — but it means a CI job configures the same four
// values a human types, rather than having to know how the address is spelled.
//
// lookup may be nil, in which case os.LookupEnv is used.
func FromEnv(lookup func(string) (string, bool)) (Project, bool) {
	if lookup == nil {
		lookup = os.LookupEnv
	}
	get := func(name string) string {
		v, _ := lookup(name)
		return strings.TrimSpace(v)
	}
	// The longer, explicit name wins when both are set.
	environment := get(EnvEnvironment)
	if environment == "" {
		environment = get(EnvEnv)
	}

	p := Project{
		Name:           EphemeralName,
		BaseURL:        get(EnvBaseURL),
		FirebaseAPIKey: get(EnvFirebaseAPIKey),
		Email:          get(EnvEmail),
		Username:       get(EnvUsername),
		Tenant:         get(EnvTenant),
		ProjectID:      get(EnvProjectID),
		Environment:    environment,
		Ephemeral:      true,
	}
	if p.Email == "" {
		p.Email = CandidateEmail(p.Username, p.ProjectID, p.Environment)
	}

	hasCredential := get(EnvPassword) != "" || get(EnvIDToken) != ""
	if p.BaseURL == "" || p.FirebaseAPIKey == "" || p.Email == "" || !hasCredential {
		return Project{}, false
	}
	return p, true
}
