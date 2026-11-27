package main

import (
	"encoding/json"
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

// treeStrategy is root → [Express → Express node, Bulky (inactive) → Bulky node].
const treeStrategy = `{"id":"s1","nameLocalized":{"en_US":"Default"},"inUse":true,"revision":7,"version":3,
 "rootNode":{"id":"root","name":"Root","active":true,
  "config":{"fences":[{"implementation":"STOCK","active":true}],"ratings":[{"implementation":"DISTANCE","active":true}]},
  "nextCondition":{"id":"c-express","name":"Express","active":true,
   "rule":{"predicates":[{"propertyPath":"tags.orderType","entityOperator":"EQUALS","expectedValue":"express"}]},
   "nextNode":{"id":"n-express","name":"Express node","active":true},
   "nextCondition":{"id":"c-bulky","name":"Bulky","active":false,
    "nextNode":{"id":"n-bulky","name":"Bulky node","active":true}}}}}`

var _ = Describe("fft routing strategy tree", func() {
	var c *cli

	BeforeEach(func() { c = newCLI() })

	tenant := func(logs string) *tenant {
		return c.fakeTenant(func(w http.ResponseWriter, r *http.Request, _ []byte) {
			switch r.URL.Path {
			case "/api/routing/strategies":
				writeJSON(w, http.StatusOK, `{"routingStrategies":[{"id":"s0","inUse":false},`+treeStrategy+`],"total":2}`)
			case "/api/routing/strategies/s1":
				writeJSON(w, http.StatusOK, treeStrategy)
			case "/api/routing/decisionlogs":
				writeJSON(w, http.StatusOK, `{"decisionLogs":[`+logs+`],"total":1}`)
			default:
				writeJSON(w, http.StatusNotFound, `[{"summary":"not found"}]`)
			}
		})
	}

	It("draws the strategy in use as a tree, folding the inactive branch", func() {
		tenant("")

		Expect(c.run("routing", "strategy", "tree")).To(Equal(exitcode.OK))

		Expect(c.out()).To(Equal(`Default (in use, revision 7)
Root  [1 fence, 1 rating]
├─ when Express  (tags.orderType EQUALS express)
│  └─ Express node
└─ when Bulky  inactive; 1 node folded
`))
	})

	It("unfolds the inactive branch with --all", func() {
		tenant("")

		Expect(c.run("routing", "strategy", "tree", "s1", "--all")).To(Equal(exitcode.OK))

		Expect(c.out()).To(ContainSubstring("└─ when Bulky  inactive\n   └─ Bulky node\n"))
	})

	It("marks the branch the router took for an order, from its latest decision log", func() {
		api := tenant(`{"id":"l1","created":"2026-11-01T10:00:00Z","routingStrategyEvaluationResult":{"evaluatedPath":[` +
			`{"type":"NODE","ref":"root","evaluationResult":"NODE_ACTIVE"},` +
			`{"type":"CONDITION","ref":"c-express","evaluationResult":"CONDITION_TRUE"},` +
			`{"type":"NODE","ref":"n-express","evaluationResult":"NODE_ACTIVE"}]}}`)

		Expect(c.run("routing", "strategy", "tree", "--order", "o-1")).To(Equal(exitcode.OK))

		Expect(c.out()).To(ContainSubstring("▶ Root"))
		Expect(c.out()).To(ContainSubstring("├─ ▶ when Express"))
		Expect(c.out()).To(ContainSubstring("└─ ▶ Express node"))
		var sawFilter bool
		for _, call := range api.recorded() {
			if call.Path == "/api/routing/decisionlogs" {
				sawFilter = call.Query.Get("orderRef") == "o-1"
			}
		}
		Expect(sawFilter).To(BeTrue())
	})

	It("exits 6 when the order has no decision log to mark from", func() {
		tenant("")

		Expect(c.run("routing", "strategy", "tree", "--order", "o-1")).To(Equal(exitcode.NotFound))

		Expect(c.errOut()).To(ContainSubstring("has no routing decision log"))
	})

	It("warns when the order was routed with another strategy", func() {
		tenant(`{"id":"l1","routingStrategyEvaluationResult":{"evaluatedPath":[{"type":"NODE","ref":"other","evaluationResult":"NODE_ACTIVE"}]}}`)

		Expect(c.run("routing", "strategy", "tree", "--order", "o-1")).To(Equal(exitcode.OK))

		Expect(c.errOut()).To(ContainSubstring("routed with another one"))
	})

	It("prints the drawn tree as a document under -o json", func() {
		tenant("")

		Expect(c.run("routing", "strategy", "tree", "-o", "json")).To(Equal(exitcode.OK))

		var doc map[string]any
		Expect(json.Unmarshal([]byte(c.out()), &doc)).To(Succeed())
		Expect(doc).To(HaveKeyWithValue("name", "Root"))
		Expect(doc).To(HaveKeyWithValue("fences", []any{"STOCK"}))
		conds := doc["conditions"].([]any)
		Expect(conds[1]).To(HaveKeyWithValue("folded", BeNumerically("==", 1)))
	})

	It("runs under --read-only, sending only reads", func() {
		api := tenant("")

		Expect(c.run("--read-only", "routing", "strategy", "tree")).To(Equal(exitcode.OK))

		for _, call := range api.recorded() {
			Expect(call.Method).To(Equal(http.MethodGet))
		}
	})

	It("says how to name one when no strategy is in use", func() {
		c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, `{"routingStrategies":[{"id":"s0","inUse":false}],"total":1}`)
		})

		Expect(c.run("routing", "strategy", "tree")).To(Equal(exitcode.Usage))

		Expect(strings.ToLower(c.errOut())).To(ContainSubstring("no routing strategy is in use"))
	})
})
