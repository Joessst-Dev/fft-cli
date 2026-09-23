package config

import "fmt"

// Fixed values of the emulator's headless recipe. They are not configurable: the
// emulator authenticates nobody, so the email and the token are placeholders that
// only have to be present, and the Firebase key is never sent anywhere.
const (
	emulatorFirebaseAPIKey = "emulator"
	emulatorEmail          = "dev@localhost"
	// Not a credential: the emulator accepts every request without looking at the
	// bearer token, and this string exists only so that FromEnv's all-or-nothing set
	// is complete and no sign-in is attempted.
	emulatorIDToken = "emulator-token" //nolint:gosec // a placeholder the emulator never reads
)

// EnvVar is one environment variable and its value.
type EnvVar struct{ Name, Value string }

// EmulatorBaseURL is where an emulator on port answers.
func EmulatorBaseURL(port int) string {
	return fmt.Sprintf("http://localhost:%d", port)
}

// EmulatorEnv is the environment that points fft at the emulator serving baseURL,
// in the order it is shown.
//
// It is the headless id-token path ([FromEnv]) because that is the only door in: the
// emulator cannot stand in for Google's sign-in, so `fft project add` has nothing to
// sign in to and these four variables are the whole of the configuration.
//
// It lives here, beside the variables it names, because three places need exactly the
// same four lines — the emulator's own start-up notice, the UI's recipe, and the UI's
// runs — and three hand-written copies of a credential recipe drift.
func EmulatorEnv(baseURL string) []EnvVar {
	return []EnvVar{
		{EnvBaseURL, baseURL},
		{EnvFirebaseAPIKey, emulatorFirebaseAPIKey},
		{EnvEmail, emulatorEmail},
		{EnvIDToken, emulatorIDToken},
	}
}
