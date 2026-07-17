package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/api"
	"github.com/Joessst-Dev/fft-cli/internal/auth"
	"github.com/Joessst-Dev/fft-cli/internal/config"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/secrets"
)

// commandsWithoutOperation are the commands that carry no operationId annotation,
// each with the reason they need none. Every one of them either makes no request at
// all, or makes one that is not to the tenant.
//
// This map is the other half of the read-only gate. The gate is keyed on the
// annotation, so a command that could reach the tenant without one would not be
// gated — it would write to a read-only project and no one would find out. The spec
// below therefore refuses to let such a command exist: a new command that can
// address an operation is either annotated, or it is named here with a reason
// somebody had to write down.
//
// That is the enforcement. There is no transport backstop catching mutating
// requests on their way out, because an invariant that says no such request can be
// *built* is worth more than a net under one that can.
var commandsWithoutOperation = map[string]string{
	"fft version": "prints the build info; no network",

	// The one command that reaches the tenant and cannot be annotated: its operation
	// is an argument, not a compile-time fact. It calls guardOperation itself, and the
	// specs below pin that it does — for a write, for a read, and for the read-alike
	// that writes.
	"fft api": "resolves its operation from args, so it gates itself in RunE",

	// The config file and the keychain are not the tenant. Blocking these on a
	// read-only project would make it unconfigurable — and `project read-only --off`,
	// the way out, would be the first thing to go.
	"fft project add":       "writes the config file and the keychain; its only request is a Firebase sign-in",
	"fft project list":      "reads the config file",
	"fft project use":       "writes the config file",
	"fft project remove":    "writes the config file and the keychain",
	"fft project current":   "reads the config file",
	"fft project read-only": "writes the config file",

	"fft auth token":   "mints a token at Google, never at the tenant",
	"fft auth refresh": "mints a token at Google, never at the tenant",

	"fft api list":     "reads the embedded spec table; no network",
	"fft api describe": "reads the embedded spec table; no network",

	// The skill is documentation compiled into the binary. `install` copies it onto
	// this machine and `show` prints it: neither reaches the tenant, and neither needs
	// a project — which is what lets `fft skill install` be the first thing a user
	// runs, before `fft project add`.
	"fft skill install": "copies the embedded skill onto the local disk; no network",
	"fft skill show":    "prints the embedded skill; no network",

	"fft update check": "asks GitHub for the latest release",

	// Renders the command tree to Markdown for the docs site. Builds a tree and
	// prints it — no project, no network, nothing that reaches the tenant.
	"fft gen-docs": "writes the CLI reference to local disk; no network",
}

// addressableCommands are the commands that can name an operation to send: every
// runnable leaf, plus every runnable parent that accepts an argument.
//
// That second clause is not a technicality. `fft api <operationId>` is a parent —
// it has `list` and `describe` under it — and it is also the single most dangerous
// command in the tree, because the operation it sends is an *argument*. A walk that
// collected only childless commands would skip it, and the invariant below would be
// asserting nothing about exactly the command that most needs it.
//
// A group like `fft facility` is runnable too, in that cobra will print its help,
// but it takes no arguments and so can address nothing. That is what tells the two
// apart here — structurally, rather than by a list someone has to remember to update.
func addressableCommands(root *cobra.Command) []*cobra.Command {
	var out []*cobra.Command

	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if c.Runnable() && (len(c.Commands()) == 0 || acceptsArgs(c)) {
			out = append(out, c)
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(root)

	return out
}

// acceptsArgs reports whether the command would accept a positional argument, by
// offering it one. A command group rejects it (cobra.NoArgs); `fft api` takes it.
func acceptsArgs(c *cobra.Command) bool {
	if c.Args == nil {
		return true
	}
	return c.Args(c, []string{"probe"}) == nil
}

var _ = Describe("the read-only gate's coverage of the command tree", func() {
	var root *cobra.Command

	BeforeEach(func() {
		root = newRootCmd(&Deps{})
	})

	It("annotates every command that can address an operation, unless it is excused", func() {
		for _, cmd := range addressableCommands(root) {
			path := cmd.CommandPath()
			if _, excused := commandsWithoutOperation[path]; excused {
				continue
			}

			Expect(cmd.Annotations).To(HaveKey(annotationOperationID),
				"%s has no operationId annotation, so the read-only gate cannot tell whether it writes. "+
					"Annotate it, or — if it makes no request to the tenant — add it to commandsWithoutOperation "+
					"with the reason", path)
		}
	})

	// Both directions. Without this, an excuse outlives the command it excused, and
	// the next command to take that name inherits a hole in the gate.
	It("excuses no command that the tree does not have", func() {
		paths := make(map[string]bool)
		for _, cmd := range addressableCommands(root) {
			paths[cmd.CommandPath()] = true
		}

		for path := range commandsWithoutOperation {
			Expect(paths).To(HaveKey(path),
				"commandsWithoutOperation excuses %q, which is not a command any more", path)
		}
	})

	It("annotates every command with an operation the spec actually has", func() {
		for _, cmd := range addressableCommands(root) {
			id, ok := cmd.Annotations[annotationOperationID]
			if !ok {
				continue
			}

			_, found := api.LookupOperation(id)
			Expect(found).To(BeTrue(), "%s is annotated with %q, which the API spec does not have", cmd.CommandPath(), id)
		}
	})
})

// readOnlyProject configures a project in the config file — the path a human uses,
// as opposed to the FFT_* one CI uses — pointing at a fake tenant, and returns the
// tenant so a spec can prove what did and did not reach it.
func (c *cli) readOnlyProject(readOnly bool) *tenant {
	t := &tenant{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		Expect(err).NotTo(HaveOccurred())

		t.calls = append(t.calls, call{Method: r.Method, Path: r.URL.Path, Body: body})

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"facilities":[],"total":0}`))
	}))
	DeferCleanup(srv.Close)

	cfg := config.New()
	cfg.ActiveProject = "prod"
	cfg.Upsert(config.Project{
		Name:           "prod",
		BaseURL:        srv.URL,
		FirebaseAPIKey: "AIzaSyExample",
		Email:          "bot@ocff-acme-prod.com",
		ReadOnly:       readOnly,
	})
	Expect(c.deps.Config.Save(cfg)).To(Succeed())

	return t
}

var _ = Describe("a read-only project", func() {
	var c *cli
	var t *tenant

	BeforeEach(func() {
		c = newCLI()
		t = c.readOnlyProject(true)
	})

	// The point of the whole feature: the write is refused, and — the part that
	// matters more than the exit code — nothing was sent.
	DescribeTable("refuses a command that would change the tenant",
		func(args ...string) {
			code := c.run(args...)

			Expect(code).To(Equal(exitcode.ReadOnly))
			Expect(t.calls).To(BeEmpty(), "a read-only project sent a request")
			Expect(c.errOut()).To(ContainSubstring(`project "prod" is read-only`))
		},
		Entry("delete", "facility", "delete", "f1", "--yes"),
		Entry("patch", "facility", "patch", "f1", "--name", "x"),
		Entry("a generated command", "picking", "add-pick-job", "--data", "{}"),
		Entry("fft api", "api", "addPickJob", "--data", "{}"),

		// The trap. It sits in the /promises family with the pure calculators, and it
		// reserves stock and creates a PROMISED order. A rule that classified POSTs by
		// their path or their family would have let this through.
		Entry("a POST that reads like a calculator", "api", "postDeliveryPromise", "--data", "{}"),
	)

	// The other half of the point, and the harder half: the API does its searches
	// over POST, so "block the writes" cannot mean "block the POSTs".
	// The gate runs before the body is read, so a create whose body is on stdin is
	// refused rather than left blocking on a pipe it was never going to send.
	It("refuses a create without reading the body it was given", func() {
		c.stdin.WriteString(`{"name":"x"}`)

		Expect(c.run("facility", "create", "--file", "-")).To(Equal(exitcode.ReadOnly))

		Expect(t.calls).To(BeEmpty())
		Expect(c.stdin.Len()).NotTo(BeZero(), "the refused create consumed stdin anyway")
	})

	It("still runs a search, which is a POST", func() {
		code := c.run("facility", "list")

		Expect(code).To(Equal(exitcode.OK))
		Expect(t.only().Method).To(Equal(http.MethodPost))
		Expect(t.only().Path).To(Equal("/api/facilities/search"))
	})

	It("still runs a read through fft api", func() {
		Expect(c.run("api", "searchPickJob", "--data", "{}")).To(Equal(exitcode.OK))
		Expect(t.calls).To(HaveLen(1))
	})

	It("still prints a sample body for a write it would refuse to send", func() {
		Expect(c.run("api", "addPickJob", "--example")).To(Equal(exitcode.OK))

		Expect(c.out()).NotTo(BeEmpty())
		Expect(t.calls).To(BeEmpty())
	})

	It("still prints the help for a write it would refuse to send", func() {
		Expect(c.run("facility", "delete", "--help")).To(Equal(exitcode.OK))
		Expect(t.calls).To(BeEmpty())
	})

	It("names the way out", func() {
		c.run("facility", "delete", "f1", "--yes")

		Expect(c.errOut()).To(ContainSubstring("fft project read-only prod --off"))
	})

	// A refused write must cost nothing: no sign-in, no keychain, no token. The
	// gate runs before any of that, and this is what proves it.
	It("mints no token", func() {
		minted := false
		c.deps.NewTokenSource = func(config.Project, secrets.Store, func() time.Time, io.Writer) (auth.TokenSource, error) {
			minted = true
			return auth.StaticTokenSource(testIDToken), nil
		}

		Expect(c.run("facility", "delete", "f1", "--yes")).To(Equal(exitcode.ReadOnly))
		Expect(minted).To(BeFalse(), "a refused write signed in anyway")
	})

	// A command line that contradicts itself is not a request to write. Reporting it
	// as a read-only refusal would send whoever read that exit code — a human, or an
	// agent whose skill says exit 10 means "ask the user" — off to argue about a
	// permission they do not need, over a command that could never have been sent.
	It("calls a self-contradictory command line a usage error, not a refused write", func() {
		code := c.run("api", "addPickJob", "--file", "job.json", "--data", "{}")

		Expect(code).To(Equal(exitcode.Usage))
		Expect(t.calls).To(BeEmpty())
	})

	When("--read-only=false tries to loosen it", func() {
		It("refuses, rather than quietly honouring the flag", func() {
			code := c.run("facility", "delete", "f1", "--yes", "--read-only=false")

			Expect(code).To(Equal(exitcode.Usage))
			Expect(t.calls).To(BeEmpty())
			Expect(c.errOut()).To(ContainSubstring("cannot loosen"))
		})
	})
})

var _ = Describe("a writable project", func() {
	var c *cli
	var t *tenant

	BeforeEach(func() {
		c = newCLI()
		t = c.readOnlyProject(false)
	})

	It("deletes, as it always did", func() {
		Expect(c.run("facility", "delete", "f1", "--yes")).To(Equal(exitcode.OK))
		Expect(t.only().Method).To(Equal(http.MethodDelete))
	})

	When("--read-only is given for one invocation", func() {
		It("refuses the write", func() {
			code := c.run("facility", "delete", "f1", "--yes", "--read-only")

			Expect(code).To(Equal(exitcode.ReadOnly))
			Expect(t.calls).To(BeEmpty())
			Expect(c.errOut()).To(ContainSubstring("Drop --read-only"))
		})

		It("persists nothing: the project is writable again on the next run", func() {
			Expect(c.run("facility", "delete", "f1", "--yes", "--read-only")).To(Equal(exitcode.ReadOnly))

			Expect(c.run("facility", "delete", "f1", "--yes")).To(Equal(exitcode.OK))
		})
	})

	// --read-only=false is only ever refused when it would loosen something. On a
	// project that was writable anyway it asks for nothing it does not already have.
	It("accepts --read-only=false as the no-op it is", func() {
		Expect(c.run("facility", "delete", "f1", "--yes", "--read-only=false")).To(Equal(exitcode.OK))
	})
})

var _ = Describe("FFT_READ_ONLY", func() {
	var c *cli
	var t *tenant

	BeforeEach(func() {
		c = newCLI()
		t = c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"facilities":[],"total":0}`))
		})
		c.setenv(config.EnvReadOnly, "1")
	})

	It("refuses a write in the headless project", func() {
		code := c.run("picking", "add-pick-job", "--data", "{}")

		Expect(code).To(Equal(exitcode.ReadOnly))
		Expect(t.calls).To(BeEmpty())
		Expect(c.errOut()).To(ContainSubstring("Unset FFT_READ_ONLY"))
	})

	It("still searches", func() {
		Expect(c.run("facility", "list")).To(Equal(exitcode.OK))
		Expect(t.only().Method).To(Equal(http.MethodPost))
	})

	It("cannot be loosened by --read-only=false", func() {
		code := c.run("picking", "add-pick-job", "--data", "{}", "--read-only=false")

		Expect(code).To(Equal(exitcode.Usage))
		Expect(t.calls).To(BeEmpty())
	})

	// It is a policy, not a credential: on its own it must not conjure a project out
	// of nothing, and it must not stop fft configuring itself.
	It("does not on its own make fft headless", func() {
		c = newCLI()
		c.setenv(config.EnvReadOnly, "1")

		Expect(addStaging(c, "s3cret")).To(Equal(exitcode.OK))

		_, err := os.Stat(c.configPath)
		Expect(err).NotTo(HaveOccurred())
	})
})
