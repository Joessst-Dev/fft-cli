package network_test

import (
	"encoding/json"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/network"
)

// strategyJSON is a strategy as the API writes it: root → [express, bulky (inactive),
// sameday (its node inactive)], express → [weekend]. Every id is a placeholder.
const strategyJSON = `{
  "id": "s1", "nameLocalized": {"en_US": "Default"}, "inUse": true, "revision": 7,
  "rootNode": {
    "id": "root", "name": "Root", "active": true, "categoryRef": "cat-1",
    "config": {
      "fences": [{"type": "StandardFence", "implementation": "STOCK", "active": true}],
      "ratings": [{"type": "StandardRating", "implementation": "DISTANCE", "active": true},
                  {"type": "ToolkitRating", "referenceId": "r-prio", "name": "Priority", "active": true}],
      "fallbackFacility": {"active": true, "facilityRefs": ["urn:fft:facility:tenantFacilityId:HUB-01"], "fallbackAfterTime": "PT2H"}
    },
    "nextCondition": {
      "id": "c-express", "name": "Express", "active": true,
      "rule": {"predicateConnector": "AND", "predicates": [{"propertyPath": "tags.orderType", "entityOperator": "EQUALS", "expectedValue": "express"}]},
      "nextNode": {
        "id": "n-express", "name": "Express node", "active": true, "categoryRef": "cat-gone",
        "config": {"ratings": [{"type": "StandardRating", "implementation": "DISTANCE", "active": false},
                               {"type": "ToolkitRating", "referenceId": "r-prio", "name": "Priority", "active": false}]},
        "nextCondition": {
          "id": "c-weekend", "name": "Weekend", "active": true,
          "activationTimeFrames": [{"activeFrom": "2020-01-01T00:00:00Z", "activeUntil": "2020-02-01T00:00:00Z"}],
          "nextNode": {"id": "n-weekend", "name": "Weekend node", "active": true}
        }
      },
      "nextCondition": {
        "id": "c-bulky", "name": "Bulky", "active": false,
        "nextNode": {"id": "n-bulky", "name": "Bulky node", "active": true,
          "nextCondition": {"id": "c-x", "name": "X", "active": true, "nextNode": {"id": "n-x", "name": "X node", "active": true}}},
        "nextCondition": {
          "id": "c-sameday", "name": "Same day", "active": true,
          "rule": {"predicates": [{"propertyPath": "$.customAttributes.hubs[?(@.tenantFacilityId == 'GONE-01')].prio", "entityOperator": "GREATER_THAN", "expectedValue": 3},
                                  {"propertyPath": "facility.tenantFacilityId", "entityOperator": "EQUALS", "expectedValue": "GONE-02"},
                                  {"propertyPath": "facility.tenantFacilityId", "entityOperator": "EQUALS", "expectedValue": "BER-01"}]},
          "nextNode": {"id": "n-sameday", "name": "Same day node", "active": false}
        }
      }
    }
  }
}`

func strategyWorld(strategies ...string) *world {
	w := germany()
	for _, s := range strategies {
		w.s.Strategies = append(w.s.Strategies, json.RawMessage(s))
	}
	w.s.Categories = []network.Category{{ID: "cat-1"}}
	w.s.CategoriesRead = true
	w.s.CreatedAt = time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)
	return w
}

var _ = Describe("a routing strategy", func() {
	It("reads the linked list of conditions as ordered children", func() {
		s, err := network.ParseStrategy(json.RawMessage(strategyJSON))

		Expect(err).NotTo(HaveOccurred())
		Expect(s.Name).To(Equal("Default"))
		Expect(s.InUse).To(BeTrue())
		names := []string{}
		for _, c := range s.Root.Conditions {
			names = append(names, c.Name)
		}
		Expect(names).To(Equal([]string{"Express", "Bulky", "Same day"}))
		Expect(s.Root.Conditions[0].Next.Conditions[0].Next.Name).To(Equal("Weekend node"))
		Expect(s.Root.Count()).To(Equal(6))
	})

	It("merges config along a path: a child's item replaces its parent's with the same key", func() {
		s, err := network.ParseStrategy(json.RawMessage(strategyJSON))
		Expect(err).NotTo(HaveOccurred())

		merged := network.Merge([]*network.StrategyNode{s.Root, s.Root.Conditions[0].Next})

		Expect(merged.Fences).To(HaveLen(1), "inherited from the root")
		Expect(merged.Ratings).To(HaveLen(2))
		for _, r := range merged.Ratings {
			Expect(r.Active).To(BeFalse(), r.Label())
		}
		Expect(merged.Fallback.Facilities).To(ConsistOf("urn:fft:facility:tenantFacilityId:HUB-01"))
	})

	It("refuses a document nested deeper than any real strategy", func() {
		doc := `{"id":"deep","rootNode":`
		for range 300 {
			doc += `{"id":"n","nextCondition":{"id":"c","nextNode":`
		}
		doc += `{"id":"leaf"}`
		for range 300 {
			doc += `}}`
		}
		doc += `}`

		_, err := network.ParseStrategy(json.RawMessage(doc))

		Expect(err).To(MatchError(ContainSubstring("levels deep")))
	})
})

var _ = Describe("the strategy rules", func() {
	check := func(w *world, rule string) network.Report {
		cfg := network.DefaultConfig()
		cfg.Rules = []string{rule}
		return network.New(&w.s).Check(cfg)
	}

	DescribeTable("report what makes part of the strategy in use dead or wrong",
		func(rule string, want []string, detail string) {
			r := check(strategyWorld(strategyJSON), rule)

			Expect(subjects(r, rule)).To(Equal(want))
			if detail != "" {
				Expect(r.Findings[0].Detail).To(Equal(detail))
			}
		},
		Entry("a categoryRef that names no category", network.RuleStrategyCategory,
			[]string{"Default › Express node"}, "the node's categoryRef cat-gone names no node config category"),
		Entry("an inactive condition with nodes below it, and an inactive node", network.RuleStrategyInactive,
			[]string{"Default › Bulky", "Default › Same day node"}, "the condition is inactive, and with it the 2 nodes below it"),
		Entry("a condition whose time frames have all ended", network.RuleStrategyExpired,
			[]string{"Default › Weekend"}, ""),
		Entry("a path whose merged config has no active rating", network.RuleStrategyNoRating,
			[]string{"Default › Express node", "Default › Weekend node"},
			"an order that ends here (Root › Express node) is routed with no active rating"),
		Entry("a facility id hard-coded in a toolkit rule that names nothing, in a filter or as a compared value",
			network.RuleToolkitHardcodeID, []string{"GONE-01", "GONE-02"}, ""),
	)

	It("does not take a number compared next to a JSONPath filter for a facility id", func() {
		r := check(strategyWorld(strategyJSON), network.RuleToolkitHardcodeID)

		Expect(subjects(r, network.RuleToolkitHardcodeID)).NotTo(ContainElement("3"))
	})

	It("reports a fallback facility that is offline", func() {
		w := strategyWorld(strategyJSON)
		w.s.Facilities[0].Status = "OFFLINE"

		r := check(w, network.RuleStrategyFallback)

		Expect(r.Findings).To(HaveLen(1))
		Expect(r.Findings[0].Detail).To(Equal("the fallback facility HUB-01 is offline"))
	})

	It("does not judge categories in a snapshot taken before fft read them", func() {
		w := strategyWorld(strategyJSON)
		w.s.CategoriesRead = false

		Expect(check(w, network.RuleStrategyCategory).Findings).To(BeEmpty())
	})

	DescribeTable("strategy-in-use reports anything but exactly one live strategy",
		func(live int, detail string) {
			var docs []string
			for i := range 2 {
				docs = append(docs, `{"id":"s`+string(rune('a'+i))+`","name":"S`+string(rune('A'+i))+
					`","inUse":`+map[bool]string{true: "true", false: "false"}[i < live]+`,"rootNode":{"id":"r","name":"R"}}`)
			}

			r := check(strategyWorld(docs...), network.RuleStrategyInUse)

			if detail == "" {
				Expect(r.Findings).To(BeEmpty())
				return
			}
			Expect(r.Findings).To(HaveLen(1))
			Expect(r.Findings[0].Detail).To(Equal(detail))
		},
		Entry("none", 0, "no routing strategy is in use"),
		Entry("one", 1, ""),
		Entry("two", 2, "2 routing strategies are in use at once: SA, SB"),
	)
})
