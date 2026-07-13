package config_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/config"
)

var _ = Describe("NormalizeBaseURL", func() {
	DescribeTable("accepting a base URL",
		func(raw, want string) {
			got, err := config.NormalizeBaseURL(raw)

			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(want))
		},
		Entry("keeps a well-formed https URL as it is",
			"https://acme.api.fulfillmenttools.com", "https://acme.api.fulfillmenttools.com"),
		Entry("assumes https for a bare host, because that is what the user meant",
			"acme.api.fulfillmenttools.com", "https://acme.api.fulfillmenttools.com"),
		Entry("drops a trailing slash, so joining a path never doubles it",
			"https://acme.api.fulfillmenttools.com/", "https://acme.api.fulfillmenttools.com"),
		Entry("trims surrounding whitespace from a pasted value",
			"  https://acme.api.fulfillmenttools.com  ", "https://acme.api.fulfillmenttools.com"),
		Entry("allows http on localhost, where a mock server legitimately lives",
			"http://localhost:8080", "http://localhost:8080"),
		Entry("allows http on the loopback address",
			"http://127.0.0.1:8080", "http://127.0.0.1:8080"),
	)

	DescribeTable("rejecting a base URL",
		func(raw string, wantMessage string) {
			_, err := config.NormalizeBaseURL(raw)

			Expect(err).To(MatchError(ContainSubstring(wantMessage)))
		},
		Entry("refuses plain http to a real host, which would leak the bearer token",
			"http://acme.api.fulfillmenttools.com", "in the clear"),
		Entry("refuses a scheme that is not http or https",
			"ftp://acme.api.fulfillmenttools.com", "want https"),
		Entry("refuses an empty value", "   ", "empty"),
		Entry("refuses a URL carrying a query string, which is always a paste accident",
			"https://acme.api.fulfillmenttools.com?key=AIza", "query or fragment"),
	)
})
