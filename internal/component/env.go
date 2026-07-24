package component

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Joessst-Dev/fft-cli/internal/config"
)

// Variables fft sets on every component it runs, whatever session it grants.
const (
	// EnvAPI is the manifest contract version of the fft that spawned the component,
	// so a component can refuse a host it does not understand instead of failing in
	// some more interesting way further in.
	EnvAPI = "FFT_COMPONENT_API"

	// EnvName is the component's own name, which is how a binary serving several
	// components — or one invoked by hand — knows which one it is being.
	EnvName = "FFT_COMPONENT_NAME"

	// EnvVersion is the version of fft itself. A component that shells out to fft can
	// check it; one that does not can ignore it.
	EnvVersion = "FFT_VERSION"
)

// apiKeyPlaceholder is what a component is given in place of the Firebase Web API
// key.
//
// [config.FromEnv] refuses to synthesize a project unless the key is non-empty, so
// a component that shells back out to fft needs *something* there. It does not need
// the real one: the key is the credential that mints fresh tokens, indefinitely,
// long after the short-lived id token beside it has expired. Handing it over would
// make every component a permanent credential holder, which is the opposite of what
// a session level is for.
//
// The value is deliberately a word rather than a plausible key, so that a component
// which somehow does try to sign in with it fails at once and unmistakably.
const apiKeyPlaceholder = "component"

// SessionInfo is the tenant session the parent already resolved: everything a
// component needs to talk to the API, and nothing it needs to obtain it.
//
// The token is short-lived and minted by the parent through its own token source,
// which is what keeps the keychain a thing only fft itself opens.
type SessionInfo struct {
	BaseURL     string
	Email       string
	Token       string
	Project     string
	Tenant      string
	ProjectID   string
	Environment string

	// ReadOnly is the parent's own read-only state, propagated so that a component
	// granted a write session under a read-only project still refuses writes. The
	// gate has already refused the command by then; this is the belt to its braces.
	ReadOnly bool
}

// EnvOptions is what [Environ] needs from the caller that is not the manifest.
type EnvOptions struct {
	// Root is the component root, passed on so a component can find its own
	// sub-components — the emulator finds its transports this way.
	Root string

	// Version is fft's own version.
	Version string

	// Output is the -o format, NoColor the --no-color flag and Timeout the --timeout
	// duration, forwarded so a component that renders anything renders it the way the
	// user asked for.
	Output  string
	NoColor bool
	Timeout time.Duration

	// Session is the resolved tenant session, or nil when there is none. It is
	// required for a command declaring anything but [SessionNone].
	Session *SessionInfo

	// Extra is configuration the host wants to hand the component, by variable name.
	//
	// Only the names the manifest declares in [Manifest.Env] are actually set, and
	// that filter is the point of the field existing at all: the emulator turns
	// --pubsub-emulator-host into PUBSUB_EMULATOR_HOST for a transport that says it
	// reads it, rather than pushing every flag it has at every child. What a
	// component consumes stays something it declared and `fft component info` can
	// print.
	Extra map[string]string
}

// Environ builds the environment a component runs with.
//
// This is the whole credential boundary, and it works by subtraction. Every
// FFT_-prefixed variable is stripped from the inherited environment first, and then
// exactly what the declared session allows is put back. So a developer with
// FFT_PASSWORD exported in their shell does not silently hand it to every component
// they run, and a component cannot reach the tenant by any route the manifest did
// not ask for. Everything outside the FFT_ namespace — PATH, HOME, PUBSUB_EMULATOR_HOST,
// a proxy setting — is inherited untouched, because those belong to the machine and
// not to fft.
//
// base is the environment to inherit, in os.Environ form.
func Environ(base []string, c Component, cmd Command, opts EnvOptions) ([]string, error) {
	env := make(map[string]string, len(base)+8)
	for _, entry := range base {
		name, value, ok := strings.Cut(entry, "=")
		if !ok || strings.HasPrefix(name, "FFT_") {
			continue
		}
		env[name] = value
	}

	// Before the fixed set below, so a component cannot redefine FFT_COMPONENT_API
	// through a variable it declared. Validation already refuses an FFT_ name in the
	// manifest; this is what makes that refusal load-bearing rather than advisory.
	for _, name := range c.Env {
		if value, ok := opts.Extra[name]; ok && value != "" {
			env[name] = value
		}
	}

	env[EnvAPI] = strconv.Itoa(APIVersion)
	env[EnvName] = c.Name
	env[EnvVersion] = opts.Version
	env[EnvRoot] = opts.Root

	if opts.Output != "" {
		env["FFT_OUTPUT"] = opts.Output
	}
	if opts.NoColor {
		env["FFT_NO_COLOR"] = "1"
	}
	if opts.Timeout > 0 {
		env["FFT_TIMEOUT"] = opts.Timeout.String()
	}

	if err := addSession(env, cmd, opts.Session); err != nil {
		return nil, err
	}
	return flatten(env), nil
}

// addSession puts back the tenant credentials the declared session allows.
func addSession(env map[string]string, cmd Command, s *SessionInfo) error {
	if cmd.Session == SessionNone {
		// Nothing at all — not even the base URL. A component that needs no credential
		// should not be told where the tenant is either; the emulator is the case that
		// proves the level is worth having.
		return nil
	}

	if s == nil {
		return fmt.Errorf("command %q needs a %s session, but none was resolved", cmd.Name, cmd.Session)
	}

	env[config.EnvBaseURL] = s.BaseURL
	env[config.EnvEmail] = s.Email
	env[config.EnvIDToken] = s.Token
	env[config.EnvFirebaseAPIKey] = apiKeyPlaceholder

	// Optional identity, set only when known: an empty FFT_TENANT is not the same
	// answer as an absent one, and [config.FromEnv] reads both.
	for name, value := range map[string]string{
		config.EnvTenant:      s.Tenant,
		config.EnvProjectID:   s.ProjectID,
		config.EnvEnvironment: s.Environment,
	} {
		if value != "" {
			env[name] = value
		}
	}

	if cmd.Session == SessionRead || s.ReadOnly {
		env[config.EnvReadOnly] = "1"
	}
	return nil
}

// flatten renders the map back into os.Environ form, sorted — so that a spec can
// assert on the whole environment, and a --debug dump reads the same way twice.
func flatten(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for name, value := range env {
		out = append(out, name+"="+value)
	}
	sort.Strings(out)
	return out
}
