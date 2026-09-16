package main

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/config"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/tui"
)

// The Projects screen in internal/tui builds these command lines and decodes what
// they print. It cannot import this package, so this is where the two halves are
// checked against each other: each invocation below is the one the screen sends,
// and each key asserted on is one the screen reads.
var _ = Describe("the commands behind the TUI's Projects screen", func() {
	var (
		c *cli
		r *cliRunner
	)

	BeforeEach(func() {
		c = newCLI()
		r = c.newRunner()
	})

	run := func(inv tui.Invocation) tui.Result {
		GinkgoHelper()
		id := start(r, inv)
		return awaitDone(r, id)[id]
	}

	succeeds := func(inv tui.Invocation) map[string]any {
		GinkgoHelper()
		res := run(inv)
		Expect(res.ExitCode).To(Equal(exitcode.OK), "stderr: %s", res.Stderr)
		var doc map[string]any
		Expect(json.Unmarshal(res.Stdout, &doc)).To(Succeed(), "stdout: %s", res.Stdout)
		return doc
	}

	// add is the form's invocation: both secrets on stdin, neither in the arguments.
	add := func(name string) tui.Invocation {
		return tui.Invocation{
			Args: []string{"project", "add", name,
				"--base-url", "https://" + name + ".example.com",
				"--username", "bot", "--project-id", "acme", "--env", "pre",
				"--api-key-stdin", "--password-stdin"},
			Stdin:     []byte("AIzaSyFromTheForm\npassword from the form"),
			Exclusive: true,
		}
	}

	It("adds a project from the form's stdin, and says whether it became the active one", func() {
		doc := succeeds(add("qa"))

		Expect(doc).To(HaveKeyWithValue("name", "qa"))
		Expect(doc).To(HaveKeyWithValue("active", true))
		Expect(c.secrets.Snapshot()).To(Equal(map[string]string{
			"fft:qa:apiKey":   "AIzaSyFromTheForm",
			"fft:qa:password": "password from the form",
		}))

		Expect(succeeds(add("qa2"))).To(HaveKeyWithValue("active", false))
	})

	When("two projects are configured", func() {
		BeforeEach(func() {
			succeeds(add("qa"))
			succeeds(add("prod"))
		})

		It("lists them with the fields the table and the badge read", func() {
			res := run(tui.Invocation{Args: []string{"project", "list"}})
			Expect(res.ExitCode).To(Equal(exitcode.OK), "stderr: %s", res.Stderr)

			var rows []map[string]any
			Expect(json.Unmarshal(res.Stdout, &rows)).To(Succeed())
			Expect(rows).To(HaveLen(2))
			for _, key := range []string{"name", "active", "baseUrl", "email", "credential", "readOnly"} {
				Expect(rows[0]).To(HaveKey(key))
			}
		})

		It("reports the selected project's credential state with the fields the status bar reads", func() {
			r.SetProject("prod")
			doc := succeeds(tui.Invocation{Args: []string{"auth", "status"}})

			Expect(doc).To(HaveKeyWithValue("project", "prod"))
			for _, key := range []string{"store", "signIn", "token", "expired"} {
				Expect(doc).To(HaveKey(key))
			}
		})

		It("switches, protects, re-arms and removes a project without a terminal to ask on", func() {
			for _, args := range [][]string{
				{"project", "use", "prod"},
				{"project", "read-only", "prod"},
				{"project", "read-only", "prod", "--off", "--yes"},
				{"project", "remove", "qa", "--yes"},
			} {
				res := run(tui.Invocation{Args: args, Exclusive: true})
				Expect(res.ExitCode).To(Equal(exitcode.OK), "%v: %s", args, res.Stderr)
			}

			cfg, err := config.NewStore(c.configPath).Load()
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.ActiveProject).To(Equal("prod"))
			Expect(cfg.Projects).To(HaveLen(1))
			Expect(cfg.Projects[0].ReadOnly).To(BeFalse())
		})
	})

	When("fft is running from the environment", func() {
		BeforeEach(func() {
			c.headless()
			c.deps.Secrets = nil
		})

		It("lists the environment's project as ephemeral, which is how the screen knows", func() {
			res := run(tui.Invocation{Args: []string{"project", "list"}})
			Expect(res.ExitCode).To(Equal(exitcode.OK), "stderr: %s", res.Stderr)

			var rows []map[string]any
			Expect(json.Unmarshal(res.Stdout, &rows)).To(Succeed())
			Expect(rows).To(ConsistOf(HaveKeyWithValue("ephemeral", true)))
		})

		It("refuses the changes the screen refuses before sending them", func() {
			res := run(tui.Invocation{Args: []string{"project", "use", "env"}, Exclusive: true})

			Expect(res.ExitCode).To(Equal(exitcode.Config))
			Expect(string(res.Stderr)).To(ContainSubstring("running from the environment"))
		})
	})
})
