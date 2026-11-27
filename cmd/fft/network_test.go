package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/config"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/network"
	"github.com/Joessst-Dev/fft-cli/internal/testsupport"
)

// networkTenant answers every request a crawl sends with a two-facility network:
// BER-01 with one wildcard connection, and FRA-02 with none.
func networkTenant(w http.ResponseWriter, r *http.Request, _ []byte) {
	last := `"pageInfo":{"hasNextPage":false}`
	switch path := r.URL.Path; {
	case path == "/api/facilities/search":
		writeJSON(w, http.StatusOK, `{"facilities":[`+
			`{"id":"ber-uuid","tenantFacilityId":"BER-01","type":"MANAGED_FACILITY","status":"ONLINE","address":{"country":"DE"}},`+
			`{"id":"fra-uuid","tenantFacilityId":"FRA-02","type":"MANAGED_FACILITY","status":"OFFLINE","address":{"country":"DE"}}],`+last+`}`)
	case path == "/api/facilities/ber-uuid/connections" && r.URL.Query().Get("startAfterId") == "":
		writeJSON(w, http.StatusOK, connectionPage([]string{
			`{"id":"c1","sourceFacilityRef":"ber-uuid","target":{"type":"MANAGED_FACILITY"},` +
				`"context":[{"type":"TAG_REFERENCE","values":["express"]}]}`,
		}, 1))
	case strings.HasSuffix(path, "/connections"):
		writeJSON(w, http.StatusOK, connectionPage(nil, 0))
	case path == "/api/facilitygroups/search":
		writeJSON(w, http.StatusOK, `{"facilityGroups":[],`+last+`}`)
	case path == "/api/tags":
		writeJSON(w, http.StatusOK, `{"tags":[{"id":"orderType","allowedValues":["express"]}],"total":1}`)
	case path == "/api/routing/strategies":
		writeJSON(w, http.StatusOK, `{"routingStrategies":[{"id":"s1","inUse":true}],"total":1}`)
	case path == "/api/routing/decisionlogs":
		writeJSON(w, http.StatusOK, `{"decisionLogs":[{"id":"l1","relatedRefs":{"orderRef":"o1"}}],"total":1}`)
	case path == "/api/orders/o1":
		writeJSON(w, http.StatusOK, `{"id":"o1","tags":[{"id":"orderType","value":"express"}]}`)
	default:
		writeJSON(w, http.StatusNotFound, fmt.Sprintf(`[{"summary":"no route for %s"}]`, path))
	}
}

var _ = Describe("fft network snapshot", func() {
	var c *cli

	BeforeEach(func() { c = newCLI() })

	defaultPath := func() string {
		path, err := defaultSnapshotPath(&Deps{}, network.Source{
			Project: config.EphemeralName, BaseURL: os.Getenv(config.EnvBaseURL)})
		Expect(err).NotTo(HaveOccurred())
		return path
	}

	It("writes the network to the project's file in the cache directory, readable by its owner only", func() {
		c.fakeTenant(networkTenant)

		Expect(c.run("network", "snapshot")).To(Equal(exitcode.OK))

		s, err := network.Load(defaultPath())
		Expect(err).NotTo(HaveOccurred())
		Expect(s.Facilities).To(HaveLen(2))
		Expect(s.Edges).To(HaveLen(1))
		Expect(s.OrderProfiles).To(HaveLen(1))
		testsupport.ExpectOwnerOnlyFile(defaultPath())
	})

	DescribeTable("naming the default file after the tenant",
		func(a, b network.Source) {
			pa, err := defaultSnapshotPath(&Deps{}, a)
			Expect(err).NotTo(HaveOccurred())
			pb, err := defaultSnapshotPath(&Deps{}, b)
			Expect(err).NotTo(HaveOccurred())

			// One file for two tenants means the second crawl truncates the
			// first's journal and overwrites its snapshot.
			Expect(pa).NotTo(Equal(pb))
		},
		Entry("projects whose names sanitise to the same slug",
			network.Source{Project: "acme prod", BaseURL: "https://a.example"},
			network.Source{Project: "acme/prod", BaseURL: "https://a.example"}),
		Entry("headless runs, which are all called env, against different tenants",
			network.Source{Project: config.EphemeralName, BaseURL: "https://a.example"},
			network.Source{Project: config.EphemeralName, BaseURL: "https://b.example"}),
		Entry("one project name on two base URLs",
			network.Source{Project: "acme", BaseURL: "https://a.example"},
			network.Source{Project: "acme", BaseURL: "https://b.example"}),
	)

	It("keeps the same default file for the same tenant, so --resume finds its journal", func() {
		src := network.Source{Project: "acme", BaseURL: "https://a.example"}
		first, err := defaultSnapshotPath(&Deps{}, src)
		Expect(err).NotTo(HaveOccurred())
		second, err := defaultSnapshotPath(&Deps{}, src)
		Expect(err).NotTo(HaveOccurred())

		Expect(second).To(Equal(first))
	})

	It("prints a summary on stdout and its progress on stderr", func() {
		c.fakeTenant(networkTenant)

		Expect(c.run("network", "snapshot")).To(Equal(exitcode.OK))

		lines := strings.Split(strings.TrimSpace(c.out()), "\n")
		Expect(lines).To(HaveLen(2))
		Expect(strings.Fields(lines[0])).To(Equal(strings.Fields(
			"FACILITIES ONLINE CONNECTIONS GROUPS STRATEGIES DECISION LOGS ORDER PROFILES")))
		Expect(strings.Fields(lines[1])).To(Equal([]string{"2", "1", "1", "0", "1", "1", "1"}))
		Expect(c.errOut()).To(ContainSubstring("reading the connections of 2 facilities"))
		Expect(c.errOut()).To(ContainSubstring("Snapshot written to " + defaultPath()))
	})

	It("names the snapshot's path in the -o json summary, so a script can find it", func() {
		c.fakeTenant(networkTenant)
		out := filepath.Join(GinkgoT().TempDir(), "n.json.gz")

		Expect(c.run("network", "snapshot", "--out", out, "-o", "json")).To(Equal(exitcode.OK))

		var sum map[string]any
		Expect(json.Unmarshal([]byte(c.out()), &sum)).To(Succeed())
		Expect(sum).To(HaveKeyWithValue("path", out))
		Expect(sum).To(HaveKeyWithValue("connections", BeNumerically("==", 1)))
		Expect(out).To(BeAnExistingFile())
	})

	It("warns that a token it cannot renew may expire mid-crawl", func() {
		c.fakeTenant(networkTenant)

		Expect(c.run("network", "snapshot")).To(Equal(exitcode.OK))

		Expect(c.errOut()).To(ContainSubstring("cannot be renewed"))
	})

	It("runs under --read-only, since every request it sends is a read", func() {
		api := c.fakeTenant(networkTenant)

		Expect(c.run("--read-only", "network", "snapshot")).To(Equal(exitcode.OK))

		for _, call := range api.recorded() {
			if call.Method == http.MethodPost {
				Expect(call.Path).To(HaveSuffix("/search"))
			} else {
				Expect(call.Method).To(Equal(http.MethodGet))
			}
		}
	})

	It("skips the decision logs and orders with --decision-logs 0", func() {
		api := c.fakeTenant(networkTenant)

		Expect(c.run("network", "snapshot", "--decision-logs", "0")).To(Equal(exitcode.OK))

		for _, call := range api.recorded() {
			Expect(call.Path).NotTo(HavePrefix("/api/routing/decisionlogs"))
			Expect(call.Path).NotTo(HavePrefix("/api/orders"))
		}
	})

	It("carries on after a gateway throttles", func() {
		throttled := 0
		c.fakeTenant(func(w http.ResponseWriter, r *http.Request, body []byte) {
			if r.URL.Path == "/api/facilities/fra-uuid/connections" && throttled < 3 {
				throttled++
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			networkTenant(w, r, body)
		})

		Expect(c.run("network", "snapshot")).To(Equal(exitcode.OK))

		Expect(c.errOut()).To(ContainSubstring("pausing 1m0s"))
	})

	When("the API refuses the facility search as 'Query too complex'", func() {
		It("stops at once with the explanation and the 400's exit code, without pausing", func() {
			c.fakeTenant(func(w http.ResponseWriter, r *http.Request, body []byte) {
				if r.URL.Path == "/api/facilities/search" {
					writeJSON(w, http.StatusBadRequest, queryTooComplex)
					return
				}
				networkTenant(w, r, body)
			})

			Expect(c.run("network", "snapshot")).To(Equal(exitcode.General))

			Expect(c.errOut()).To(ContainSubstring("Query too complex"))
			Expect(c.errOut()).To(ContainSubstring(`A smaller page size (--size, or the body's "size") will not help`))
			Expect(c.errOut()).NotTo(ContainSubstring("pausing"))
			Expect(c.out()).To(BeEmpty())
			Expect(defaultPath() + network.JournalSuffix).To(BeAnExistingFile())
		})
	})

	When("the crawl stops part-way", func() {
		refuseFRA := func(w http.ResponseWriter, r *http.Request, body []byte) {
			if r.URL.Path == "/api/facilities/fra-uuid/connections" {
				writeJSON(w, http.StatusForbidden, `[{"summary":"forbidden"}]`)
				return
			}
			networkTenant(w, r, body)
		}

		It("exits with the cause's code and says how to resume", func() {
			c.fakeTenant(refuseFRA)

			Expect(c.run("network", "snapshot", "--concurrency", "1")).To(Equal(exitcode.Forbidden))

			Expect(c.errOut()).To(ContainSubstring("--resume"))
			Expect(defaultPath() + network.JournalSuffix).To(BeAnExistingFile())
			Expect(defaultPath()).NotTo(BeAnExistingFile())
		})

		It("continues with --resume without reading again what it already has", func() {
			refusing := true
			api := c.fakeTenant(func(w http.ResponseWriter, r *http.Request, body []byte) {
				if refusing {
					refuseFRA(w, r, body)
					return
				}
				networkTenant(w, r, body)
			})
			Expect(c.run("network", "snapshot", "--concurrency", "1")).To(Equal(exitcode.Forbidden))
			before := len(api.recorded())

			refusing = false
			Expect(c.run("network", "snapshot", "--resume")).To(Equal(exitcode.OK))

			for _, call := range api.recorded()[before:] {
				Expect(call.Path).NotTo(Equal("/api/facilities/ber-uuid/connections"))
			}
			s, err := network.Load(defaultPath())
			Expect(err).NotTo(HaveOccurred())
			Expect(s.Edges).To(HaveLen(1))
			Expect(s.Stats.Resumed).To(Equal(1))
		})
	})

	DescribeTable("refuses a flag out of range before sending anything",
		func(args ...string) {
			api := c.fakeTenant(networkTenant)

			Expect(c.run(append([]string{"network", "snapshot"}, args...)...)).To(Equal(exitcode.Usage))

			Expect(api.recorded()).To(BeEmpty())
		},
		Entry("no concurrency", "--concurrency", "0"),
		Entry("more concurrency than the tenant should bear", "--concurrency", "9"),
		Entry("a negative number of decision logs", "--decision-logs", "-1"),
		Entry("more decision logs than one page holds", "--decision-logs", "251"),
	)
})

var _ = Describe("the read-only gate on a command that sends several operations", func() {
	var c *cli

	BeforeEach(func() { c = newCLI() })

	gated := func(ids string) *cobra.Command {
		return &cobra.Command{Use: "crawl", Annotations: map[string]string{annotationOperations: ids}}
	}

	It("lets it run when every operation is a read", func() {
		c.headless()
		c.setenv(config.EnvReadOnly, "1")
		Expect(c.deps.complete(newRootCmd(c.deps))).To(Succeed())

		Expect(c.deps.guard(gated("searchFacility,getFacilityConnections"), nil)).To(Succeed())
	})

	It("refuses it when any one of them writes", func() {
		c.headless()
		c.setenv(config.EnvReadOnly, "1")
		Expect(c.deps.complete(newRootCmd(c.deps))).To(Succeed())

		err := c.deps.guard(gated("searchFacility,addFacility"), nil)

		Expect(exitcode.FromError(err)).To(Equal(exitcode.ReadOnly))
	})

	It("refuses it when an operation is not in the spec, whatever the project", func() {
		err := c.deps.guard(gated("searchFacility,noSuchOperation"), nil)

		Expect(err).To(MatchError(ContainSubstring(`"noSuchOperation"`)))
	})
})

// gappySnapshot is a hub supplying eleven Berlin stores but BER-11, a supplier
// supplying all of them, and a connection to a facility that is offline.
func gappySnapshot() *network.Snapshot {
	s := &network.Snapshot{SchemaVersion: network.SchemaVersion, CreatedAt: time.Now().Add(-time.Hour)}
	add := func(label, typ, status string) {
		s.Facilities = append(s.Facilities, network.Facility{ID: "id-" + label, TenantFacilityID: label,
			Type: typ, Status: status, Country: "DE"})
	}
	add("HUB-01", network.TypeManaged, network.StatusOnline)
	add("SUP-01", network.TypeSupplier, network.StatusOnline)
	add("OLD-01", network.TypeManaged, "OFFLINE")
	for i := 1; i <= 11; i++ {
		add(fmt.Sprintf("BER-%02d", i), network.TypeManaged, network.StatusOnline)
	}
	s.Edges = []network.Edge{
		{ID: "h1", Source: "id-HUB-01", Target: network.Target{Type: network.TypeManaged, ExcludedFacilities: []string{"id-BER-11"}}},
		{ID: "s1", Source: "id-SUP-01", Target: network.Target{Type: network.TypeManaged}},
		{ID: "o1", Source: "id-HUB-01", Target: network.Target{Type: network.TypeManaged, FacilityRef: "id-OLD-01"}},
	}
	return s
}

func cleanSnapshot() *network.Snapshot {
	s := gappySnapshot()
	s.Edges = s.Edges[1:2]
	s.Edges = append(s.Edges, network.Edge{ID: "h1", Source: "id-HUB-01", Target: network.Target{Type: network.TypeManaged}})
	return s
}

var _ = Describe("fft network check", func() {
	var (
		c    *cli
		path string
	)

	BeforeEach(func() {
		c = newCLI()
		path = filepath.Join(GinkgoT().TempDir(), "s.json.gz")
	})

	save := func(s *network.Snapshot) {
		GinkgoHelper()
		Expect(network.Save(path, s)).To(Succeed())
	}

	It("prints the findings on stdout and exits 11", func() {
		save(gappySnapshot())

		Expect(c.run("network", "check", "--snapshot", path)).To(Equal(exitcode.ProblemsFound))

		lines := strings.Split(strings.TrimSpace(c.out()), "\n")
		Expect(strings.Fields(lines[0])).To(Equal([]string{"RULE", "SUBJECT", "PROFILES", "DETAIL"}))
		Expect(lines).To(HaveLen(3))
		Expect(lines[1]).To(MatchRegexp(`^peer-coverage\s+BER-11\s+\(no tags\)\s+no hub reaches it, though one reaches 90% of its peers in DE$`))
		Expect(lines[2]).To(MatchRegexp(`^offline-ref\s+OLD-01\s+-\s+it is offline, and 1 connection still leads? to it$`))
		Expect(c.errOut()).To(ContainSubstring("Error: the check found 2 problems"))
	})

	It("says on stderr what it assumed about the network", func() {
		save(gappySnapshot())

		c.run("network", "check", "--snapshot", path)

		Expect(c.errOut()).To(ContainSubstring("14 facilities, 3 connections"))
		Expect(c.errOut()).To(ContainSubstring("Hubs: 1, the facilities reaching 10 or more others"))
		Expect(c.errOut()).To(ContainSubstring("Peer groups, by country: DE 11"))
		Expect(c.errOut()).To(ContainSubstring("Coverage: a hub reaches 10 of 11 stores in DE (90%"))
	})

	It("gives each finding's evidence and next step under -o json", func() {
		save(gappySnapshot())

		Expect(c.run("network", "check", "--snapshot", path, "-o", "json")).To(Equal(exitcode.ProblemsFound))

		var findings []map[string]any
		Expect(json.Unmarshal([]byte(c.out()), &findings)).To(Succeed())
		Expect(findings).To(HaveLen(2))
		Expect(findings[0]).To(HaveKeyWithValue("rule", "peer-coverage"))
		Expect(findings[0]).To(HaveKeyWithValue("subject", "BER-11"))
		Expect(findings[0]).To(HaveKey("evidence"))
		Expect(findings[0]).To(HaveKeyWithValue("next", ContainSubstring("Compare it with a peer")))
	})

	It("exits 0 with nothing on stdout when the network is clean", func() {
		save(cleanSnapshot())

		Expect(c.run("network", "check", "--snapshot", path)).To(Equal(exitcode.OK))

		Expect(c.out()).To(BeEmpty())
		Expect(c.errOut()).To(ContainSubstring("No problems found."))
	})

	It("prints an empty array for a clean network under -o json", func() {
		save(cleanSnapshot())

		Expect(c.run("network", "check", "--snapshot", path, "-o", "json")).To(Equal(exitcode.OK))

		Expect(strings.TrimSpace(c.out())).To(Equal("[]"))
	})

	It("runs only the rules named with --rule", func() {
		save(gappySnapshot())

		Expect(c.run("network", "check", "--snapshot", path, "--rule", "offline-ref")).To(Equal(exitcode.ProblemsFound))

		Expect(c.out()).NotTo(ContainSubstring("peer-coverage"))
		Expect(c.out()).To(ContainSubstring("offline-ref"))
	})

	It("refuses a rule it does not have, naming the ones it does", func() {
		save(gappySnapshot())

		Expect(c.run("network", "check", "--snapshot", path, "--rule", "no-sources")).To(Equal(exitcode.Usage))

		Expect(c.errOut()).To(ContainSubstring(`unknown --rule "no-sources"`))
		Expect(c.errOut()).To(ContainSubstring("no-source, peer-coverage"))
	})

	It("reads the snapshot fft network snapshot wrote for the project when --snapshot is not given", func() {
		c.fakeTenant(networkTenant)
		Expect(c.run("network", "snapshot", "-o", "json")).To(Equal(exitcode.OK))
		var sum map[string]any
		Expect(json.Unmarshal([]byte(c.out()), &sum)).To(Succeed())

		Expect(c.run("network", "check")).To(BeElementOf(exitcode.OK, exitcode.ProblemsFound))

		Expect(c.errOut()).To(ContainSubstring("Snapshot " + sum["path"].(string)))
	})

	It("exits 3 and says how to take one when there is no snapshot", func() {
		Expect(c.run("network", "check", "--snapshot", path)).To(Equal(exitcode.Config))

		Expect(c.errOut()).To(ContainSubstring("fft network snapshot"))
	})

	It("sends no request", func() {
		save(gappySnapshot())
		api := c.fakeTenant(networkTenant)

		c.run("network", "check", "--snapshot", path)

		Expect(api.recorded()).To(BeEmpty())
	})
})

var _ = Describe("fft network infer and config", func() {
	var (
		c        *cli
		dir      string
		snapshot string
		config   string
	)

	BeforeEach(func() {
		c = newCLI()
		dir = GinkgoT().TempDir()
		snapshot = filepath.Join(dir, "s.json.gz")
		config = filepath.Join(dir, network.DefaultConfigFile)
		Expect(network.Save(snapshot, gappySnapshot())).To(Succeed())
	})

	write := func(text string) {
		GinkgoHelper()
		Expect(os.WriteFile(config, []byte(text), 0o600)).To(Succeed())
	}

	Describe("infer", func() {
		It("writes a config the check then follows, finding the same gap as an expectation", func() {
			Expect(c.run("network", "infer", "--snapshot", snapshot, "--out", config)).To(Equal(exitcode.OK))
			Expect(c.errOut()).To(ContainSubstring("Wrote " + config))

			Expect(c.run("network", "check", "--snapshot", snapshot, "--config", config)).To(Equal(exitcode.ProblemsFound))

			Expect(c.out()).To(MatchRegexp(`(?m)^expect\s+BER-11\s+.*hub-coverage-de: 0 hubs reach it, and at least 1 should$`))
			Expect(c.errOut()).To(ContainSubstring("Hubs: 1, as the config pins them."))
			Expect(c.errOut()).To(ContainSubstring("Config " + config + ": 1 expectation, 0 suppressions in force."))
		})

		It("will not replace a file without --force", func() {
			write("version: 1\n")

			Expect(c.run("network", "infer", "--snapshot", snapshot, "--out", config)).To(Equal(exitcode.Usage))
			Expect(c.errOut()).To(ContainSubstring("pass --force"))

			Expect(c.run("network", "infer", "--snapshot", snapshot, "--out", config, "--force")).To(Equal(exitcode.OK))
		})

		It("prints the proposal on stdout with --out -", func() {
			Expect(c.run("network", "infer", "--snapshot", snapshot, "--out", "-")).To(Equal(exitcode.OK))

			Expect(c.out()).To(HavePrefix("# fft-network.yaml"))
			_, err := network.ParseConfig([]byte(c.out()), "stdout")
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("check with a config", func() {
		It("does not report a finding the config suppresses, and counts it on stderr", func() {
			write("version: 1\nsuppress:\n" +
				"  - {rule: offline-ref, facility: OLD-01, reason: reopens in spring, until: 2099-03-01}\n" +
				"  - {rule: peer-coverage, facility: BER-11, reason: new store, until: 2099-03-01}\n")

			Expect(c.run("network", "check", "--snapshot", snapshot, "--config", config)).To(Equal(exitcode.OK))

			Expect(c.out()).To(BeEmpty())
			Expect(c.errOut()).To(ContainSubstring("Suppressed by the config: 2 findings."))
		})

		It("warns about an expired suppression, which suppresses nothing", func() {
			write("version: 1\nsuppress:\n" +
				"  - {rule: offline-ref, facility: OLD-01, reason: reopens in spring, until: 2020-03-01}\n")

			Expect(c.run("network", "check", "--snapshot", snapshot, "--config", config)).To(Equal(exitcode.ProblemsFound))

			Expect(c.errOut()).To(ContainSubstring("expired on 2020-03-01"))
			Expect(c.out()).To(ContainSubstring("offline-ref"))
		})

		It("exits 3 for a config it cannot use, naming the line", func() {
			write("version: 1\nexpect:\n  - name: x\n    for: {role: shop}\n    reachableFrom: {role: hub, atLeast: 1}\n")

			Expect(c.run("network", "check", "--snapshot", snapshot, "--config", config)).To(Equal(exitcode.Config))

			Expect(c.errOut()).To(ContainSubstring(`line 4: expect[0].for.role: must be one of hub, store, supplier, not "shop"`))
		})

		It("exits 3 when the config it was pointed at is not there", func() {
			Expect(c.run("network", "check", "--snapshot", snapshot, "--config", config)).To(Equal(exitcode.Config))

			Expect(c.errOut()).To(ContainSubstring("there is no config file there"))
		})
	})

	Describe("config validate", func() {
		It("exits 0 and prints nothing for a valid file", func() {
			write("version: 1\n")

			Expect(c.run("network", "config", "validate", "--config", config, "--snapshot", snapshot)).To(Equal(exitcode.OK))

			Expect(c.out()).To(BeEmpty())
			Expect(c.errOut()).To(ContainSubstring("is valid"))
		})

		It("lists every problem with its line, and exits 3", func() {
			write("version: 1\nroles:\n  hub:\n    facilities: [HUB-01, HUB-09]\nsuppress:\n" +
				"  - {rule: offline-ref, facility: OLD-01, reason: r, until: 2020-03-01}\n")

			Expect(c.run("network", "config", "validate", "--config", config, "--snapshot", snapshot, "-o", "json")).
				To(Equal(exitcode.Config))

			var got []map[string]any
			Expect(json.Unmarshal([]byte(c.out()), &got)).To(Succeed())
			Expect(got).To(ConsistOf(
				HaveKeyWithValue("message", "HUB-09 is not a facility in the snapshot"),
				HaveKeyWithValue("message", ContainSubstring("expired on 2020-03-01")),
			))
			Expect(got[0]).To(HaveKeyWithValue("line", BeNumerically("==", 4)))
		})

		It("still checks the file itself when there is no snapshot", func() {
			write("version: 3\n")

			Expect(c.run("network", "config", "validate", "--config", config)).To(Equal(exitcode.Config))

			Expect(c.out()).To(MatchRegexp(`(?m)^1\s+version\s+must be 1`))
		})
	})

	It("prints the JSON Schema", func() {
		Expect(c.run("network", "config", "schema")).To(Equal(exitcode.OK))

		var schema map[string]any
		Expect(json.Unmarshal([]byte(c.out()), &schema)).To(Succeed())
		Expect(schema).To(HaveKeyWithValue("title", "fft-network.yaml"))
	})
})
