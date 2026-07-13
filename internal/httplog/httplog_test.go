package httplog_test

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/httplog"
)

func TestHTTPLog(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "httplog")
}

var _ = Describe("redacting a trace", func() {
	// A --debug dump is the single most likely thing a user pastes into a bug
	// report. The bar is not "unlikely to leak" but "cannot".
	DescribeTable("Redact",
		func(given string, wants []string, unwanted string) {
			got := httplog.Redact(given)

			for _, want := range wants {
				Expect(got).To(ContainSubstring(want))
			}
			Expect(got).NotTo(ContainSubstring(unwanted))
		},
		Entry("the Firebase API key on a query string",
			"https://identitytoolkit.googleapis.com/v1/accounts:signInWithPassword?key=AIzaSyRealKey",
			[]string{"key=" + httplog.Redacted, "signInWithPassword"},
			"AIzaSyRealKey"),
		Entry("the password in a sign-in body",
			`{"email":"bot@ocff-acme-prod.com","password":"hunter2-and-then-some","returnSecureToken":true}`,
			[]string{`"password":"` + httplog.Redacted + `"`, "bot@ocff-acme-prod.com"},
			"hunter2-and-then-some"),
		Entry("both tokens in a sign-in answer",
			`{"idToken":"eyJhbGciOi.header.sig","refreshToken":"AMf-vBxSecret","expiresIn":"3600"}`,
			[]string{`"expiresIn":"3600"`},
			"AMf-vBxSecret"),
		Entry("the snake_case tokens a refresh answers with",
			`{"id_token":"eyJhbGciOi.header.sig","refresh_token":"AMf-vBxSecret","expires_in":"3600"}`,
			[]string{httplog.Redacted},
			"eyJhbGciOi.header.sig"),
		Entry("the refresh token in a form-encoded body",
			"grant_type=refresh_token&refresh_token=AMf-vBxSecret",
			[]string{"grant_type=refresh_token", "refresh_token=" + httplog.Redacted},
			"AMf-vBxSecret"),
	)

	It("removes a secret it is handed verbatim, wherever it appears", func() {
		got := httplog.Redact("dial https://acme.example/x?t=super-secret-token", "super-secret-token")

		Expect(got).NotTo(ContainSubstring("super-secret-token"))
		Expect(got).To(ContainSubstring(httplog.Redacted))
	})

	It("leaves a very short secret alone, rather than mangling the message into uselessness", func() {
		// Replacing every occurrence of a three-letter password would redact half the
		// sentence, and the sentence is all the user has to go on.
		Expect(httplog.Redact("no such host: acme.api", "acme")).To(ContainSubstring("no such host: acme.api"))
	})
})

var _ = Describe("logging a request and its answer", func() {
	var (
		log    *bytes.Buffer
		server *httptest.Server
		client *http.Client
	)

	BeforeEach(func() {
		log = &bytes.Buffer{}

		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"facilities":[{"name":"Berlin Mitte"}]}`)
		}))
		DeferCleanup(server.Close)

		client = &http.Client{Transport: httplog.New(log, nil)}
	})

	send := func(body string) *http.Response {
		GinkgoHelper()

		req, err := http.NewRequest(http.MethodPost, server.URL+"/api/facilities/search", bytes.NewReader([]byte(body)))
		Expect(err).NotTo(HaveOccurred())
		req.Header.Set("Authorization", "Bearer eyJhbGciOiJSUzI1NiIsImtpZCI6")

		res, err := client.Do(req)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(res.Body.Close)
		return res
	}

	It("records the request line, the status and the body", func() {
		send(`{"query":{}}`)

		Expect(log.String()).To(ContainSubstring("> POST " + server.URL + "/api/facilities/search"))
		Expect(log.String()).To(ContainSubstring("200 OK"))
		Expect(log.String()).To(ContainSubstring(`{"query":{}}`))
		Expect(log.String()).To(ContainSubstring("Berlin Mitte"))
	})

	It("never writes the bearer token, which is a credential for as long as it lives", func() {
		send(`{"query":{}}`)

		Expect(log.String()).NotTo(ContainSubstring("eyJhbGciOiJSUzI1NiIsImtpZCI6"))
		Expect(log.String()).To(ContainSubstring("Authorization: " + httplog.Redacted))
	})

	It("leaves the response body intact for the caller that asked for it", func() {
		// The logger reads the body to print it. If it did not put it back, --debug
		// would break every command it was meant to explain.
		res := send(`{"query":{}}`)

		body, err := io.ReadAll(res.Body)

		Expect(err).NotTo(HaveOccurred())
		Expect(string(body)).To(Equal(`{"facilities":[{"name":"Berlin Mitte"}]}`))
	})

	It("writes nothing at all when there is nowhere to write it", func() {
		quiet := &http.Client{Transport: httplog.New(nil, nil)}

		req, err := http.NewRequest(http.MethodGet, server.URL, nil)
		Expect(err).NotTo(HaveOccurred())

		res, err := quiet.Do(req)

		Expect(err).NotTo(HaveOccurred())
		Expect(res.Body.Close()).To(Succeed())
		Expect(log.Len()).To(BeZero())
	})
})
