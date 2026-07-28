package main

import (
	"fmt"
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/config"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

// strategy renders one routing strategy the way the API does: an allOf whose name
// lives only in nameLocalized, with a rootNode and globalConfiguration the response
// schema requires.
func strategy(id, name string, revision int, inUse bool, version int) string {
	return fmt.Sprintf(
		`{"id":%q,"nameLocalized":{"en_US":%q},"revision":%d,"inUse":%t,"version":%d,`+
			`"rootNode":{"id":"root"},"globalConfiguration":{}}`,
		id, name, revision, inUse, version)
}

// strategyPage is the envelope GET /api/routing/strategies answers with: an array
// keyed routingStrategies, and a total. No cursor, no hasNextPage.
func strategyPage(items []string, total int) string {
	return fmt.Sprintf(`{"routingStrategies":[%s],"total":%d}`, strings.Join(items, ","), total)
}

var _ = Describe("fft routing strategy list", func() {
	var c *cli

	BeforeEach(func() { c = newCLI() })

	It("renders the strategies, marking only the live one", func() {
		c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, strategyPage([]string{
				strategy("s1", "Peak season", 2, true, 5),
				strategy("s2", "Off season", 1, false, 3),
			}, 2))
		})

		Expect(c.run("routing", "strategy", "list")).To(Equal(exitcode.OK))

		Expect(c.out()).To(Equal(strings.Join([]string{
			"ID   NAME          REVISION   IN USE   VERSION",
			"s1   Peak season   2          yes      5",
			"s2   Off season    1          -        3",
			"",
		}, "\n")))
	})

	It("reads the strategies out of the routingStrategies envelope", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, strategyPage(nil, 0))
		})

		Expect(c.run("routing", "strategy", "list")).To(Equal(exitcode.OK))
		Expect(api.only().Method).To(Equal(http.MethodGet))
		Expect(api.only().Path).To(Equal("/api/routing/strategies"))
	})

	It("starts the next page after the last id of the one before", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, r *http.Request, _ []byte) {
			if r.URL.Query().Get("startAfterId") == "s2" {
				writeJSON(w, http.StatusOK, strategyPage([]string{strategy("s3", "C", 1, false, 1)}, 3))
				return
			}
			writeJSON(w, http.StatusOK, strategyPage([]string{
				strategy("s1", "A", 1, false, 1),
				strategy("s2", "B", 1, false, 1),
			}, 3))
		})

		Expect(c.run("routing", "strategy", "list", "--all", "--size", "2")).To(Equal(exitcode.OK))

		Expect(api.calls).To(HaveLen(2))
		Expect(api.calls[1].Query.Get("startAfterId")).To(Equal("s2"))
		Expect(c.out()).To(ContainSubstring("s3"))
	})

	It("says there are more when the total outruns the page", func() {
		c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, strategyPage([]string{strategy("s1", "A", 1, false, 1)}, 9))
		})

		Expect(c.run("routing", "strategy", "list", "--size", "1")).To(Equal(exitcode.OK))
		Expect(c.errOut()).To(ContainSubstring("There are more routing strategies. Pass --all"))
	})
})

var _ = Describe("fft routing strategy get", func() {
	var c *cli

	BeforeEach(func() { c = newCLI() })

	It("reads one strategy by id and renders it", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, strategy("s1", "Peak season", 2, true, 5))
		})

		Expect(c.run("routing", "strategy", "get", "s1")).To(Equal(exitcode.OK))
		Expect(api.only().Path).To(Equal("/api/routing/strategies/s1"))
		Expect(c.out()).To(ContainSubstring("Peak season"))
	})

	It("prints the API's own JSON under -o json", func() {
		c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, strategy("s1", "Peak season", 2, true, 5))
		})

		Expect(c.run("routing", "strategy", "get", "s1", "-o", "json")).To(Equal(exitcode.OK))
		Expect(c.out()).To(ContainSubstring(`"rootNode"`))
	})
})

var _ = Describe("fft routing strategy create", func() {
	var c *cli

	BeforeEach(func() { c = newCLI() })

	It("prints an example that needs no project, credentials or network", func() {
		Expect(c.run("routing", "strategy", "create", "--example")).To(Equal(exitcode.OK))
		Expect(c.out()).To(ContainSubstring("nameLocalized"))
		doc, err := decodeDoc([]byte(c.out()), "the example")
		Expect(err).NotTo(HaveOccurred())
		Expect(doc).To(HaveKey("nameLocalized"))
	})

	It("sends the body and reports the id the API minted", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusCreated, strategy("s-new", "Peak season", 1, false, 1))
		})

		file := tempFile(`{"nameLocalized":{"en_US":"Peak season"}}`)
		Expect(c.run("routing", "strategy", "create", "--file", file)).To(Equal(exitcode.OK))

		Expect(api.only().Method).To(Equal(http.MethodPost))
		Expect(api.only().Path).To(Equal("/api/routing/strategies"))
		Expect(c.errOut()).To(ContainSubstring("Created routing strategy s-new"))
	})

	It("requires --file when no example was asked for", func() {
		api := c.fakeTenant(func(_ http.ResponseWriter, r *http.Request, _ []byte) {
			Fail("fft sent " + r.Method + " " + r.URL.Path + " for a create with no body")
		})

		Expect(c.run("routing", "strategy", "create")).To(Equal(exitcode.Usage))
		Expect(c.errOut()).To(ContainSubstring("--file is required"))
		Expect(api.calls).To(BeEmpty())
	})
})

var _ = Describe("fft routing strategy update", func() {
	var c *cli

	BeforeEach(func() { c = newCLI() })

	It("reads the current version and sends it back in the body", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, strategy("s1", "Peak season", 2, true, 4))
		})

		file := tempFile(strategy("s1", "Peak season", 2, true, 4))
		Expect(c.run("routing", "strategy", "update", "s1", "--file", file)).To(Equal(exitcode.OK))

		Expect(api.calls).To(HaveLen(2))
		Expect(api.calls[0].Method).To(Equal(http.MethodGet))

		put := api.calls[1]
		Expect(put.Method).To(Equal(http.MethodPut))
		Expect(put.Path).To(Equal("/api/routing/strategies/s1"))
		Expect(put.json()).To(HaveKeyWithValue("version", BeNumerically("==", 4)))
	})

	// The entityDoc design exists so a PUT carries the fields fft has no model for,
	// unchanged. rootNode is exactly such a field — a deep tree the view struct never
	// touches — so the invariant worth pinning is that it reaches the wire byte-for-byte,
	// not merely that the version does. A switch to the lossy generated model would drop
	// it and still pass a version-only assertion. Same guard as connection_test's round
	// trip.
	It("sends the whole document through untouched, not just the version", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, strategy("s1", "Peak season", 2, true, 4))
		})

		file := tempFile(`{"id":"s1","nameLocalized":{"en_US":"Peak season"},"revision":2,"inUse":true,` +
			`"version":4,"rootNode":{"id":"root","config":{"fences":[{"type":"StandardFence","implementation":"FACILITY-BUSINESSTYPE"}]}},` +
			`"globalConfiguration":{"defaultPrice":10}}`)
		Expect(c.run("routing", "strategy", "update", "s1", "--file", file)).To(Equal(exitcode.OK))

		put := api.calls[len(api.calls)-1].json()
		// The nested node tree survived: id, config, and the fence buried inside it.
		root, ok := put["rootNode"].(map[string]any)
		Expect(ok).To(BeTrue(), "rootNode was dropped from the PUT body")
		Expect(root).To(HaveKeyWithValue("id", "root"))
		cfg, ok := root["config"].(map[string]any)
		Expect(ok).To(BeTrue(), "rootNode.config was dropped")
		Expect(cfg["fences"]).To(HaveLen(1))
		Expect(put).To(HaveKey("globalConfiguration"))
	})

	It("skips the read when --if-version says what the version is", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, strategy("s1", "Peak season", 2, true, 9))
		})

		file := tempFile(strategy("s1", "Peak season", 2, true, 9))
		Expect(c.run("routing", "strategy", "update", "s1", "--file", file, "--if-version", "7")).To(Equal(exitcode.OK))

		Expect(api.only().Method).To(Equal(http.MethodPut))
		Expect(api.only().json()).To(HaveKeyWithValue("version", BeNumerically("==", 7)))
	})
})

var _ = Describe("fft routing strategy activate", func() {
	var c *cli

	BeforeEach(func() { c = newCLI() })

	It("reads the version, then POSTs the ACTIVATE action carrying it", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, r *http.Request, _ []byte) {
			if strings.HasSuffix(r.URL.Path, "/actions") {
				writeJSON(w, http.StatusOK, strategy("s1", "Peak season", 2, true, 5))
				return
			}
			writeJSON(w, http.StatusOK, strategy("s1", "Peak season", 2, false, 4))
		})

		Expect(c.run("routing", "strategy", "activate", "s1")).To(Equal(exitcode.OK))

		Expect(api.calls).To(HaveLen(2))
		Expect(api.calls[0].Method).To(Equal(http.MethodGet))

		post := api.calls[1]
		Expect(post.Method).To(Equal(http.MethodPost))
		Expect(post.Path).To(Equal("/api/routing/strategies/s1/actions"))
		Expect(post.json()).To(HaveKeyWithValue("name", "ACTIVATE"))
		Expect(post.json()).To(HaveKeyWithValue("version", BeNumerically("==", 4)))
		Expect(c.errOut()).To(ContainSubstring("Activated routing strategy s1"))
	})

	It("skips the read under --if-version and sends only the action", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, strategy("s1", "Peak season", 2, true, 8))
		})

		Expect(c.run("routing", "strategy", "activate", "s1", "--if-version", "8")).To(Equal(exitcode.OK))

		post := api.only()
		Expect(post.Method).To(Equal(http.MethodPost))
		Expect(post.json()).To(HaveKeyWithValue("name", "ACTIVATE"))
		Expect(post.json()).To(HaveKeyWithValue("version", BeNumerically("==", 8)))
	})

	// The version travels in the body, so a stale one is a 409 the retry cannot fix
	// when --if-version pins it: fft sends what it was told and reports the conflict.
	It("exits 7 on a hard version conflict under --if-version", func() {
		c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusConflict, `[{"status":409,"detail":"stale"}]`)
		})

		Expect(c.run("routing", "strategy", "activate", "s1", "--if-version", "1")).To(Equal(exitcode.Conflict))
	})
})

var _ = Describe("fft routing strategy evaluate", func() {
	var c *cli

	BeforeEach(func() { c = newCLI() })

	It("sends the order to the evaluation endpoint and renders the path", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, `{"evaluatedConfig":{},"evaluatedPath":[`+
				`{"type":"NODE","nameLocalized":{"en_US":"Root"},"ref":"root","evaluationResult":"PASSED"}]}`)
		})

		file := tempFile(`{"consumer":{},"orderLineItems":[]}`)
		Expect(c.run("routing", "strategy", "evaluate", "s1", "--file", file)).To(Equal(exitcode.OK))

		Expect(api.only().Method).To(Equal(http.MethodPost))
		Expect(api.only().Path).To(Equal("/api/routing/strategies/s1/evaluation"))
		Expect(c.out()).To(ContainSubstring("PASSED"))
	})

	It("is allowed under --read-only, because evaluation reserves nothing", func() {
		c.setenv(config.EnvReadOnly, "1")
		c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, `{"evaluatedConfig":{},"evaluatedPath":[]}`)
		})

		file := tempFile(`{"consumer":{},"orderLineItems":[]}`)
		Expect(c.run("routing", "strategy", "evaluate", "s1", "--file", file)).To(Equal(exitcode.OK))
	})

	// --example takes no id, and cobra validates args before RunE — so ExactArgs(1)
	// would make `evaluate --example` fail with exit 2 before the example branch runs.
	It("prints an example with no id and no network", func() {
		Expect(c.run("routing", "strategy", "evaluate", "--example")).To(Equal(exitcode.OK))
		Expect(c.out()).NotTo(BeEmpty())
	})

	// An empty evaluated path is a real answer for a single-object response, so the
	// API's document is still printed under -o json — Printer.Empty (`[]`) would be the
	// wrong shape and throw the evaluatedConfig away.
	It("still prints the API document under -o json when the path is empty", func() {
		c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, `{"evaluatedConfig":{"marker":true},"evaluatedPath":[]}`)
		})

		file := tempFile(`{"consumer":{},"orderLineItems":[]}`)
		Expect(c.run("routing", "strategy", "evaluate", "s1", "--file", file, "-o", "json")).To(Equal(exitcode.OK))
		Expect(c.out()).To(ContainSubstring(`"evaluatedConfig"`))
		Expect(c.out()).NotTo(Equal("[]\n"))
	})
})

var _ = Describe("fft routing strategy actions", func() {
	var c *cli

	BeforeEach(func() { c = newCLI() })

	It("sends the action body to the actions endpoint and reports what ran", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, strategy("s1", "Peak season", 3, false, 6))
		})

		file := tempFile(`{"name":"REPLACE_GLOBAL_CONFIGURATION","version":5,"globalConfiguration":{"defaultPrice":20}}`)
		Expect(c.run("routing", "strategy", "actions", "s1", "--file", file)).To(Equal(exitcode.OK))

		Expect(api.only().Method).To(Equal(http.MethodPost))
		Expect(api.only().Path).To(Equal("/api/routing/strategies/s1/actions"))
		Expect(c.errOut()).To(ContainSubstring("Ran REPLACE_GLOBAL_CONFIGURATION against routing strategy s1"))
	})

	It("refuses an action the API does not have, before sending anything", func() {
		api := c.fakeTenant(func(_ http.ResponseWriter, r *http.Request, _ []byte) {
			Fail("fft sent " + r.Method + " " + r.URL.Path + " for an unknown action")
		})

		file := tempFile(`{"name":"DEMOLISH"}`)
		Expect(c.run("routing", "strategy", "actions", "s1", "--file", file)).To(Equal(exitcode.Usage))
		Expect(c.errOut()).To(ContainSubstring("unknown action"))
		Expect(api.calls).To(BeEmpty())
	})

	It("prints an example with no id and no network", func() {
		Expect(c.run("routing", "strategy", "actions", "--example")).To(Equal(exitcode.OK))
		Expect(c.out()).To(ContainSubstring("name"))
	})

	It("is refused under --read-only, because an action is a write", func() {
		c.setenv(config.EnvReadOnly, "1")
		api := c.fakeTenant(func(_ http.ResponseWriter, r *http.Request, _ []byte) {
			Fail("fft sent " + r.Method + " " + r.URL.Path + " under --read-only")
		})

		file := tempFile(`{"name":"COPY"}`)
		Expect(c.run("routing", "strategy", "actions", "s1", "--file", file)).To(Equal(exitcode.ReadOnly))
		Expect(api.calls).To(BeEmpty())
	})
})

var _ = Describe("fft routing strategy evaluate-node", func() {
	var c *cli

	BeforeEach(func() { c = newCLI() })

	It("evaluates one node by strategy and node id", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, `{"evaluatedConfig":{},"evaluatedPath":[`+
				`{"type":"NODE","nameLocalized":{"en_US":"Root"},"ref":"n1","evaluationResult":"PASSED"}]}`)
		})

		Expect(c.run("routing", "strategy", "evaluate-node", "s1", "n1")).To(Equal(exitcode.OK))

		Expect(api.only().Method).To(Equal(http.MethodPost))
		Expect(api.only().Path).To(Equal("/api/routing/strategies/s1/nodes/n1/evaluation"))
		Expect(c.out()).To(ContainSubstring("PASSED"))
	})

	It("is allowed under --read-only, because node evaluation reserves nothing", func() {
		c.setenv(config.EnvReadOnly, "1")
		c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, `{"evaluatedConfig":{},"evaluatedPath":[]}`)
		})

		Expect(c.run("routing", "strategy", "evaluate-node", "s1", "n1")).To(Equal(exitcode.OK))
	})
})

var _ = Describe("fft routing strategy read-only refusals", func() {
	var c *cli
	var api *tenant

	BeforeEach(func() {
		c = newCLI()
		c.setenv(config.EnvReadOnly, "1")
		// A tenant that fails the spec if it is ever reached: a read-only refusal must
		// happen before a byte goes over the wire (exit 10).
		api = c.fakeTenant(func(_ http.ResponseWriter, r *http.Request, _ []byte) {
			Fail("fft sent " + r.Method + " " + r.URL.Path + " under --read-only")
		})
	})

	AfterEach(func() { Expect(api.calls).To(BeEmpty()) })

	It("refuses create", func() {
		file := tempFile(`{"nameLocalized":{"en_US":"x"}}`)
		Expect(c.run("routing", "strategy", "create", "--file", file)).To(Equal(exitcode.ReadOnly))
	})

	It("refuses update", func() {
		file := tempFile(strategy("s1", "x", 1, false, 1))
		Expect(c.run("routing", "strategy", "update", "s1", "--file", file)).To(Equal(exitcode.ReadOnly))
	})

	It("refuses activate", func() {
		Expect(c.run("routing", "strategy", "activate", "s1")).To(Equal(exitcode.ReadOnly))
	})
})
