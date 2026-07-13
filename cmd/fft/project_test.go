package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/config"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/secrets"
	"github.com/Joessst-Dev/fft-cli/internal/testsupport"
)

// addStaging is the canonical non-interactive `project add`: every value as a
// flag, the password piped in. It is what a provisioning script would run.
func addStaging(c *cli, password string) int {
	c.stdin.WriteString(password)
	return c.run("project", "add", "staging",
		"--base-url", "https://acme.api.fulfillmenttools.com",
		"--api-key", "AIzaSyExample",
		"--project-id", "acme",
		"--env", "staging",
		"--username", "bot",
		"--password-stdin")
}

var _ = Describe("fft project add", func() {
	var c *cli

	BeforeEach(func() {
		c = newCLI()
	})

	When("every value is given as a flag and the password is piped in", func() {
		BeforeEach(func() {
			Expect(addStaging(c, "s3cret")).To(Equal(exitcode.OK))
		})

		It("persists the project to a config file only its owner can read", func() {
			testsupport.ExpectOwnerOnlyFile(c.configPath)
		})

		It("stores the base URL verbatim, deriving nothing from the project id", func() {
			cfg, err := config.NewStore(c.configPath).Load()

			Expect(err).NotTo(HaveOccurred())
			project, ok := cfg.Find("staging")
			Expect(ok).To(BeTrue())
			Expect(project.BaseURL).To(Equal("https://acme.api.fulfillmenttools.com"))
		})

		It("builds the email from the username, the project id and the environment", func() {
			cfg, err := config.NewStore(c.configPath).Load()

			Expect(err).NotTo(HaveOccurred())
			project, _ := cfg.Find("staging")
			Expect(project.Email).To(Equal("bot@ocff-acme-staging.com"))
		})

		It("stores the password in the keychain under its own key, and nothing else", func() {
			Expect(c.secrets.Snapshot()).To(Equal(map[string]string{
				"fft:staging:password": "s3cret",
			}))
		})

		It("keeps the password out of the config file", func() {
			data, err := os.ReadFile(c.configPath)

			Expect(err).NotTo(HaveOccurred())
			Expect(string(data)).NotTo(ContainSubstring("s3cret"))
		})

		It("keeps the Firebase Web API key in the config file, since it is not a credential", func() {
			data, err := os.ReadFile(c.configPath)

			Expect(err).NotTo(HaveOccurred())
			Expect(string(data)).To(ContainSubstring("firebaseApiKey: AIzaSyExample"))
		})

		It("makes the first project the active one", func() {
			cfg, err := config.NewStore(c.configPath).Load()

			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.ActiveProject).To(Equal("staging"))
			Expect(c.errOut()).To(ContainSubstring(`Project "staging" added and is now active.`))
		})
	})

	When("the same project is added twice", func() {
		BeforeEach(func() {
			Expect(addStaging(c, "s3cret")).To(Equal(exitcode.OK))
		})

		It("refuses, rather than silently replacing what is already there", func() {
			code := addStaging(c, "different")

			Expect(code).To(Equal(exitcode.Config))
			Expect(c.errOut()).To(ContainSubstring("already configured"))
		})

		It("overwrites it when --force is given", func() {
			c.stdin.WriteString("rotated")
			code := c.run("project", "add", "staging", "--force",
				"--base-url", "https://acme.api.fulfillmenttools.com",
				"--api-key", "AIzaSyExample",
				"--email", "someone@acme.com",
				"--password-stdin")

			Expect(code).To(Equal(exitcode.OK))
			Expect(c.secrets.Snapshot()).To(HaveKeyWithValue("fft:staging:password", "rotated"))
		})
	})

	When("--email is given", func() {
		It("uses it verbatim rather than deriving one", func() {
			c.stdin.WriteString("s3cret")
			code := c.run("project", "add", "staging",
				"--base-url", "https://acme.api.fulfillmenttools.com",
				"--api-key", "AIzaSyExample",
				"--email", "someone@acme.com",
				"--password-stdin")

			Expect(code).To(Equal(exitcode.OK))

			cfg, err := config.NewStore(c.configPath).Load()
			Expect(err).NotTo(HaveOccurred())
			project, _ := cfg.Find("staging")
			Expect(project.Email).To(Equal("someone@acme.com"))
		})
	})

	When("stdin is not a terminal and a required flag is missing", func() {
		It("exits 2 and names every flag it still needs, rather than one at a time", func() {
			code := c.run("project", "add", "staging")

			Expect(code).To(Equal(exitcode.Usage))
			Expect(c.errOut()).To(ContainSubstring("--base-url"))
			Expect(c.errOut()).To(ContainSubstring("--api-key"))
			Expect(c.errOut()).To(ContainSubstring("--email or --username"))
		})

		It("writes nothing to the config file", func() {
			Expect(c.run("project", "add", "staging")).To(Equal(exitcode.Usage))

			Expect(c.configPath).NotTo(BeAnExistingFile())
		})
	})

	When("stdin is not a terminal and --password-stdin was not given", func() {
		It("exits 2 and says to pipe the password in", func() {
			code := c.run("project", "add", "staging",
				"--base-url", "https://acme.api.fulfillmenttools.com",
				"--api-key", "AIzaSyExample",
				"--email", "someone@acme.com")

			Expect(code).To(Equal(exitcode.Usage))
			Expect(c.errOut()).To(ContainSubstring("--password-stdin"))
		})
	})

	When("--password-stdin is given but stdin is empty", func() {
		It("exits 2 rather than storing an empty password", func() {
			code := c.run("project", "add", "staging",
				"--base-url", "https://acme.api.fulfillmenttools.com",
				"--api-key", "AIzaSyExample",
				"--email", "someone@acme.com",
				"--password-stdin")

			Expect(code).To(Equal(exitcode.Usage))
			Expect(c.secrets.Snapshot()).To(BeEmpty())
		})
	})

	When("the password is piped in with a trailing newline", func() {
		It("strips the newline, which the user did not mean to be part of the password", func() {
			c.stdin.WriteString("s3cret\n")
			code := c.run("project", "add", "staging",
				"--base-url", "https://acme.api.fulfillmenttools.com",
				"--api-key", "AIzaSyExample",
				"--email", "someone@acme.com",
				"--password-stdin")

			Expect(code).To(Equal(exitcode.OK))
			Expect(c.secrets.Snapshot()).To(HaveKeyWithValue("fft:staging:password", "s3cret"))
		})
	})

	When("the base URL is plain http to a real host", func() {
		It("exits 2, because fft would send the bearer token in the clear", func() {
			c.stdin.WriteString("s3cret")
			code := c.run("project", "add", "staging",
				"--base-url", "http://acme.api.fulfillmenttools.com",
				"--api-key", "AIzaSyExample",
				"--email", "someone@acme.com",
				"--password-stdin")

			Expect(code).To(Equal(exitcode.Usage))
			Expect(c.errOut()).To(ContainSubstring("in the clear"))
			Expect(c.secrets.Snapshot()).To(BeEmpty())
		})
	})

	When("credential verification is wired in", func() {
		// M3 replaces the nil VerifyFunc with the real Firebase sign-in. These two
		// specs pin the contract it must honour: nothing is persisted unless
		// verification passes, and the email that actually worked is the one stored.
		It("persists nothing when the credentials do not authenticate", func() {
			c.deps.Verify = func(_ context.Context, _ config.Project, _ string, _ io.Writer) (string, error) {
				return "", errors.New("wrong password")
			}

			code := addStaging(c, "s3cret")

			Expect(code).To(Equal(exitcode.General))
			Expect(c.configPath).NotTo(BeAnExistingFile())
			Expect(c.secrets.Snapshot()).To(BeEmpty())
		})

		It("stores the email that actually authenticated, not the one fft guessed", func() {
			c.deps.Verify = func(_ context.Context, _ config.Project, _ string, _ io.Writer) (string, error) {
				return "the-real-one@acme.com", nil
			}

			Expect(addStaging(c, "s3cret")).To(Equal(exitcode.OK))

			cfg, err := config.NewStore(c.configPath).Load()
			Expect(err).NotTo(HaveOccurred())
			project, _ := cfg.Find("staging")
			Expect(project.Email).To(Equal("the-real-one@acme.com"))
		})
	})

	When("there is no --password flag", func() {
		It("rejects one, so that a password can never land in the shell history", func() {
			code := c.run("project", "add", "staging", "--password", "s3cret")

			Expect(code).To(Equal(exitcode.Usage))
			Expect(c.errOut()).To(ContainSubstring("unknown flag"))
		})
	})
})

var _ = Describe("fft project list", func() {
	var c *cli

	BeforeEach(func() {
		c = newCLI()
	})

	When("no project is configured", func() {
		It("says so on stderr and leaves stdout empty, so a pipe gets nothing", func() {
			Expect(c.run("project", "list")).To(Equal(exitcode.OK))

			Expect(c.out()).To(BeEmpty())
			Expect(c.errOut()).To(ContainSubstring("No projects are configured"))
		})

		It("still emits a parseable empty array under -o json", func() {
			Expect(c.run("project", "list", "-o", "json")).To(Equal(exitcode.OK))

			var views []map[string]any
			Expect(json.Unmarshal([]byte(c.out()), &views)).To(Succeed())
			Expect(views).To(BeEmpty())
		})
	})

	When("projects are configured", func() {
		BeforeEach(func() {
			Expect(addStaging(c, "s3cret")).To(Equal(exitcode.OK))

			c.stdin.WriteString("prod-secret")
			Expect(c.run("project", "add", "prod",
				"--base-url", "https://prod.api.fulfillmenttools.com",
				"--api-key", "AIzaSyProd",
				"--email", "bot@acme.com",
				"--password-stdin")).To(Equal(exitcode.OK))
		})

		It("marks the active project with an asterisk", func() {
			Expect(c.run("project", "list")).To(Equal(exitcode.OK))

			Expect(c.out()).To(MatchRegexp(`\* staging`))
			Expect(c.out()).To(MatchRegexp(`  prod`))
		})

		It("shows the name, base URL, email and where the credential lives", func() {
			Expect(c.run("project", "list")).To(Equal(exitcode.OK))

			Expect(c.out()).To(ContainSubstring("NAME"))
			Expect(c.out()).To(ContainSubstring("BASE URL"))
			Expect(c.out()).To(ContainSubstring("EMAIL"))
			Expect(c.out()).To(ContainSubstring("CREDENTIAL"))
			Expect(c.out()).To(ContainSubstring("https://acme.api.fulfillmenttools.com"))
			Expect(c.out()).To(ContainSubstring("bot@ocff-acme-staging.com"))
			Expect(c.out()).To(ContainSubstring("memory"))
		})

		It("reports a project whose secrets have vanished as missing, not as usable", func() {
			Expect(secrets.DeleteAll(c.secrets, "prod")).To(Succeed())

			Expect(c.run("project", "list")).To(Equal(exitcode.OK))

			Expect(c.out()).To(ContainSubstring("missing"))
		})

		It("emits valid JSON on stdout under -o json, with a clean stderr", func() {
			Expect(c.run("project", "list", "-o", "json")).To(Equal(exitcode.OK))

			var views []struct {
				Name     string `json:"name"`
				Active   bool   `json:"active"`
				BaseURL  string `json:"baseUrl"`
				Password string `json:"password"`
			}
			Expect(json.Unmarshal([]byte(c.out()), &views)).To(Succeed())
			Expect(views).To(HaveLen(2))
			Expect(views[0].Name).To(Equal("staging"))
			Expect(views[0].Active).To(BeTrue())
			Expect(views[0].BaseURL).To(Equal("https://acme.api.fulfillmenttools.com"))
			Expect(c.errOut()).To(BeEmpty())
		})

		It("never puts a secret in the JSON, because the view has nowhere to hold one", func() {
			Expect(c.run("project", "list", "-o", "json")).To(Equal(exitcode.OK))

			Expect(c.out()).NotTo(ContainSubstring("s3cret"))
			Expect(c.out()).NotTo(ContainSubstring("prod-secret"))
		})

		It("renders YAML under -o yaml", func() {
			Expect(c.run("project", "list", "-o", "yaml")).To(Equal(exitcode.OK))

			Expect(c.out()).To(ContainSubstring("name: staging"))
			Expect(c.out()).To(ContainSubstring("baseUrl: https://acme.api.fulfillmenttools.com"))
		})
	})

	When("an unknown output format is asked for", func() {
		It("exits 2", func() {
			Expect(c.run("project", "list", "-o", "xml")).To(Equal(exitcode.Usage))

			Expect(c.errOut()).To(ContainSubstring("unknown output format"))
		})
	})
})

var _ = Describe("fft project use", func() {
	var c *cli

	BeforeEach(func() {
		c = newCLI()
		Expect(addStaging(c, "s3cret")).To(Equal(exitcode.OK))

		c.stdin.WriteString("prod-secret")
		Expect(c.run("project", "add", "prod",
			"--base-url", "https://prod.api.fulfillmenttools.com",
			"--api-key", "AIzaSyProd",
			"--email", "bot@acme.com",
			"--password-stdin")).To(Equal(exitcode.OK))
	})

	It("switches the active project", func() {
		Expect(c.run("project", "use", "prod")).To(Equal(exitcode.OK))

		cfg, err := config.NewStore(c.configPath).Load()
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.ActiveProject).To(Equal("prod"))
		Expect(c.errOut()).To(ContainSubstring(`Now using project "prod".`))
	})

	When("the project is not configured", func() {
		It("exits 3 and lists the projects that are", func() {
			code := c.run("project", "use", "nope")

			Expect(code).To(Equal(exitcode.Config))
			Expect(c.errOut()).To(ContainSubstring("project not found"))
			Expect(c.errOut()).To(ContainSubstring("staging"))
			Expect(c.errOut()).To(ContainSubstring("prod"))
		})
	})

	When("the project has lost its credentials", func() {
		It("switches anyway, but warns that nothing can sign in as it", func() {
			Expect(secrets.DeleteAll(c.secrets, "prod")).To(Succeed())

			Expect(c.run("project", "use", "prod")).To(Equal(exitcode.OK))

			Expect(c.errOut()).To(ContainSubstring("no credentials are stored"))
		})
	})

	When("no project is named", func() {
		It("exits 2", func() {
			Expect(c.run("project", "use")).To(Equal(exitcode.Usage))
		})
	})
})

var _ = Describe("fft project remove", func() {
	var c *cli

	BeforeEach(func() {
		c = newCLI()
		Expect(addStaging(c, "s3cret")).To(Equal(exitcode.OK))

		// Give the project the full set of secrets a signed-in project would have,
		// so that "remove deletes all of them" is actually being tested.
		for _, kind := range secrets.AllKinds() {
			Expect(c.secrets.Set(secrets.Key("staging", kind), "value-of-"+kind)).To(Succeed())
		}
	})

	When("--yes is given", func() {
		It("removes the project from the config file", func() {
			Expect(c.run("project", "remove", "staging", "--yes")).To(Equal(exitcode.OK))

			cfg, err := config.NewStore(c.configPath).Load()
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.Projects).To(BeEmpty())
			Expect(cfg.ActiveProject).To(BeEmpty())
		})

		It("deletes every one of its keychain entries, leaving nothing behind", func() {
			Expect(c.run("project", "remove", "staging", "--yes")).To(Equal(exitcode.OK))

			Expect(c.secrets.Snapshot()).To(BeEmpty())
		})

		It("leaves another project's secrets alone", func() {
			Expect(c.secrets.Set(secrets.Key("prod", secrets.KindPassword), "prod-secret")).To(Succeed())

			Expect(c.run("project", "remove", "staging", "--yes")).To(Equal(exitcode.OK))

			Expect(c.secrets.Snapshot()).To(Equal(map[string]string{"fft:prod:password": "prod-secret"}))
		})
	})

	When("-y is not given and stdin is not a terminal", func() {
		It("refuses rather than assuming yes, and destroys nothing", func() {
			code := c.run("project", "remove", "staging")

			Expect(code).To(Equal(exitcode.Usage))
			Expect(c.errOut()).To(ContainSubstring("--yes"))
			Expect(c.secrets.Snapshot()).To(HaveLen(4))

			cfg, err := config.NewStore(c.configPath).Load()
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.Projects).To(HaveLen(1))
		})
	})

	When("the project is not configured", func() {
		It("exits 3", func() {
			Expect(c.run("project", "remove", "nope", "--yes")).To(Equal(exitcode.Config))
		})
	})
})

var _ = Describe("fft project current", func() {
	var c *cli

	BeforeEach(func() {
		c = newCLI()
	})

	When("a project is active", func() {
		It("prints it", func() {
			Expect(addStaging(c, "s3cret")).To(Equal(exitcode.OK))

			Expect(c.run("project", "current")).To(Equal(exitcode.OK))

			Expect(c.out()).To(ContainSubstring("* staging"))
		})
	})

	When("--project names another project", func() {
		It("prints that one instead, without changing what is active", func() {
			Expect(addStaging(c, "s3cret")).To(Equal(exitcode.OK))
			c.stdin.WriteString("prod-secret")
			Expect(c.run("project", "add", "prod",
				"--base-url", "https://prod.api.fulfillmenttools.com",
				"--api-key", "AIzaSyProd",
				"--email", "bot@acme.com",
				"--password-stdin")).To(Equal(exitcode.OK))

			Expect(c.run("project", "current", "--project", "prod")).To(Equal(exitcode.OK))

			Expect(c.out()).To(ContainSubstring("prod"))

			cfg, err := config.NewStore(c.configPath).Load()
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.ActiveProject).To(Equal("staging"))
		})
	})

	When("--project names a project that is not configured", func() {
		It("exits 3", func() {
			Expect(addStaging(c, "s3cret")).To(Equal(exitcode.OK))

			Expect(c.run("project", "current", "--project", "nope")).To(Equal(exitcode.Config))
			Expect(c.errOut()).To(ContainSubstring("project not found"))
		})
	})

	When("nothing is configured at all", func() {
		It("exits 3 and tells the user to run 'fft project add'", func() {
			code := c.run("project", "current")

			Expect(code).To(Equal(exitcode.Config))
			Expect(c.errOut()).To(ContainSubstring("no active project"))
			Expect(c.errOut()).To(ContainSubstring("fft project add"))
		})
	})

	When("FFT_PROJECT names the project", func() {
		It("is honoured, since every global flag has an environment equivalent", func() {
			Expect(addStaging(c, "s3cret")).To(Equal(exitcode.OK))
			c.setenv("FFT_PROJECT", "nope")

			Expect(c.run("project", "current")).To(Equal(exitcode.Config))
			Expect(c.errOut()).To(ContainSubstring(`"nope"`))
		})
	})
})

var _ = Describe("read-only projects in the config file", func() {
	var c *cli

	BeforeEach(func() {
		c = newCLI()
	})

	Describe("fft project add --read-only", func() {
		It("persists the flag", func() {
			c.stdin.WriteString("s3cret")
			code := c.run("project", "add", "staging",
				"--base-url", "https://acme.api.fulfillmenttools.com",
				"--api-key", "AIzaSyExample",
				"--email", "bot@ocff-acme-staging.com",
				"--password-stdin", "--read-only")

			Expect(code).To(Equal(exitcode.OK))
			Expect(c.errOut()).To(ContainSubstring("read-only"))

			cfg, err := config.NewStore(c.configPath).Load()
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.Projects[0].ReadOnly).To(BeTrue())
		})

		// --force is for rotating a password. The day it also quietly disarms the
		// guardrail on prod is the day the guardrail is worth nothing.
		It("does not clear the flag when the project is overwritten with --force", func() {
			c.stdin.WriteString("s3cret")
			Expect(c.run("project", "add", "prod",
				"--base-url", "https://acme.api.fulfillmenttools.com",
				"--api-key", "AIzaSyExample",
				"--email", "bot@ocff-acme-prod.com",
				"--password-stdin", "--read-only")).To(Equal(exitcode.OK))

			c.stdin.WriteString("rotated")
			Expect(c.run("project", "add", "prod", "--force",
				"--base-url", "https://acme.api.fulfillmenttools.com",
				"--api-key", "AIzaSyExample",
				"--email", "bot@ocff-acme-prod.com",
				"--password-stdin")).To(Equal(exitcode.OK))

			cfg, err := config.NewStore(c.configPath).Load()
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.Projects[0].ReadOnly).To(BeTrue(), "--force disarmed the guardrail")
		})

		When("--read-only=false is said out loud", func() {
			readd := func(extra ...string) int {
				c.stdin.WriteString("s3cret")
				args := append([]string{"project", "add", "prod", "--force", "--read-only=false",
					"--base-url", "https://acme.api.fulfillmenttools.com",
					"--api-key", "AIzaSyExample",
					"--email", "bot@ocff-acme-prod.com",
					"--password-stdin"}, extra...)
				return c.run(args...)
			}

			BeforeEach(func() {
				c.stdin.WriteString("s3cret")
				Expect(c.run("project", "add", "prod",
					"--base-url", "https://acme.api.fulfillmenttools.com",
					"--api-key", "AIzaSyExample",
					"--email", "bot@ocff-acme-prod.com",
					"--password-stdin", "--read-only")).To(Equal(exitcode.OK))
			})

			// Otherwise this is the way around the confirmation on
			// `project read-only --off`: same irreversible step, no question asked.
			It("still asks first, exactly as 'project read-only --off' does", func() {
				Expect(readd()).To(Equal(exitcode.Usage))

				cfg, err := config.NewStore(c.configPath).Load()
				Expect(err).NotTo(HaveOccurred())
				Expect(cfg.Projects[0].ReadOnly).To(BeTrue())
			})

			It("clears the flag when it is confirmed", func() {
				Expect(readd("--yes")).To(Equal(exitcode.OK))

				cfg, err := config.NewStore(c.configPath).Load()
				Expect(err).NotTo(HaveOccurred())
				Expect(cfg.Projects[0].ReadOnly).To(BeFalse())
			})
		})
	})

	Describe("fft project read-only", func() {
		BeforeEach(func() {
			Expect(addStaging(c, "s3cret")).To(Equal(exitcode.OK))
		})

		readOnly := func() bool {
			cfg, err := config.NewStore(c.configPath).Load()
			Expect(err).NotTo(HaveOccurred())
			return cfg.Projects[0].ReadOnly
		}

		It("marks the project read-only", func() {
			Expect(c.run("project", "read-only", "staging")).To(Equal(exitcode.OK))

			Expect(readOnly()).To(BeTrue())
			Expect(c.errOut()).To(ContainSubstring("refuse every request"))
		})

		It("shows it in the table", func() {
			Expect(c.run("project", "read-only", "staging")).To(Equal(exitcode.OK))
			Expect(c.run("project", "list")).To(Equal(exitcode.OK))

			Expect(c.out()).To(ContainSubstring("ACCESS"))
			Expect(c.out()).To(ContainSubstring("read-only"))
		})

		It("shows it in the JSON, as false rather than absent when it is off", func() {
			Expect(c.run("project", "list", "-o", "json")).To(Equal(exitcode.OK))

			var views []map[string]any
			Expect(json.Unmarshal([]byte(c.out()), &views)).To(Succeed())
			Expect(views[0]).To(HaveKeyWithValue("readOnly", false))
		})

		It("is idempotent, and writes nothing when there is nothing to change", func() {
			Expect(c.run("project", "read-only", "staging")).To(Equal(exitcode.OK))
			Expect(c.run("project", "read-only", "staging")).To(Equal(exitcode.OK))

			Expect(c.errOut()).To(ContainSubstring("already read-only"))
			Expect(readOnly()).To(BeTrue())
		})

		When("switching it back off", func() {
			BeforeEach(func() {
				Expect(c.run("project", "read-only", "staging")).To(Equal(exitcode.OK))
			})

			// Re-arming writes on a protected tenant is the one direction of this
			// command that can lose data. A script that forgot --yes should be noisy.
			It("refuses without a terminal to ask on, rather than assuming yes", func() {
				code := c.run("project", "read-only", "staging", "--off")

				Expect(code).To(Equal(exitcode.Usage))
				Expect(readOnly()).To(BeTrue())
			})

			It("allows writes again when confirmed", func() {
				Expect(c.run("project", "read-only", "staging", "--off", "--yes")).To(Equal(exitcode.OK))

				Expect(readOnly()).To(BeFalse())
				Expect(c.errOut()).To(ContainSubstring("accepts writes again"))
			})

			It("aborts on a no, leaving the project protected", func() {
				c.answer("n")

				Expect(c.run("project", "read-only", "staging", "--off")).To(Equal(exitcode.OK))

				Expect(readOnly()).To(BeTrue())
				Expect(c.errOut()).To(ContainSubstring("still read-only"))
			})
		})

		It("reports an unknown project the way every other command does", func() {
			Expect(c.run("project", "read-only", "nope")).To(Equal(exitcode.Config))
		})
	})
})
