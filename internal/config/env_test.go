package config_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/config"
)

var _ = Describe("FromEnv", func() {
	// A GitHub Linux runner has no Secret Service, so go-keyring cannot work
	// there. The ephemeral project synthesized from these variables is the only
	// way fft runs in CI at all — which is why "a partial set is ignored" is a
	// spec and not a detail.
	var env map[string]string

	lookup := func(name string) (string, bool) {
		v, ok := env[name]
		return v, ok
	}

	BeforeEach(func() {
		env = map[string]string{
			config.EnvBaseURL:        "https://acme.api.fulfillmenttools.com",
			config.EnvFirebaseAPIKey: "AIzaSyExample",
			config.EnvEmail:          "bot@ocff-acme-staging.com",
			config.EnvPassword:       "s3cret",
		}
	})

	When("the base URL, API key, email and a password are all set", func() {
		It("synthesizes an ephemeral project", func() {
			p, ok := config.FromEnv(lookup)

			Expect(ok).To(BeTrue())
			Expect(p.Name).To(Equal(config.EphemeralName))
			Expect(p.Ephemeral).To(BeTrue())
			Expect(p.BaseURL).To(Equal("https://acme.api.fulfillmenttools.com"))
			Expect(p.FirebaseAPIKey).To(Equal("AIzaSyExample"))
			Expect(p.Email).To(Equal("bot@ocff-acme-staging.com"))
		})

		It("carries the descriptive fields through when they are set", func() {
			env[config.EnvProjectID] = "acme"
			env[config.EnvEnvironment] = "staging"
			env[config.EnvTenant] = "acme-tenant"
			env[config.EnvUsername] = "bot"

			p, ok := config.FromEnv(lookup)

			Expect(ok).To(BeTrue())
			Expect(p.ProjectID).To(Equal("acme"))
			Expect(p.Environment).To(Equal("staging"))
			Expect(p.Tenant).To(Equal("acme-tenant"))
			Expect(p.Username).To(Equal("bot"))
		})
	})

	When("an id token is supplied instead of a password", func() {
		It("still synthesizes a project, because a token is a credential too", func() {
			delete(env, config.EnvPassword)
			env[config.EnvIDToken] = "eyJhbGciOi..."

			p, ok := config.FromEnv(lookup)

			Expect(ok).To(BeTrue())
			Expect(p.Ephemeral).To(BeTrue())
		})
	})

	DescribeTable("an incomplete set is ignored entirely, rather than half-honoured",
		// Falling back to the config file when one variable is missing is how a CI
		// job ends up running against the wrong tenant. Better to have no project
		// and fail loudly.
		func(missing string) {
			delete(env, missing)

			_, ok := config.FromEnv(lookup)

			Expect(ok).To(BeFalse())
		},
		Entry("without a base URL", config.EnvBaseURL),
		Entry("without a Firebase API key", config.EnvFirebaseAPIKey),
		Entry("without an email", config.EnvEmail),
		Entry("without any credential", config.EnvPassword),
	)

	When("a required variable is set but empty", func() {
		It("is treated as absent", func() {
			env[config.EnvEmail] = "   "

			_, ok := config.FromEnv(lookup)

			Expect(ok).To(BeFalse())
		})
	})

	When("no email is given but the username, project id and environment are", func() {
		// A CI job should have to configure the same four things a human types into
		// `fft project add`, rather than having to know how fulfillmenttools spells
		// the synthetic address. It is still only a candidate — the sign-in settles
		// it — but it is the one fft would have guessed anyway.
		BeforeEach(func() {
			delete(env, config.EnvEmail)
			env[config.EnvUsername] = "bot"
			env[config.EnvProjectID] = "acme"
		})

		It("builds the email from them", func() {
			env[config.EnvEnvironment] = "staging"

			p, ok := config.FromEnv(lookup)

			Expect(ok).To(BeTrue())
			Expect(p.Email).To(Equal("bot@ocff-acme-staging.com"))
		})

		It("accepts FFT_ENV as the alias of FFT_ENVIRONMENT, matching the --env flag", func() {
			env[config.EnvEnv] = "staging"

			p, ok := config.FromEnv(lookup)

			Expect(ok).To(BeTrue())
			Expect(p.Environment).To(Equal("staging"))
			Expect(p.Email).To(Equal("bot@ocff-acme-staging.com"))
		})

		It("prefers the explicit FFT_ENVIRONMENT when both are set", func() {
			env[config.EnvEnvironment] = "prod"
			env[config.EnvEnv] = "staging"

			p, ok := config.FromEnv(lookup)

			Expect(ok).To(BeTrue())
			Expect(p.Environment).To(Equal("prod"))
		})

		It("still refuses when the environment is missing, rather than inventing an address", func() {
			_, ok := config.FromEnv(lookup)

			Expect(ok).To(BeFalse())
		})
	})

	When("an email is given as well as a username", func() {
		It("uses the email verbatim: some tenants sign in with a corporate address", func() {
			env[config.EnvEmail] = "someone@acme.com"
			env[config.EnvUsername] = "bot"
			env[config.EnvProjectID] = "acme"
			env[config.EnvEnv] = "staging"

			p, ok := config.FromEnv(lookup)

			Expect(ok).To(BeTrue())
			Expect(p.Email).To(Equal("someone@acme.com"))
		})
	})

	When("nothing is set at all", func() {
		It("reports no ephemeral project", func() {
			env = nil

			_, ok := config.FromEnv(lookup)

			Expect(ok).To(BeFalse())
		})
	})
})
