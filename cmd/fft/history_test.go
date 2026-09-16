package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/Joessst-Dev/fft-cli/internal/config"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/history"
	"github.com/Joessst-Dev/fft-cli/internal/tui"
)

// withHistory points the spec's history at a file of its own, and returns the log.
func (c *cli) withHistory() history.Log {
	c.deps.HistoryPath = filepath.Join(GinkgoT().TempDir(), "state", "history.jsonl")
	return history.Log{Path: c.deps.HistoryPath}
}

// recorded reads back every entry of log.
func recorded(log history.Log) []history.Entry {
	GinkgoHelper()
	entries, err := log.Read()
	Expect(err).NotTo(HaveOccurred())
	return entries
}

// historyTenant is [cli.configuredTenant] answering like a tenant: a facility
// search finds one facility, and anything asking for "missing" is not there.
func (c *cli) historyTenant() *tenant {
	return c.configuredTenant(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "missing") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"facilities":[{"id":"f-1","name":"Berlin"}],"total":1,"id":"f-1"}`))
	})
}

var _ = Describe("request history", func() {
	var (
		c   *cli
		log history.Log
	)

	BeforeEach(func() {
		c = newCLI()
		log = c.withHistory()
	})

	When("a configured project is used", func() {
		BeforeEach(func() {
			c.historyTenant()
		})

		It("records the request: what, where, and how it ended", func() {
			before := time.Now().UTC()
			Expect(c.run("facility", "list", "--status", "ONLINE", "--all")).To(Equal(exitcode.OK), c.errOut())

			entries := recorded(log)
			Expect(entries).To(HaveLen(1))
			e := entries[0]
			Expect(e.V).To(Equal(history.Version))
			Expect(e.Source).To(Equal(history.SourceCLI))
			Expect(e.Project).To(Equal("prod"))
			Expect(e.OperationID).To(Equal("searchFacility"))
			Expect(e.Command).To(Equal("fft facility list"))
			Expect(e.Args).To(Equal([]string{"--all", "--status=ONLINE"}))
			Expect(e.Status).To(Equal(http.StatusOK))
			Expect(e.Exit).To(Equal(exitcode.OK))
			Expect(e.TS).To(BeTemporally(">=", before.Truncate(time.Second)))
		})

		It("records the project a run named, not the active one", func() {
			Expect(c.run("facility", "get", "f-1", "--project", "other")).To(Equal(exitcode.OK), c.errOut())

			Expect(recorded(log)).To(ConsistOf(HaveField("Project", "other")))
		})

		It("records a failure with its status and exit code", func() {
			Expect(c.run("facility", "get", "missing")).To(Equal(exitcode.NotFound))

			Expect(recorded(log)).To(ConsistOf(And(
				HaveField("OperationID", "getFacility"),
				HaveField("Status", http.StatusNotFound),
				HaveField("Exit", exitcode.NotFound),
				HaveField("Args", []string{"missing"}),
			)))
		})

		It("records an operation reached through fft api by the operation it names", func() {
			Expect(c.run("api", "getFacility", "--param", "facilityId=f-1")).To(Equal(exitcode.OK), c.errOut())

			Expect(recorded(log)).To(ConsistOf(And(
				HaveField("OperationID", "getFacility"),
				HaveField("Command", "fft api"),
				HaveField("Args", []string{"getFacility", "--param=facilityId=<redacted>"}),
			)))
		})

		It("records a usage error on a known operation, though nothing was sent", func() {
			Expect(c.run("api", "getFacility")).To(Equal(exitcode.Usage))

			Expect(recorded(log)).To(ConsistOf(And(
				HaveField("OperationID", "getFacility"),
				HaveField("Status", 0),
				HaveField("Exit", exitcode.Usage),
			)))
		})

		It("records a write the read-only gate refused", func() {
			Expect(c.run("--read-only", "facility", "delete", "f-1", "--yes")).To(Equal(exitcode.ReadOnly))

			Expect(recorded(log)).To(ConsistOf(And(
				HaveField("OperationID", "deleteFacility"),
				HaveField("Project", "prod"),
				HaveField("Exit", exitcode.ReadOnly),
			)))
		})

		It("keeps no request body, header value or credential", func() {
			Expect(c.run("api", "addPickJob",
				"--data", `{"pickLineItems":[],"secret":"s3cret"}`,
				"--header", "X-Trace=s3cret",
			)).To(Equal(exitcode.OK), c.errOut())

			data, err := os.ReadFile(log.Path)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(data)).NotTo(ContainSubstring("s3cret"))
			Expect(recorded(log)).To(ConsistOf(HaveField("Args", ConsistOf(
				"addPickJob", "--data=<redacted>", "--header=X-Trace=<redacted>",
			))))
		})

		It("keeps where a body came from", func() {
			c.stdin.WriteString(`{"pickLineItems":[]}`)
			Expect(c.run("api", "addPickJob", "--data", "-")).To(Equal(exitcode.OK), c.errOut())

			Expect(recorded(log)).To(ConsistOf(HaveField("Args", ConsistOf("addPickJob", "--data=-"))))
		})

		DescribeTable("records nothing for a command line that sent nothing of its own",
			func(args ...string) {
				c.run(args...)
				Expect(recorded(log)).To(BeEmpty())
			},
			Entry("help", "facility", "list", "--help"),
			Entry("an example", "api", "addPickJob", "--example"),
			Entry("a generated command's example", "picking", "add-pick-job", "--example"),
			Entry("a command with no operation", "version"),
			Entry("a project being added", "project", "add", "dev", "--base-url", "https://dev.api.fulfillmenttools.com"),
			Entry("an unknown command", "nonsense"),
			Entry("an unknown operation", "api", "getNonsense"),
		)

		It("records nothing for a component, whose requests fft never sees", func() {
			c.installFake(fakeManifest("hello"))
			Expect(c.run("hello")).To(Equal(exitcode.OK))

			Expect(recorded(log)).To(BeEmpty())
		})

		It("records a run from fft tui as the UI's", func() {
			r := c.newRunner()
			id := start(r, tui.Invocation{Args: []string{"facility", "list"}})
			Expect(awaitDone(r, id)[id].ExitCode).To(Equal(exitcode.OK))

			Expect(recorded(log)).To(ConsistOf(And(
				HaveField("Source", history.SourceTUI),
				HaveField("Status", http.StatusOK),
			)))
		})

		It("records nothing when the config file says not to", func() {
			cfg, err := c.deps.Config.Load()
			Expect(err).NotTo(HaveOccurred())
			cfg.Settings.NoHistory = true
			Expect(c.deps.Config.Save(cfg)).To(Succeed())

			Expect(c.run("facility", "list")).To(Equal(exitcode.OK))
			Expect(recorded(log)).To(BeEmpty())
		})

		It("records nothing when FFT_HISTORY is off", func() {
			c.setenv(config.EnvHistory, "off")

			Expect(c.run("facility", "list")).To(Equal(exitcode.OK))
			Expect(recorded(log)).To(BeEmpty())
		})

		It("says under --debug why an entry could not be written", func() {
			blocker := filepath.Join(GinkgoT().TempDir(), "a-file")
			Expect(os.WriteFile(blocker, nil, 0o600)).To(Succeed())
			c.deps.HistoryPath = filepath.Join(blocker, "history.jsonl")

			Expect(c.run("facility", "list", "--debug")).To(Equal(exitcode.OK))
			Expect(c.errOut()).To(ContainSubstring("history: not recorded"))
		})

		It("stays within its size limit however much is recorded", func() {
			c.deps.historyMaxBytes = 2048
			for range 30 {
				Expect(c.run("facility", "list", "--status", "ONLINE")).To(Equal(exitcode.OK))
			}

			info, err := os.Stat(log.Path)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.Size()).To(BeNumerically("<=", 2048))
			Expect(len(recorded(log))).To(BeNumerically("<", 30))
		})
	})

	When("fft runs from the environment", func() {
		BeforeEach(func() {
			c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"facilities":[],"total":0}`))
			})
		})

		It("records nothing: a CI job's history is a file nobody reads", func() {
			Expect(c.run("facility", "list")).To(Equal(exitcode.OK), c.errOut())
			Expect(recorded(log)).To(BeEmpty())
		})

		It("records when FFT_HISTORY=on asks it to", func() {
			c.setenv(config.EnvHistory, "on")

			Expect(c.run("facility", "list")).To(Equal(exitcode.OK), c.errOut())
			Expect(recorded(log)).To(ConsistOf(HaveField("OperationID", "searchFacility")))
		})
	})

	// Recording is a side effect nobody asked this command for. Whether it happens,
	// is switched off, or cannot happen, what the command prints and how it exits
	// must be exactly the same.
	DescribeTable("never changes what a command prints or how it exits",
		func(args ...string) {
			type outcome struct {
				code           int
				stdout, stderr string
			}
			runWith := func(setup func(*cli)) outcome {
				c := newCLI()
				c.withHistory()
				c.historyTenant()
				setup(c)
				code := c.run(args...)
				return outcome{code, c.out(), c.errOut()}
			}

			on := runWith(func(*cli) {})
			off := runWith(func(c *cli) { c.setenv(config.EnvHistory, "off") })
			broken := runWith(func(c *cli) {
				blocker := filepath.Join(GinkgoT().TempDir(), "a-file")
				Expect(os.WriteFile(blocker, nil, 0o600)).To(Succeed())
				c.deps.HistoryPath = filepath.Join(blocker, "history.jsonl")
			})

			Expect(off).To(Equal(on))
			Expect(broken).To(Equal(on))
		},
		Entry("a successful read", "facility", "list", "-o", "json"),
		Entry("a table", "facility", "list"),
		Entry("a failed read", "facility", "get", "missing"),
		Entry("a refused write", "--read-only", "facility", "delete", "f-1", "--yes"),
		Entry("a usage error", "api", "getFacility"),
	)
})

var _ = Describe("fft history", func() {
	var (
		c   *cli
		log history.Log
	)

	seed := func(entries ...history.Entry) {
		GinkgoHelper()
		for _, e := range entries {
			Expect(log.Append(e)).To(Succeed())
		}
	}

	at := func(project, operation string, minute int) history.Entry {
		return history.Entry{
			V:           history.Version,
			TS:          time.Date(2026, 9, 16, 12, minute, 0, 0, time.UTC),
			Source:      history.SourceCLI,
			Project:     project,
			OperationID: operation,
			Command:     "fft api",
			Args:        []string{operation},
			Status:      http.StatusOK,
			DurationMS:  42,
		}
	}

	BeforeEach(func() {
		c = newCLI()
		log = c.withHistory()
		c.historyTenant()
	})

	Describe("list", func() {
		BeforeEach(func() {
			seed(
				at("prod", "getFacility", 1),
				at("other", "addPickJob", 2),
				at("prod", "searchFacility", 3),
			)
		})

		It("prints every project's requests as recorded, newest first", func() {
			Expect(c.run("history", "list", "-o", "json")).To(Equal(exitcode.OK), c.errOut())

			var got []history.Entry
			Expect(json.Unmarshal(c.stdout.Bytes(), &got)).To(Succeed(), c.out())
			Expect(got).To(Equal([]history.Entry{
				at("prod", "searchFacility", 3),
				at("other", "addPickJob", 2),
				at("prod", "getFacility", 1),
			}))
		})

		It("prints a table of them", func() {
			Expect(c.run("history", "list")).To(Equal(exitcode.OK), c.errOut())

			lines := strings.Split(strings.TrimSpace(c.out()), "\n")
			Expect(lines).To(HaveLen(4))
			Expect(lines[0]).To(MatchRegexp(`^WHEN\s+PROJECT\s+OPERATION\s+STATUS\s+EXIT\s+TOOK\s+COMMAND$`))
			Expect(lines[1]).To(MatchRegexp(`prod\s+searchFacility\s+200\s+0\s+42ms\s+fft api searchFacility$`))
		})

		It("stops at --limit", func() {
			Expect(c.run("history", "list", "--limit", "1", "-o", "json")).To(Equal(exitcode.OK))

			var got []history.Entry
			Expect(json.Unmarshal(c.stdout.Bytes(), &got)).To(Succeed())
			Expect(got).To(ConsistOf(HaveField("OperationID", "searchFacility")))
		})

		It("lists one project's requests when --project names it", func() {
			Expect(c.run("history", "list", "--project", "other", "-o", "json")).To(Equal(exitcode.OK))

			var got []history.Entry
			Expect(json.Unmarshal(c.stdout.Bytes(), &got)).To(Succeed())
			Expect(got).To(ConsistOf(HaveField("OperationID", "addPickJob")))
		})

		It("refuses a negative --limit", func() {
			Expect(c.run("history", "list", "--limit", "-1")).To(Equal(exitcode.Usage))
			Expect(c.out()).To(BeEmpty())
		})

		It("is not recorded itself", func() {
			Expect(c.run("history", "list")).To(Equal(exitcode.OK))
			Expect(recorded(log)).To(HaveLen(3))
		})
	})

	Describe("list with nothing recorded", func() {
		It("prints an empty JSON list", func() {
			Expect(c.run("history", "list", "-o", "json")).To(Equal(exitcode.OK))
			Expect(strings.TrimSpace(c.out())).To(Equal("[]"))
		})

		It("says so on stderr and prints no table", func() {
			Expect(c.run("history", "list")).To(Equal(exitcode.OK))
			Expect(c.out()).To(BeEmpty())
			Expect(c.errOut()).To(ContainSubstring("No recorded requests found."))
		})

		It("says why when recording is off", func() {
			c.setenv(config.EnvHistory, "off")

			Expect(c.run("history", "list", "-o", "json")).To(Equal(exitcode.OK))
			Expect(strings.TrimSpace(c.out())).To(Equal("[]"))
			Expect(c.errOut()).To(ContainSubstring("Requests are not being recorded: FFT_HISTORY is off."))
		})
	})

	Describe("top", func() {
		BeforeEach(func() {
			seed(
				at("prod", "getFacility", 1),
				at("prod", "searchFacility", 2),
				at("prod", "getFacility", 3),
				at("other", "addPickJob", 4),
				at("other", "addPickJob", 5),
				at("other", "addPickJob", 6),
			)
		})

		It("counts the current project's operations, most used first", func() {
			Expect(c.run("history", "top", "-o", "json")).To(Equal(exitcode.OK), c.errOut())

			var got []history.Usage
			Expect(json.Unmarshal(c.stdout.Bytes(), &got)).To(Succeed(), c.out())
			Expect(got).To(Equal([]history.Usage{
				{Project: "prod", OperationID: "getFacility", Command: "fft api", Count: 2, LastUsed: at("", "", 3).TS},
				{Project: "prod", OperationID: "searchFacility", Command: "fft api", Count: 1, LastUsed: at("", "", 2).TS},
			}))
		})

		It("counts the project --project names", func() {
			Expect(c.run("history", "top", "--project", "other", "-o", "json")).To(Equal(exitcode.OK))

			var got []history.Usage
			Expect(json.Unmarshal(c.stdout.Bytes(), &got)).To(Succeed())
			Expect(got).To(ConsistOf(And(HaveField("OperationID", "addPickJob"), HaveField("Count", 3))))
		})

		It("counts every project with --all-projects, and stops at --limit", func() {
			Expect(c.run("history", "top", "--all-projects", "--limit", "2", "-o", "json")).To(Equal(exitcode.OK))

			var got []history.Usage
			Expect(json.Unmarshal(c.stdout.Bytes(), &got)).To(Succeed())
			Expect(got).To(HaveExactElements(
				And(HaveField("Project", "other"), HaveField("Count", 3)),
				And(HaveField("Project", "prod"), HaveField("Count", 2)),
			))
		})

		It("prints a table", func() {
			Expect(c.run("history", "top")).To(Equal(exitcode.OK))

			lines := strings.Split(strings.TrimSpace(c.out()), "\n")
			Expect(lines).To(HaveLen(3))
			Expect(lines[0]).To(MatchRegexp(`^COUNT\s+LAST USED\s+PROJECT\s+OPERATION\s+COMMAND$`))
			Expect(lines[1]).To(MatchRegexp(`^2\s+.*prod\s+getFacility\s+fft api$`))
		})

		It("prints an empty JSON list for a project with no history", func() {
			cfg, err := c.deps.Config.Load()
			Expect(err).NotTo(HaveOccurred())
			cfg.Upsert(config.Project{Name: "quiet", BaseURL: "https://quiet.api.fulfillmenttools.com", Email: "bot@quiet.com"})
			Expect(c.deps.Config.Save(cfg)).To(Succeed())

			Expect(c.run("history", "top", "--project", "quiet", "-o", "json")).To(Equal(exitcode.OK))
			Expect(strings.TrimSpace(c.out())).To(Equal("[]"))
		})

		It("asks for a project when there is no current one", func() {
			cfg, err := c.deps.Config.Load()
			Expect(err).NotTo(HaveOccurred())
			cfg.ActiveProject = ""
			Expect(c.deps.Config.Save(cfg)).To(Succeed())

			Expect(c.run("history", "top")).To(Equal(exitcode.Config))
			Expect(c.errOut()).To(ContainSubstring("--all-projects"))
			Expect(c.out()).To(BeEmpty())
		})
	})

	Describe("clear", func() {
		BeforeEach(func() {
			seed(at("prod", "getFacility", 1), at("prod", "getFacility", 2))
		})

		It("deletes the history with --yes, and says so on stderr", func() {
			Expect(c.run("history", "clear", "--yes")).To(Equal(exitcode.OK), c.errOut())

			Expect(recorded(log)).To(BeEmpty())
			Expect(c.out()).To(BeEmpty())
			Expect(c.errOut()).To(ContainSubstring("Cleared 2 entries"))
		})

		It("asks first, and keeps the history on a no", func() {
			c.answer("n")
			Expect(c.run("history", "clear")).To(Equal(exitcode.OK))

			Expect(recorded(log)).To(HaveLen(2))
			Expect(c.errOut()).To(ContainSubstring("Delete the whole request history?"))
		})

		It("clears on a yes", func() {
			c.answer("y")
			Expect(c.run("history", "clear")).To(Equal(exitcode.OK))

			Expect(recorded(log)).To(BeEmpty())
		})

		It("refuses without a terminal to ask on", func() {
			Expect(c.run("history", "clear")).To(Equal(exitcode.Usage))

			Expect(recorded(log)).To(HaveLen(2))
			Expect(c.errOut()).To(ContainSubstring("--yes"))
		})
	})
})

// The redaction rules are keyed on flag names, so a flag added tomorrow that
// carries a body or a secret under a new name would be recorded in the clear. This
// walks every command fft has and holds each flag that looks like it could to the
// rules.
var _ = Describe("the history's redaction of every flag in the tree", func() {
	// notABody are flags whose usage mentions JSON without carrying any.
	notABody := map[string]string{
		"file":   "names the file a body is read from; the body itself is never recorded",
		"output": "names an output format",
	}

	It("drops the value of every flag that could carry a body or a credential", func() {
		root := newRootCmd(newCLI().deps)

		var walk func(cmd *cobra.Command)
		walk = func(cmd *cobra.Command) {
			cmd.Flags().VisitAll(func(f *pflag.Flag) {
				if !strings.Contains(f.Value.Type(), "string") {
					return
				}
				credential := looksCredential(f.Name)
				body := strings.Contains(strings.ToLower(f.Usage), "json")
				if _, excused := notABody[f.Name]; excused {
					body = false
				}
				if !credential && !body {
					return
				}

				arg := "--" + f.Name + "=plainly-a-secret"
				Expect(strings.Join(history.Redact([]string{arg}), " ")).NotTo(ContainSubstring("plainly-a-secret"),
					"%s --%s (%q) would be recorded in the clear: teach history.Redact about it",
					cmd.CommandPath(), f.Name, f.Usage)
			})
			for _, sub := range cmd.Commands() {
				walk(sub)
			}
		}
		walk(root)
	})
})

// looksCredential is the spec's own, deliberately broader, idea of a
// credential-shaped flag name, so that the guard does not merely restate the rule
// it is guarding.
func looksCredential(name string) bool {
	name = strings.ToLower(name)
	for _, word := range []string{"password", "passwd", "secret", "token", "apikey", "api-key", "auth", "credential"} {
		if strings.Contains(name, word) {
			return true
		}
	}
	return false
}
