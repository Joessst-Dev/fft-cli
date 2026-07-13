package auth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/config"
	"github.com/Joessst-Dev/fft-cli/internal/secrets"
)

func TestAuth(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "internal/auth")
}

// The specs live *inside* the package for one reason: withEndpoints, which points
// the client at the fakes below, is unexported. That is the security property —
// nothing outside internal/auth can redirect the Firebase API key at a host of
// its choosing — and testing it from outside would mean giving it away.

const (
	testAPIKey   = "AIzaSyTEST-not-a-real-key"
	testEmail    = "bot@ocff-acme-staging.com"
	testPassword = "correct horse battery staple"
	testProject  = "staging"
)

// google is a pair of fake Google identity endpoints: identitytoolkit (sign-in,
// camelCase) and securetoken (refresh, snake_case). They are two servers because
// in production they are two hosts, and the whole point of several of these specs
// is that the two responses do not have the same shape.
type google struct {
	signIn  *httptest.Server
	refresh *httptest.Server

	mu sync.Mutex

	// signInStatus and refreshStatus drive the failure paths. 0 means 200.
	signInStatus, refreshStatus int
	// signInError and refreshError are the Google error codes returned with a
	// non-200 status.
	signInError, refreshError string

	// signIns and refreshes count the calls, which is how "concurrent callers mint
	// only once" is asserted.
	signIns, refreshes int

	// keys and queries record what actually arrived, so that a spec can prove the
	// API key was sent to Google (and, elsewhere, that it was not sent anywhere
	// else).
	keys []string

	// lifetime is what the fakes report as expiresIn / expires_in — as a JSON
	// *string*, which is what the live API really returns.
	lifetime string
}

func newGoogle() *google {
	g := &google{lifetime: "3600"}

	g.signIn = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		defer g.mu.Unlock()

		g.signIns++
		g.keys = append(g.keys, r.URL.Query().Get("key"))

		var body struct {
			Email             string `json:"email"`
			Password          string `json:"password"`
			ReturnSecureToken bool   `json:"returnSecureToken"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}

		if g.signInStatus != 0 {
			writeGoogleError(w, g.signInStatus, g.signInError)
			return
		}
		if body.Password != testPassword {
			writeGoogleError(w, http.StatusBadRequest, "INVALID_LOGIN_CREDENTIALS")
			return
		}

		// camelCase — the confirmed shape of the sign-in response.
		writeJSON(w, map[string]any{
			"kind":         "identitytoolkit#VerifyPasswordResponse",
			"idToken":      fmt.Sprintf("id-from-signin-%d", g.signIns),
			"refreshToken": fmt.Sprintf("refresh-from-signin-%d", g.signIns),
			"expiresIn":    g.lifetime,
			"email":        body.Email,
			"localId":      "local-123",
			"registered":   true,
		})
	}))

	g.refresh = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		defer g.mu.Unlock()

		g.refreshes++
		g.keys = append(g.keys, r.URL.Query().Get("key"))

		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}

		if g.refreshStatus != 0 {
			writeGoogleError(w, g.refreshStatus, g.refreshError)
			return
		}
		if r.PostForm.Get("grant_type") != "refresh_token" || r.PostForm.Get("refresh_token") == "" {
			writeGoogleError(w, http.StatusBadRequest, "INVALID_REFRESH_TOKEN")
			return
		}

		// snake_case — a *different shape* from the sign-in response above. This is
		// the trap the whole package is arranged around.
		writeJSON(w, map[string]any{
			"access_token":  fmt.Sprintf("id-from-refresh-%d", g.refreshes),
			"expires_in":    g.lifetime,
			"token_type":    "Bearer",
			"refresh_token": fmt.Sprintf("refresh-from-refresh-%d", g.refreshes),
			"id_token":      fmt.Sprintf("id-from-refresh-%d", g.refreshes),
			"user_id":       "local-123",
			"project_id":    "12345",
		})
	}))

	DeferCleanup(func() {
		g.signIn.Close()
		g.refresh.Close()
	})

	return g
}

// client returns a Client pointed at the fakes, with a clock the spec controls.
func (g *google) client(now func() time.Time) *Client {
	c, err := NewClient(testAPIKey,
		withEndpoints(g.signIn.URL, g.refresh.URL),
		WithClock(now))
	Expect(err).NotTo(HaveOccurred())
	return c
}

func (g *google) failSignIn(status int, code string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.signInStatus, g.signInError = status, code
}

func (g *google) failRefresh(status int, code string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.refreshStatus, g.refreshError = status, code
}

func (g *google) setLifetime(seconds string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.lifetime = seconds
}

func (g *google) counts() (signIns, refreshes int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.signIns, g.refreshes
}

func (g *google) receivedKeys() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.keys...)
}

func writeJSON(w http.ResponseWriter, body map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	Expect(json.NewEncoder(w).Encode(body)).To(Succeed())
}

func writeGoogleError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	Expect(json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{"code": status, "message": code, "status": "INVALID_ARGUMENT"},
	})).To(Succeed())
}

// clock is a time the spec moves by hand, so that "this token has four minutes
// left" is a fact and not a race with the wall clock.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func newClock() *clock {
	return &clock{now: time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)}
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// testProjectConfig is the project the fakes authenticate.
func testProjectConfig() config.Project {
	return config.Project{
		Name:           testProject,
		BaseURL:        "https://acme.api.fulfillmenttools.com",
		FirebaseAPIKey: testAPIKey,
		Email:          testEmail,
	}
}

// storeWithPassword is the credential store a configured project has: a password
// and nothing else, exactly as `fft project add` leaves it.
func storeWithPassword(password string) *secrets.MemStore {
	store := secrets.NewMem()
	Expect(store.Set(secrets.Key(testProject, secrets.KindPassword), password)).To(Succeed())
	return store
}

// hostOf is a spec-readable "which host did this URL point at".
func hostOf(raw string) string {
	u, err := url.Parse(raw)
	Expect(err).NotTo(HaveOccurred())
	return u.Hostname()
}
