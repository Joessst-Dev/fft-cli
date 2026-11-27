package network_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/network"
)

// exampleConfig uses every part of the format.
const exampleConfig = `version: 1
roles:
  hub:
    facilities:
      - HUB-01
      - HUB-02
  store:
    type: MANAGED_FACILITY
peers:
  by: address.country
  minSize: 10
  share: 0.9
profiles:
  from: decision-logs
expect:
  - name: hub-coverage
    for:
      role: store
      where:
        address.country: DE
    reachableFrom:
      role: hub
      atLeast: 2
      maxTransitDays: 2
suppress:
  - rule: hub-coverage
    facility: BER-11
    reason: opens 2027-01, hub connections are added at go-live
    until: 2027-01-15
`

var today = time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)

func parse(text string) (*network.LoadedFile, error) {
	return network.ParseConfig([]byte(text), "fft-network.yaml")
}

func problems(err error) []network.Problem {
	GinkgoHelper()
	var cfgErr *network.ConfigError
	Expect(errors.As(err, &cfgErr)).To(BeTrue(), "%v", err)
	return cfgErr.Problems
}

// schemaValidates checks a YAML document against the embedded JSON Schema.
func schemaValidates(text string) error {
	GinkgoHelper()
	c := jsonschema.NewCompiler()
	c.AssertFormat()
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(network.ConfigSchema))
	Expect(err).NotTo(HaveOccurred())
	Expect(c.AddResource("fft-network.schema.json", doc)).To(Succeed())
	sch, err := c.Compile("fft-network.schema.json")
	Expect(err).NotTo(HaveOccurred())

	// An unquoted date is a YAML timestamp, which would reach the schema as a
	// date-time; the file means the calendar day it spells.
	var node yaml.Node
	Expect(yaml.Unmarshal([]byte(text), &node)).To(Succeed())
	var asStrings func(*yaml.Node)
	asStrings = func(n *yaml.Node) {
		if n.Tag == "!!timestamp" {
			n.Tag = "!!str"
		}
		for _, c := range n.Content {
			asStrings(c)
		}
	}
	asStrings(&node)
	var v any
	Expect(node.Decode(&v)).To(Succeed())
	raw, err := json.Marshal(v)
	Expect(err).NotTo(HaveOccurred())
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	Expect(err).NotTo(HaveOccurred())
	return sch.Validate(inst)
}

var _ = Describe("the config file", func() {
	It("reads a file that uses every part of the format", func() {
		lf, err := parse(exampleConfig)

		Expect(err).NotTo(HaveOccurred())
		Expect(lf.Roles.Hub.Facilities).To(Equal([]string{"HUB-01", "HUB-02"}))
		Expect(lf.Expect[0].ReachableFrom.AtLeast).To(Equal(2))
		Expect(*lf.Expect[0].ReachableFrom.MaxTransitDays).To(Equal(2.0))
		Expect(lf.Suppress[0].Until.Format(time.DateOnly)).To(Equal("2027-01-15"))
	})

	It("refuses a field it does not know, naming its line", func() {
		_, err := parse("version: 1\nsuppres: []\n")

		Expect(problems(err)).To(ConsistOf(network.Problem{Line: 2, Message: "field suppres not found in type network.File"}))
		Expect(exitcode.FromError(err)).To(Equal(exitcode.Config))
	})

	It("refuses YAML it cannot read, naming its line", func() {
		_, err := parse("version: 1\nroles: [\n")

		Expect(problems(err)[0].Line).To(BeNumerically(">", 0))
	})

	DescribeTable("names the line and the path of every value it cannot use",
		func(text string, line int, path, message string) {
			_, err := parse(text)

			Expect(problems(err)).To(ContainElement(network.Problem{Line: line, Path: path, Message: message}))
		},
		Entry("another format version", "version: 2\n", 1, "version",
			"must be 1, the format this fft reads; got 2"),
		Entry("no version at all, named at the top of the file", "rules: [no-source]\n", 1, "version",
			"must be 1, the format this fft reads; got 0"),
		Entry("an unknown rule", "version: 1\nrules:\n  - no-source\n  - no-sources\n", 4, "rules[1]",
			`unknown rule "no-sources": want one of no-source, peer-coverage, expect, dangling-ref, offline-ref, duplicate-edge, config-drift`),
		Entry("hubs both pinned and chosen by reach",
			"version: 1\nroles:\n  hub:\n    outDegree: relative\n    facilities: [HUB-01]\n", 3, "roles.hub",
			"pin the facilities or choose them by reach, not both"),
		Entry("stores chosen by reach",
			"version: 1\nroles:\n  store:\n    outDegree: relative\n", 4, "roles.store.outDegree",
			"only the hub role is chosen by reach"),
		Entry("an unknown where field",
			"version: 1\nroles:\n  store:\n    where:\n      address.city: Berlin\n", 5, "roles.store.where.address.city",
			"unknown field: want address.country, address.postalCode, group or customAttributes.<key>"),
		Entry("a share above 1", "version: 1\npeers:\n  share: 1.5\n", 3, "peers.share",
			"must be a fraction between 0 and 1, such as 0.9"),
		Entry("declared profiles with none declared", "version: 1\nprofiles:\n  from: declared\n", 2, "profiles.declared",
			"from: declared needs at least one profile"),
		Entry("an expectation named like a rule",
			"version: 1\nexpect:\n  - name: no-source\n    for: {role: store}\n    reachableFrom: {role: hub, atLeast: 1}\n",
			3, "expect[0].name", `"no-source" is a rule id; name the expectation something else`),
		Entry("an expectation wanting no source",
			"version: 1\nexpect:\n  - name: x\n    for: {role: store}\n    reachableFrom: {role: hub, atLeast: 0}\n",
			5, "expect[0].reachableFrom.atLeast", "must be at least 1"),
		Entry("a suppression for a rule that does not exist",
			"version: 1\nsuppress:\n  - rule: hub-coverage\n    facility: BER-01\n    reason: r\n    until: 2027-01-01\n",
			3, "suppress[0].rule", `"hub-coverage" is neither a rule id nor the name of an expectation`),
		Entry("a suppression without a reason",
			"version: 1\nsuppress:\n  - rule: no-source\n    facility: BER-01\n    until: 2027-01-01\n",
			3, "suppress[0].reason", "say why the finding is accepted, so the next person can judge whether it still is"),
	)

	It("refuses a date it cannot read", func() {
		_, err := parse("version: 1\nsuppress:\n  - rule: no-source\n    facility: BER-01\n    reason: r\n    until: next week\n")

		Expect(problems(err)[0]).To(Equal(network.Problem{Line: 6,
			Message: `"next week" is not a date: write it as YYYY-MM-DD`}))
	})

	It("reports a suppression whose date has passed, and keeps it out of the settings", func() {
		lf, err := parse(strings.Replace(exampleConfig, "2027-01-15", "2026-11-26", 1))
		Expect(err).NotTo(HaveOccurred())

		Expect(lf.Expired(today)).To(ConsistOf(network.Problem{Line: 29, Path: "suppress[0].until",
			Message: "expired on 2026-11-26: renew it with a reason that still holds, or remove it"}))
		Expect(lf.Settings(today).Suppress).To(BeEmpty())
	})

	It("keeps a suppression in force through the day it names", func() {
		lf, err := parse(strings.Replace(exampleConfig, "2027-01-15", "2026-11-27", 1))
		Expect(err).NotTo(HaveOccurred())

		Expect(lf.Expired(today)).To(BeEmpty())
		Expect(lf.Settings(today).Suppress).To(HaveLen(1))
	})

	It("reports what names nothing in the snapshot", func() {
		lf, err := parse(strings.Replace(exampleConfig, "HUB-02", "HUB-09", 1))
		Expect(err).NotTo(HaveOccurred())

		got := lf.Unmatched(network.New(&germany().s))

		Expect(got).To(ConsistOf(network.Problem{Line: 6, Path: "roles.hub.facilities[1]",
			Message: "HUB-09 is not a facility in the snapshot"}))
	})

	It("reads a file from disk, and says when there is none", func() {
		path := filepath.Join(GinkgoT().TempDir(), network.DefaultConfigFile)
		_, err := network.ReadConfig(path)
		Expect(network.IsNotExist(err)).To(BeTrue())

		Expect(os.WriteFile(path, []byte(exampleConfig), 0o600)).To(Succeed())
		lf, err := network.ReadConfig(path)
		Expect(err).NotTo(HaveOccurred())
		Expect(lf.Path).To(Equal(path))
	})

	Describe("its JSON Schema", func() {
		It("accepts the example", func() {
			Expect(schemaValidates(exampleConfig)).To(Succeed())
		})

		DescribeTable("refuses what the decoder refuses",
			func(text string) {
				_, parseErr := parse(text)
				Expect(parseErr).To(HaveOccurred())
				Expect(schemaValidates(text)).NotTo(Succeed())
			},
			Entry("an unknown field", "version: 1\nsuppres: []\n"),
			Entry("another version", "version: 2\n"),
			Entry("an unknown rule", "version: 1\nrules: [no-sources]\n"),
			Entry("hubs both pinned and by reach", "version: 1\nroles:\n  hub: {outDegree: relative, facilities: [A]}\n"),
			Entry("a suppression without a reason",
				"version: 1\nsuppress:\n  - {rule: no-source, facility: A, until: 2027-01-01}\n"),
			Entry("a suppression with no date", "version: 1\nsuppress:\n  - {rule: no-source, facility: A, reason: r, until: soon}\n"),
			Entry("a share above 1", "version: 1\npeers: {share: 1.5}\n"),
		)
	})
})

var _ = Describe("checking with a config file", func() {
	settings := func(text string) network.Config {
		GinkgoHelper()
		lf, err := parse(text)
		Expect(err).NotTo(HaveOccurred())
		return lf.Settings(today)
	}

	check := func(w *world, text string) network.Report {
		return network.New(&w.s).Check(settings(text))
	}

	It("takes the pinned hubs as the hubs", func() {
		r := check(germany(), "version: 1\nroles:\n  hub:\n    facilities: [HUB-01]\n")

		Expect(r.Roles.Hubs.Count()).To(Equal(1))
		Expect(r.Roles.Stores.Count()).To(Equal(12), "HUB-02 is a store now")
	})

	Describe("config-drift", func() {
		It("reports a pinned hub the heuristics would not call one, and a hub it does not pin", func() {
			w := germany().stores("STO", "DE", 1).
				edge("x", "STO-01", network.Target{Type: network.TypeManaged, FacilityRef: id("BER-01")})

			r := check(w, "version: 1\nroles:\n  hub:\n    facilities: [HUB-01, STO-01]\n")

			Expect(subjects(r, network.RuleConfigDrift)).To(ConsistOf("STO-01", "HUB-02"))
			Expect(finding(r, network.RuleConfigDrift, "STO-01").Detail).To(Equal(
				"pinned as a hub, but its connections reach 1 facility, which the heuristics would not call a hub"))
			Expect(finding(r, network.RuleConfigDrift, "HUB-02").Detail).To(ContainSubstring("roles.hub does not pin it"))
		})

		It("reports a pinned hub that has gone offline, or is gone", func() {
			w := germany()
			w.s.Facilities[1].Status = "OFFLINE"

			r := check(w, "version: 1\nroles:\n  hub:\n    facilities: [HUB-01, HUB-02, HUB-09]\n")

			Expect(finding(r, network.RuleConfigDrift, "HUB-02").Detail).To(Equal("pinned as a hub, and offline"))
			Expect(finding(r, network.RuleConfigDrift, "HUB-09").Detail).To(ContainSubstring("not a facility in the snapshot"))
		})

		It("reports a routed profile the declared ones miss", func() {
			w := germany().edge("x", "HUB-01", network.Target{Type: network.TypeManaged}, in(network.ContextTag, "express"))
			w.s.OrderProfiles = []network.OrderProfile{{Tags: []network.TagValue{{ID: "orderType", Value: "express"}}, Orders: 3}}

			r := check(w, "version: 1\nprofiles:\n  from: declared\n  declared:\n    - {orderType: standard}\n")

			Expect(finding(r, network.RuleConfigDrift, "orderType=express").Detail).To(Equal(
				"3 orders routed recently carried this profile, which profiles.declared does not list"))
		})

		It("is not checked without a config file, which pins nothing to drift from", func() {
			r := germany().stores("STO", "DE", 1).check()

			Expect(subjects(r, network.RuleConfigDrift)).To(BeEmpty())
		})
	})

	Describe("expect", func() {
		It("reports a store fewer sources reach than the expectation asks", func() {
			r := check(germany(), "version: 1\nexpect:\n"+
				"  - name: two-hubs\n    for: {role: store}\n    reachableFrom: {role: hub, atLeast: 2}\n")

			Expect(subjects(r, network.RuleExpect)).To(Equal([]string{"BER-11"}))
			f := finding(r, network.RuleExpect, "BER-11")
			Expect(f.Detail).To(Equal("two-hubs: 1 hub reaches it, and at least 2 should"))
			Expect(f.Evidence).To(HaveKeyWithValue("expectation", "two-hubs"))
		})

		It("counts only connections that arrive in time when maxTransitDays is set", func() {
			w := germany()
			w.s.Edges[0].Transit = &network.Transit{Min: 1, Max: 1}
			w.s.Edges[1].Transit = &network.Transit{Min: 2, Max: 3}

			r := check(w, "version: 1\nexpect:\n"+
				"  - name: fast\n    for: {role: store, where: {address.country: DE}}\n"+
				"    reachableFrom: {role: hub, atLeast: 2, maxTransitDays: 2}\n")

			Expect(subjects(r, network.RuleExpect)).To(HaveLen(11))
			Expect(finding(r, network.RuleExpect, "BER-01").Detail).To(Equal(
				"fast: 1 hub reaches it within 2 days, and at least 2 should"))
		})
	})

	Describe("suppress", func() {
		const twoHubs = "version: 1\nexpect:\n" +
			"  - name: two-hubs\n    for: {role: store}\n    reachableFrom: {role: hub, atLeast: 2}\n"

		It("accepts a finding by expectation name and subject, and says it did", func() {
			r := check(germany(), twoHubs+"suppress:\n"+
				"  - {rule: two-hubs, facility: BER-11, reason: second hub at go-live, until: 2027-01-15}\n")

			Expect(subjects(r, network.RuleExpect)).To(BeEmpty())
			Expect(r.Suppressed).To(HaveLen(1))
		})

		It("accepts a finding by rule id, with the subject given as a platform id", func() {
			w := germany().facility("OLD-01", network.TypeManaged, "OFFLINE", "DE").
				edge("o1", "HUB-01", network.Target{Type: network.TypeManaged, FacilityRef: id("OLD-01")})

			r := check(w, "version: 1\nsuppress:\n"+
				"  - {rule: offline-ref, facility: id-OLD-01, reason: reopens in spring, until: 2027-03-01}\n")

			Expect(subjects(r, network.RuleOfflineRef)).To(BeEmpty())
			Expect(r.Suppressed).To(HaveLen(1))
		})

		It("suppresses nothing once it has expired", func() {
			r := check(germany(), twoHubs+"suppress:\n"+
				"  - {rule: two-hubs, facility: BER-11, reason: second hub at go-live, until: 2026-11-01}\n")

			Expect(subjects(r, network.RuleExpect)).To(Equal([]string{"BER-11"}))
			Expect(r.Suppressed).To(BeEmpty())
		})
	})
})

var _ = Describe("inferring a config file", func() {
	It("writes a file the decoder and the schema both accept, and that names nothing missing", func() {
		w := germany()
		n := network.New(&w.s)

		text := string(n.InferFile(today))

		lf, err := parse(text)
		Expect(err).NotTo(HaveOccurred(), text)
		Expect(schemaValidates(text)).To(Succeed())
		Expect(lf.Unmatched(n)).To(BeEmpty())
	})

	It("pins the hubs it found, with the reason as a comment", func() {
		text := string(network.New(&germany().s).InferFile(today))

		Expect(text).To(ContainSubstring("# 2 facilities' connections each reach 11 or more online facilities"))
		Expect(text).To(ContainSubstring("    facilities:\n      - HUB-01\n      - HUB-02\n"))
	})

	It("writes one expectation per country a hub already covers, so a check with it finds the same gaps", func() {
		w := germany()
		w.s.Edges[0].Target = except(network.TypeManaged, "BER-11")
		n := network.New(&w.s)

		lf, err := parse(string(n.InferFile(today)))
		Expect(err).NotTo(HaveOccurred())
		r := n.Check(lf.Settings(today))

		Expect(lf.Expect).To(HaveLen(1))
		Expect(lf.Expect[0].Name).To(Equal("hub-coverage-de"))
		Expect(subjects(r, network.RuleExpect)).To(Equal([]string{"BER-11"}))
		Expect(subjects(r, network.RulePeerCoverage)).To(Equal([]string{"BER-11"}))
		Expect(subjects(r, network.RuleConfigDrift)).To(BeEmpty())
	})

	It("chooses hubs by reach when the network has no hub tier", func() {
		w := newWorld().stores("BER", "DE", 3).
			edge("a", "BER-01", network.Target{Type: network.TypeManaged, FacilityRef: id("BER-02")})

		text := string(network.New(&w.s).InferFile(today))

		Expect(text).To(ContainSubstring("  hub:\n    outDegree: relative\n"))
		_, err := parse(text)
		Expect(err).NotTo(HaveOccurred())
	})
})
