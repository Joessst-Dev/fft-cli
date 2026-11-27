package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

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
