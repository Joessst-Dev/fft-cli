package main

import (
	"encoding/json"
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

var _ = Describe("fft ping", func() {
	var c *cli

	BeforeEach(func() {
		c = newCLI()
	})

	When("the tenant answers", func() {
		var requests *[]*http.Request

		BeforeEach(func() {
			requests = c.fakeAPI(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, err := w.Write([]byte(`{"status":"OK"}`))
				Expect(err).NotTo(HaveOccurred())
			})
		})

		It("reports the status the tenant gave", func() {
			Expect(c.run("ping")).To(Equal(exitcode.OK))

			Expect(c.out()).To(ContainSubstring("STATUS"))
			Expect(c.out()).To(ContainSubstring("OK"))
		})

		It("calls GET /api/status and nothing else", func() {
			Expect(c.run("ping")).To(Equal(exitcode.OK))

			Expect(*requests).To(HaveLen(1))
			Expect((*requests)[0].Method).To(Equal(http.MethodGet))
			Expect((*requests)[0].URL.Path).To(Equal("/api/status"))
		})

		It("sends no credentials: /api/status is the one endpoint that needs none", func() {
			// A ping that fails because the *password* is wrong is a diagnostic that
			// misdiagnoses. It tests the base URL and the network, and nothing else.
			Expect(c.run("ping")).To(Equal(exitcode.OK))

			Expect((*requests)[0].Header).NotTo(HaveKey("Authorization"))
		})

		It("emits valid JSON on stdout and nothing at all on stderr with -o json", func() {
			Expect(c.run("ping", "-o", "json")).To(Equal(exitcode.OK))

			var view struct {
				BaseURL string `json:"baseUrl"`
				Status  string `json:"status"`
				Latency string `json:"latency"`
			}
			Expect(json.Unmarshal([]byte(c.out()), &view)).To(Succeed())
			Expect(view.Status).To(Equal("OK"))
			Expect(view.BaseURL).NotTo(BeEmpty())
			Expect(c.errOut()).To(BeEmpty())
		})
	})

	When("the tenant is broken", func() {
		BeforeEach(func() {
			c.fakeAPI(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadGateway)
				_, err := w.Write([]byte(`<html>502 Bad Gateway</html>`))
				Expect(err).NotTo(HaveOccurred())
			})
		})

		It("exits 9 (upstream unavailable) and says what came back", func() {
			code := c.run("ping")

			Expect(code).To(Equal(exitcode.Unavailable))
			Expect(c.errOut()).To(ContainSubstring("502"))
			Expect(c.out()).To(BeEmpty())
		})
	})

	When("no project is configured", func() {
		It("exits 3 and names the command that fixes it", func() {
			code := c.run("ping")

			Expect(code).To(Equal(exitcode.Config))
			Expect(c.errOut()).To(ContainSubstring("no active project"))
			Expect(c.errOut()).To(ContainSubstring("fft project add"))
		})
	})
})
