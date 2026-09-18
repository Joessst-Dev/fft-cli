package secrets

import "strings"

// credentialNamePatterns are name substrings that a credential-shaped field or
// flag carries in fulfillmenttools and in fft: a password on user creation, a
// clientSecret or firebaseWebApiKey on SSO/OIDC config, a bearer token, an
// Authorization header.
var credentialNamePatterns = []string{"password", "secret", "apikey", "token", "authorization"}

// LooksLikeCredential reports whether name — a JSON key, a flag name, a header —
// is shaped like one that holds a credential.
//
// It is a name-shaped heuristic, not a value-shaped one: nothing can tell a real
// secret from a placeholder, so the callers use it to decide what to hold back or
// ask about, never to prove that something is safe. Case and the separators '-'
// and '_' are ignored, so api-key, API_KEY and apiKey all match.
func LooksLikeCredential(name string) bool {
	normalized := strings.ToLower(strings.NewReplacer("-", "", "_", "").Replace(name))
	for _, pattern := range credentialNamePatterns {
		if strings.Contains(normalized, pattern) {
			return true
		}
	}
	return false
}
