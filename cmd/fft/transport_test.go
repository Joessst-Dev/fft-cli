package main

import (
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/config"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

var _ = Describe("a request that never gets an answer", func() {
	var c *cli

	BeforeEach(func() {
		c = newCLI()
	})

	// Scripts back off on 9 and give up on 1. A tenant that cannot be reached is the
	// case 9 exists for, and it used to exit 1.
	When("nothing is listening at the tenant's address", func() {
		BeforeEach(func() {
			c.headless()
			// A server that has been closed leaves a port that refuses connections.
			gone := httptest.NewServer(http.NotFoundHandler())
			c.setenv(config.EnvBaseURL, gone.URL)
			gone.Close()
		})

		It("exits 9 and still names the URL that could not be reached", func() {
			Expect(c.run("facility", "list")).To(Equal(exitcode.Unavailable))

			Expect(c.errOut()).To(ContainSubstring("/api/facilities"))
			Expect(c.out()).To(BeEmpty())
		})
	})

	When("the tenant does not answer within --timeout", func() {
		BeforeEach(func() {
			c.fakeTenant(func(_ http.ResponseWriter, r *http.Request, _ []byte) {
				<-r.Context().Done()
			})
		})

		It("exits 9, because a slower answer may well come on the next try", func() {
			Expect(c.run("facility", "list", "--timeout", "50ms")).To(Equal(exitcode.Unavailable))

			Expect(c.out()).To(BeEmpty())
		})
	})
})
