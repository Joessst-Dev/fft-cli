package main

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/config"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

// A GitHub Linux runner has no Secret Service, so go-keyring cannot work there
// at all. Synthesizing an ephemeral project from the environment — and touching
// neither the config file nor the keychain while doing it — is therefore not a
// convenience, it is the only way fft runs in CI.
var _ = Describe("running headless from the environment", func() {
	var c *cli

	BeforeEach(func() {
		c = newCLI()

		// The credential store is left unset, so that the spec exercises fft's own
		// choice of store rather than one handed to it.
		c.deps.Secrets = nil

		c.headless()
	})

	It("synthesizes an ephemeral project with no config file present", func() {
		Expect(c.run("project", "current")).To(Equal(exitcode.OK))

		Expect(c.out()).To(ContainSubstring(config.EphemeralName))
		Expect(c.out()).To(ContainSubstring("https://ci.api.fulfillmenttools.com"))
	})

	It("never creates the config file", func() {
		Expect(c.run("project", "current")).To(Equal(exitcode.OK))
		Expect(c.run("project", "list")).To(Equal(exitcode.OK))

		Expect(c.configPath).NotTo(BeAnExistingFile())
	})

	It("reads its credentials from the environment rather than the keychain", func() {
		Expect(c.run("project", "current")).To(Equal(exitcode.OK))

		Expect(c.deps.Secrets.Kind()).To(Equal("env"))
		Expect(c.out()).To(ContainSubstring("env"))
	})

	It("reports the ephemeral project as the active one", func() {
		Expect(c.run("project", "list", "-o", "json")).To(Equal(exitcode.OK))

		var views []struct {
			Name       string `json:"name"`
			Active     bool   `json:"active"`
			Credential string `json:"credential"`
			Ephemeral  bool   `json:"ephemeral"`
		}
		Expect(json.Unmarshal([]byte(c.out()), &views)).To(Succeed())
		Expect(views).To(HaveLen(1))
		Expect(views[0].Name).To(Equal(config.EphemeralName))
		Expect(views[0].Active).To(BeTrue())
		Expect(views[0].Ephemeral).To(BeTrue())
		Expect(views[0].Credential).To(Equal("env"))
	})

	It("works with an id token instead of a password, for a job that signs in once", func() {
		c.setenv(config.EnvPassword, "")
		c.setenv(config.EnvIDToken, "eyJhbGciOi...")

		Expect(c.run("project", "current")).To(Equal(exitcode.OK))

		Expect(c.out()).To(ContainSubstring(config.EphemeralName))
	})

	DescribeTable("refusing to manage the config file while the environment is in charge",
		// Writing a config file that the very next command would ignore — because
		// the environment still wins — is worse than refusing: the user would think
		// they had configured something, and they would not have.
		func(args ...string) {
			code := c.run(args...)

			Expect(code).To(Equal(exitcode.Config))
			Expect(c.errOut()).To(ContainSubstring("running from the environment"))
			Expect(c.configPath).NotTo(BeAnExistingFile())
		},
		Entry("project add", "project", "add", "staging",
			"--base-url", "https://acme.api.fulfillmenttools.com",
			"--api-key", "AIza", "--email", "a@b.com", "--password-stdin"),
		Entry("project use", "project", "use", "staging"),
		Entry("project remove", "project", "remove", "staging", "--yes"),
	)

	When("only some of the FFT_* variables are set", func() {
		It("ignores them entirely, rather than running against a half-configured tenant", func() {
			c.setenv(config.EnvFirebaseAPIKey, "")

			// With no ephemeral project and no config file, there is nothing to act
			// on — which is exactly the loud failure a misconfigured CI job needs.
			code := c.run("project", "current")

			Expect(code).To(Equal(exitcode.Config))
			Expect(c.errOut()).To(ContainSubstring("no active project"))
		})
	})
})
