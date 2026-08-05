package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/config"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/secrets"
)

// noKeyringStore is a machine with no keychain on it: every write reports the
// sentinel, which is what the real keyring store does under WSL.
type noKeyringStore struct {
	secrets.Store
}

func (noKeyringStore) Set(key, _ string) error {
	return fmt.Errorf("store %q: %w", key, secrets.ErrKeyringUnavailable)
}

var _ = Describe("the advice given when there is no keychain", func() {
	// keyringHint is asserted directly rather than through the environment,
	// because hermeticEnv does not clear WSL_* — a developer running this suite
	// inside WSL would otherwise get the other branch and a green build for the
	// wrong reason.
	It("names the flag, the setting, and what the file costs", func() {
		hint := keyringHint(false, "/home/jane/.config/fft/config.yaml")

		Expect(hint).To(ContainSubstring("--no-keyring"))
		Expect(hint).To(ContainSubstring("FFT_NO_KEYRING=1"))
		Expect(hint).To(ContainSubstring(`"noKeyring: true"`))
		Expect(hint).To(ContainSubstring("cleartext"))
	})

	It("says why there is no keychain, when the machine is WSL", func() {
		Expect(keyringHint(true, "/tmp/config.yaml")).To(ContainSubstring("WSL"))
		Expect(keyringHint(false, "/tmp/config.yaml")).NotTo(ContainSubstring("WSL"))
	})

	// A hint that names a file fft does not read is worse than no hint: the user
	// edits it, nothing changes, and the setting looks broken.
	It("names the config file fft would actually read, not a hardcoded path", func() {
		dir := GinkgoT().TempDir()
		GinkgoT().Setenv("XDG_CONFIG_HOME", dir)

		Expect(keyringHint(false, hintConfigPath())).To(ContainSubstring(dir))
	})
})

var _ = Describe("report", func() {
	It("turns an unreachable keychain into exit 3 and a way out", func() {
		var stderr bytes.Buffer

		code := report(&stderr, fmt.Errorf("store the password: %w", secrets.ErrKeyringUnavailable))

		// Not exit 4: nobody refused our credentials, and re-authenticating cannot
		// help. This is a fact about the machine, which is what exit 3 means.
		Expect(code).To(Equal(exitcode.Config))
		Expect(stderr.String()).To(ContainSubstring("no OS keychain is available"))
		Expect(stderr.String()).To(ContainSubstring("--no-keyring"))
	})

	It("leaves every other error exactly as it was", func() {
		var stderr bytes.Buffer

		code := report(&stderr, errors.New("something else went wrong"))

		Expect(code).To(Equal(exitcode.General))
		Expect(stderr.String()).To(ContainSubstring("something else went wrong"))
		Expect(stderr.String()).NotTo(ContainSubstring("--no-keyring"))
	})

	It("says nothing at all when nothing went wrong", func() {
		var stderr bytes.Buffer

		Expect(report(&stderr, nil)).To(Equal(exitcode.OK))
		Expect(stderr.String()).To(BeEmpty())
	})
})

var _ = Describe("choosing the credential store", func() {
	var c *cli

	// seedConfig writes a config file with the given settings block and no
	// projects, which is enough to exercise the store choice.
	seedConfig := func(settings string) {
		Expect(os.WriteFile(c.configPath, []byte("version: 2\nsettings:\n"+settings), 0o600)).To(Succeed())
	}

	BeforeEach(func() {
		c = newCLI()
		// Left unset so the spec exercises fft's own choice rather than one handed
		// to it.
		c.deps.Secrets = nil
		Expect(os.MkdirAll(filepath.Dir(c.configPath), 0o700)).To(Succeed())
	})

	It("uses the keychain by default", func() {
		seedConfig("    output: table\n")

		Expect(c.run("project", "list")).To(Equal(exitcode.OK))

		Expect(c.deps.Secrets.Kind()).To(Equal("keyring"))
	})

	It("uses the file store when the config says so, so nobody has to keep typing the flag", func() {
		seedConfig("    output: table\n    noKeyring: true\n")

		Expect(c.run("project", "list")).To(Equal(exitcode.OK))

		Expect(c.deps.Secrets.Kind()).To(Equal("file"))
	})

	DescribeTable("but the flag and the variable still win over the file",
		func(setup func(), args ...string) {
			seedConfig("    output: table\n    noKeyring: true\n")
			setup()

			Expect(c.run(append([]string{"project", "list"}, args...)...)).To(Equal(exitcode.OK))

			Expect(c.deps.Secrets.Kind()).To(Equal("keyring"))
		},
		Entry("--no-keyring=false", func() {}, "--no-keyring=false"),
		Entry("FFT_NO_KEYRING=0", func() { c.setenv("FFT_NO_KEYRING", "0") }),
	)

	It("is overridden by headless mode, which touches neither", func() {
		seedConfig("    output: table\n    noKeyring: true\n")
		c.headless()

		Expect(c.run("project", "current")).To(Equal(exitcode.OK))

		Expect(c.deps.Secrets.Kind()).To(Equal("env"))
	})
})

var _ = Describe("fft project add on a machine with no keychain", func() {
	var c *cli

	add := func(extra ...string) int {
		return c.run(append([]string{"project", "add", "wsl",
			"--base-url", "https://acme.api.fulfillmenttools.com",
			"--api-key", "AIzaSyExample",
			"--project-id", "acme",
			"--env", "prd",
			"--username", "bot"}, extra...)...)
	}

	BeforeEach(func() {
		c = newCLI()
		c.deps.Secrets = noKeyringStore{secrets.NewMem()}
	})

	When("there is no terminal to ask on", func() {
		It("refuses with exit 3 and the way out, having written nothing", func() {
			c.stdin.WriteString("s3cret")

			Expect(add("--password-stdin")).To(Equal(exitcode.Config))

			Expect(c.errOut()).To(ContainSubstring("no OS keychain is available"))
			Expect(c.errOut()).To(ContainSubstring("--no-keyring"))
			// A project in the config file with no credential behind it is a project
			// every later command fails on.
			Expect(c.configPath).NotTo(BeAnExistingFile())
		})

	})

	// --yes has to be refused where it could actually have answered: on a terminal.
	// Asserting it alongside --password-stdin would prove only that stdin was
	// already spent, and would stay green if someone wired deps.AssumeYes into
	// offerFileStore "for consistency" with the other confirmations.
	It("is not answered by --yes on a terminal, which is consent to the questions a command was always going to ask", func() {
		c.answer("s3cret", "n")

		// Storing a password in cleartext on the strength of a blanket -y is exactly
		// the accident the explicit opt-in exists to prevent.
		Expect(add("--yes")).To(Equal(exitcode.Config))
		Expect(c.configPath).NotTo(BeAnExistingFile())
	})

	When("the user is asked and says yes", func() {
		BeforeEach(func() {
			c.answer("s3cret", "y")
			Expect(add()).To(Equal(exitcode.OK))
		})

		It("asks before storing anything in cleartext", func() {
			Expect(c.errOut()).To(ContainSubstring("Store credentials in a 0600 file"))
		})

		It("does not claim a blast radius it does not have, on the first project", func() {
			Expect(c.errOut()).NotTo(ContainSubstring("machine-wide"))
		})

		It("remembers the answer, so the next run does not ask again", func() {
			cfg, err := config.NewStore(c.configPath).Load()

			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.Settings.NoKeyring).To(BeTrue())
		})

		It("puts the credentials in the file store the setting now points at", func() {
			Expect(c.deps.Secrets.Kind()).To(Equal("file"))
			Expect(secrets.Has(c.deps.Secrets, "wsl")).To(BeTrue())
		})
	})

	// settings.noKeyring is machine-wide, so a yes while adding one project moves
	// every project's credentials. A user who is not told that finds `pre` and
	// `prd` reporting "missing" the next time they look, with nothing pointing at
	// the answer they gave while adding something else.
	When("other projects are already configured", func() {
		It("says the answer moves them too", func() {
			Expect(os.MkdirAll(filepath.Dir(c.configPath), 0o700)).To(Succeed())
			Expect(os.WriteFile(c.configPath, []byte(`version: 2
activeProject: prd
projects:
    - name: prd
      baseUrl: https://acme.api.fulfillmenttools.com
      email: bot@ocff-acme-prd.com
    - name: pre
      baseUrl: https://pre.api.fulfillmenttools.com
      email: bot@ocff-acme-pre.com
settings:
    output: table
    updateCheck: false
`), 0o600)).To(Succeed())
			c.answer("s3cret", "n")

			Expect(add()).To(Equal(exitcode.Config))

			Expect(c.errOut()).To(ContainSubstring("machine-wide"))
			Expect(c.errOut()).To(ContainSubstring("2 projects already configured"))
		})
	})

	When("the user is asked and says no", func() {
		It("stops, with the same error it would have given without a terminal", func() {
			c.answer("s3cret", "n")

			Expect(add()).To(Equal(exitcode.Config))

			Expect(c.errOut()).To(ContainSubstring("no OS keychain is available"))
			Expect(c.configPath).NotTo(BeAnExistingFile())
		})
	})
})
