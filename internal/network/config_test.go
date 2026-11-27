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
			"must be a fraction above 0 and at most 1, such as 0.9"),
		// An explicit 0 used to be read as "not set" and replaced by the default.
		Entry("a share of 0", "version: 1\npeers:\n  share: 0\n", 3, "peers.share",
			"must be a fraction above 0 and at most 1, such as 0.9"),
		Entry("a minSize of 0", "version: 1\npeers:\n  minSize: 0\n", 3, "peers.minSize",
			"must be at least 1"),
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

	It("keeps a suppression in force through its last day for a caller west of UTC", func() {
		// until is a calendar day, parsed as UTC midnight. At 17:00 PST on the day
		// it names, the instant is already past 2027-01-16T00:00Z, yet the caller's
		// calendar still says the 15th.
		pst := time.FixedZone("PST", -8*3600)
		evening := time.Date(2027, 1, 15, 17, 0, 0, 0, pst)
		lf, err := parse(exampleConfig)
		Expect(err).NotTo(HaveOccurred())

		Expect(lf.Expired(evening)).To(BeEmpty())
		Expect(lf.Settings(evening).Suppress).To(HaveLen(1))
	})

	It("ends a suppression with its last day for a caller east of UTC", func() {
		// 01:00 on the 16th in Berlin is still 2027-01-15T23:00Z, but the day is over.
		cet := time.FixedZone("CET", 3600)
		morning := time.Date(2027, 1, 16, 1, 0, 0, 0, cet)
		lf, err := parse(exampleConfig)
		Expect(err).NotTo(HaveOccurred())

		Expect(lf.Expired(morning)).To(HaveLen(1))
		Expect(lf.Settings(morning).Suppress).To(BeEmpty())
	})

	It("reports the problems in the same order every time, by line", func() {
		text := "version: 1\nroles:\n  supplier:\n    type: NOPE\n  store:\n    type: NOPE\n  hub:\n    type: NOPE\n"
		var first string
		for range 20 {
			_, err := parse(text)
			Expect(err).To(HaveOccurred())
			if first == "" {
				first = err.Error()
			}
			Expect(err.Error()).To(Equal(first))
		}
		Expect(first).To(Equal("fft-network.yaml: line 4: roles.supplier.type: must be MANAGED_FACILITY or SUPPLIER, not \"NOPE\"; " +
			"line 6: roles.store.type: must be MANAGED_FACILITY or SUPPLIER, not \"NOPE\"; " +
			"line 8: roles.hub.type: must be MANAGED_FACILITY or SUPPLIER, not \"NOPE\""))
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
			Entry("a minJump without outDegree", "version: 1\nroles:\n  hub: {minJump: 2}\n"),
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
			Expect(finding(r, network.RuleConfigDrift, "HUB-02").Detail).To(Equal(
				"its connections reach 12 facilities, as a hub's do, but roles.hub does not pin it"))
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

		It("lets a drift finding about a profile be suppressed, and validate accept the suppression", func() {
			w := germany().edge("x", "HUB-01", network.Target{Type: network.TypeManaged}, in(network.ContextTag, "express"))
			w.s.OrderProfiles = []network.OrderProfile{{Tags: []network.TagValue{{ID: "orderType", Value: "express"}}, Orders: 3}}
			text := "version: 1\nprofiles:\n  from: declared\n  declared:\n    - {orderType: standard}\n" +
				"suppress:\n  - {rule: config-drift, facility: orderType=express, reason: express is being retired, until: 2027-01-15}\n"

			lf, err := parse(text)
			Expect(err).NotTo(HaveOccurred())
			r := network.New(&w.s).Check(lf.Settings(today))

			Expect(subjects(r, network.RuleConfigDrift)).NotTo(ContainElement("orderType=express"))
			Expect(r.Suppressed).To(HaveLen(1))
			Expect(lf.Unmatched(network.New(&w.s))).To(BeEmpty(),
				"a profile is a subject a config-drift suppression may name")
		})

		It("still reports a config-drift suppression whose subject is no profile, facility or reference", func() {
			w := germany()
			lf, err := parse("version: 1\nsuppress:\n  - {rule: config-drift, facility: orderType=nothing, reason: r, until: 2027-01-15}\n")
			Expect(err).NotTo(HaveOccurred())

			Expect(lf.Unmatched(network.New(&w.s))).To(HaveLen(1))
		})

		It("reports a config-drift suppression of a declared profile that was never routed, which drift cannot report", func() {
			w := germany().edge("x", "HUB-01", network.Target{Type: network.TypeManaged}, in(network.ContextTag, "express"))
			lf, err := parse("version: 1\nprofiles:\n  from: declared\n  declared:\n    - {orderType: express}\n" +
				"suppress:\n  - {rule: config-drift, facility: orderType=express, reason: r, until: 2027-01-15}\n")
			Expect(err).NotTo(HaveOccurred())

			Expect(lf.Unmatched(network.New(&w.s))).To(HaveLen(1))
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

	It("writes no expectation for stores that have no country, which no where could select", func() {
		w := germany().stores("NOC", "", 11)
		n := network.New(&w.s)

		text := string(n.InferFile(today))

		lf, err := parse(text)
		Expect(err).NotTo(HaveOccurred(), text)
		Expect(text).NotTo(ContainSubstring("hub-coverage-none"))
		Expect(text).NotTo(ContainSubstring(`address.country: "(none)"`))
		// validate runs Unmatched; before the fix it exited 3 on infer's own output.
		Expect(lf.Unmatched(n)).To(BeEmpty())
	})

	DescribeTable("keeps tenant data inside its comment, whatever line break it carries",
		func(lineBreak string) {
			payload := "demo" + lineBreak + "expect: [{name: x, for: {role: store}, reachableFrom: {role: hub, atLeast: 1}}]" + lineBreak + "#"
			w := newWorld().stores("BER", "DE", 3).
				edge("a", "BER-01", network.Target{Type: network.TypeManaged, FacilityRef: id("BER-02")})
			w.s.Source.Project = payload
			n := network.New(&w.s)

			text := string(n.InferFile(today))

			lf, err := parse(text)
			Expect(err).NotTo(HaveOccurred(), text)
			Expect(lf.Expect).To(BeEmpty(), "nobody wrote an expectation; the tenant's project name did")
			Expect(lf.Suppress).To(BeEmpty())
		},
		Entry("LF", "\n"),
		Entry("CR", "\r"),
		Entry("CRLF", "\r\n"),
		Entry("NEL", "\u0085"),
		Entry("LS", "\u2028"),
		Entry("PS", "\u2029"),
	)

	DescribeTable("keeps tenant data inside its value, whatever line break it carries",
		func(lineBreak string) {
			// yaml.Marshal writes a string with a break as a "|-" block indented from
			// column 0, which ends empty under a deeper prefix and frees the rest.
			hubID := "HUB-01" + lineBreak + "maxTransitDays: 1"
			country := "# x" + lineBreak + "maxTransitDays: 1"
			w := germany()
			for i := range w.s.Facilities {
				switch f := &w.s.Facilities[i]; {
				case f.TenantFacilityID == "HUB-01":
					f.TenantFacilityID = hubID
				case strings.HasPrefix(f.TenantFacilityID, "BER-"):
					f.Country = country
				}
			}
			n := network.New(&w.s)

			text := string(n.InferFile(today))

			lf, err := parse(text)
			Expect(err).NotTo(HaveOccurred(), text)
			Expect(schemaValidates(text)).To(Succeed())
			Expect(lf.Roles.Hub.Facilities).To(ContainElement(hubID))
			Expect(lf.Expect).To(HaveLen(1))
			Expect(lf.Expect[0].For.Where).To(Equal(map[string]string{"address.country": country}))
			Expect(lf.Expect[0].ReachableFrom.MaxTransitDays).To(BeZero(), "nobody wrote a delivery promise; the tenant's country did")
			Expect(lf.Suppress).To(BeEmpty())
			Expect(lf.Unmatched(n)).To(BeEmpty())
		},
		Entry("LF", "\n"),
		Entry("CR", "\r"),
		Entry("CRLF", "\r\n"),
		Entry("NEL", "\u0085"),
		Entry("LS", "\u2028"),
		Entry("PS", "\u2029"),
	)

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
