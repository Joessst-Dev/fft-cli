package network_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/api"
	"github.com/Joessst-Dev/fft-cli/internal/client"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/network"
	"github.com/Joessst-Dev/fft-cli/internal/testsupport"
)

// fakeTenant serves every endpoint a crawl reads, from the specs' network, and
// records what it was sent. fail, when set, answers a request instead.
type fakeTenant struct {
	*httptest.Server

	mu   sync.Mutex
	sent []string
	fail func(r *http.Request) (int, bool)
}

func (t *fakeTenant) requests() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.sent...)
}

// connectionsOf is how many connection walks the tenant served for a facility.
func (t *fakeTenant) connectionsOf(id string) int {
	n := 0
	for _, r := range t.requests() {
		if r == "GET /api/facilities/"+id+"/connections" {
			n++
		}
	}
	return n
}

func rawFacility(id, tenantID, typ, status string) map[string]any {
	return map[string]any{"id": id, "tenantFacilityId": tenantID, "type": typ, "status": status,
		"address": map[string]any{"country": "DE", "postalCode": "10115"}, "version": 3}
}

func rawConnection(id, source string, target map[string]any, context ...map[string]any) map[string]any {
	c := map[string]any{"id": id, "sourceFacilityRef": source, "type": target["type"], "target": target,
		"fallbackTransitTime": map[string]any{"minTransitDays": 1, "maxTransitDays": 2}}
	if len(context) > 0 {
		c["context"] = context
	}
	return c
}

func newFakeTenant() *fakeTenant {
	t := &fakeTenant{}
	stores := []any{berlinGroup}
	facilities := []any{
		rawFacility(hubID, "HUB-01", network.TypeManaged, network.StatusOnline),
		rawFacility(ber1ID, "BER-01", network.TypeManaged, network.StatusOnline),
		rawFacility(ber2ID, "BER-02", network.TypeManaged, network.StatusOnline),
		rawFacility(offID, "OFF-01", network.TypeManaged, "OFFLINE"),
	}
	shared := map[string]any{"type": network.ContextFacilityGroup, "values": stores}
	connections := map[string][]any{
		hubID: {
			rawConnection("c1", hubID, map[string]any{"type": network.TypeManaged}, shared,
				map[string]any{"type": network.ContextTag, "operator": "NOT", "values": []any{"bulky"}}),
			rawConnection("c2", hubID, map[string]any{"type": network.TypeManaged, "facilityRef": ber2ID}),
		},
		ber1ID: {
			rawConnection("c3", ber1ID, map[string]any{"type": network.TypeCustomer}, shared),
		},
	}

	t.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.mu.Lock()
		t.sent = append(t.sent, r.Method+" "+r.URL.Path)
		fail := t.fail
		t.mu.Unlock()

		if fail != nil {
			if status, ok := fail(r); ok {
				w.WriteHeader(status)
				fmt.Fprint(w, `[{"summary":"refused"}]`)
				return
			}
		}

		answer := func(v any) {
			w.Header().Set("Content-Type", "application/json")
			Expect(json.NewEncoder(w).Encode(v)).To(Succeed())
		}
		last := map[string]any{"hasNextPage": false}
		path := r.URL.Path
		switch {
		case r.Method == http.MethodPost && path == "/api/facilities/search":
			answer(map[string]any{"facilities": facilities, "pageInfo": last})
		case r.Method == http.MethodGet && strings.HasSuffix(path, "/connections"):
			id := strings.Split(path, "/")[3]
			cs := connections[id]
			if r.URL.Query().Get("startAfterId") != "" {
				cs = nil
			}
			answer(map[string]any{"interFacilityConnections": append([]any{}, cs...), "total": len(connections[id])})
		case r.Method == http.MethodPost && path == "/api/facilitygroups/search":
			answer(map[string]any{"facilityGroups": []any{
				map[string]any{"id": berlinGroup, "name": "Berlin", "facilityRefs": []any{ber1ID, ber2ID}},
			}, "pageInfo": last})
		case r.Method == http.MethodGet && path == "/api/tags":
			answer(map[string]any{"tags": []any{map[string]any{"id": "orderType", "allowedValues": []any{"bulky", "express"}}}, "total": 1})
		case r.Method == http.MethodGet && path == "/api/routing/strategies":
			answer(map[string]any{"routingStrategies": []any{map[string]any{"id": "s1", "inUse": true, "rootNode": map[string]any{}}}, "total": 1})
		case r.Method == http.MethodGet && path == "/api/routing/decisionlogs":
			log := func(id, order string) map[string]any {
				return map[string]any{
					"id": id, "relatedRefs": map[string]any{"orderRef": order},
					"routingStrategyEvaluationResult": map[string]any{"evaluatedPath": []any{}},
					"sourcingOptions": []any{map[string]any{"transfers": []any{
						map[string]any{"facilityConnectionRef": "c1"}, map[string]any{"facilityConnectionRef": "c3"},
					}}},
				}
			}
			answer(map[string]any{"decisionLogs": []any{log("l1", "o1"), log("l2", "o2"), log("l3", "o3"), log("l4", "gone")}, "total": 4})
		case r.Method == http.MethodGet && path == "/api/orders/gone":
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `[{"summary":"not found"}]`)
		case r.Method == http.MethodGet && strings.HasPrefix(path, "/api/orders/"):
			value := "express"
			if path == "/api/orders/o3" {
				value = "bulky"
			}
			answer(map[string]any{"id": strings.TrimPrefix(path, "/api/orders/"),
				"tags": []any{map[string]any{"id": "orderType", "value": value}}})
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprintf(w, `[{"summary":"no route for %s %s"}]`, r.Method, path)
		}
	}))
	DeferCleanup(t.Close)
	return t
}

var _ = Describe("crawling a network", func() {
	var (
		ctx    context.Context
		tenant *fakeTenant
		out    string
		slept  []time.Duration
		src    network.Source
	)

	BeforeEach(func() {
		ctx = context.Background()
		tenant = newFakeTenant()
		out = filepath.Join(GinkgoT().TempDir(), "network", "spec.snapshot.json.gz")
		slept = nil
		src = network.Source{Project: "spec", BaseURL: tenant.URL}
	})

	newClient := func() *client.Client {
		c, err := client.New(tenant.URL, client.WithRetry(client.Retry{
			Sleep: func(context.Context, time.Duration) error { return nil },
		}))
		Expect(err).NotTo(HaveOccurred())
		return c
	}

	crawl := func(change ...func(*network.CrawlOptions)) (*network.Snapshot, error) {
		o := network.CrawlOptions{
			Source: src, FFTVersion: "v0.0.0-spec", Out: out,
			DecisionLogs: 250, Orders: true, Concurrency: 2,
			Progress: io.Discard,
			Sleep: func(_ context.Context, d time.Duration) error {
				slept = append(slept, d)
				return nil
			},
		}
		for _, f := range change {
			f(&o)
		}
		return network.Crawl(ctx, newClient(), o)
	}

	It("writes every facility, connection, group, tag, strategy and decision log to the snapshot", func() {
		s, err := crawl()
		Expect(err).NotTo(HaveOccurred())

		loaded, err := network.Load(out)
		Expect(err).NotTo(HaveOccurred())
		Expect(loaded.SchemaVersion).To(Equal(network.SchemaVersion))
		Expect(loaded.Source).To(Equal(src))
		Expect(loaded.Facilities).To(HaveLen(4))
		Expect(loaded.Facilities[1]).To(Equal(network.Facility{ID: ber1ID, TenantFacilityID: "BER-01",
			Type: network.TypeManaged, Status: network.StatusOnline, Country: "DE", PostalCode: "10115"}))
		Expect(loaded.Edges).To(HaveLen(3))
		Expect(loaded.Groups).To(HaveLen(1))
		Expect(loaded.Groups[0].Members).To(ConsistOf(ber1ID, ber2ID))
		Expect(loaded.Tags).To(HaveLen(1))
		Expect(loaded.Strategies).To(HaveLen(1))
		Expect(loaded.DecisionLogs).To(HaveLen(4))
		Expect(loaded.DecisionLogs[0].OrderRef).To(Equal("o1"))
		Expect(loaded.DecisionLogs[0].Connections).To(Equal([]string{"c1", "c3"}))
		Expect(s.Stats.Pauses).To(BeZero())
	})

	It("stores a context value list shared by several connections once", func() {
		s, err := crawl()
		Expect(err).NotTo(HaveOccurred())

		Expect(s.ValueSets).To(HaveLen(2))
		var c1, c3 network.Edge
		for _, e := range s.Edges {
			switch e.ID {
			case "c1":
				c1 = e
			case "c3":
				c3 = e
			}
		}
		Expect(c1.Source).To(Equal(hubID))
		Expect(c3.Source).To(Equal(ber1ID))
		Expect(c1.Context[0].Values).To(Equal(c3.Context[0].Values))
		Expect(c1.Context[1]).To(Equal(network.ContextEntry{Type: network.ContextTag, Operator: "NOT", Values: 1}))
	})

	It("counts the order profiles of the routed orders, most common first, skipping deleted orders", func() {
		s, err := crawl()
		Expect(err).NotTo(HaveOccurred())

		Expect(s.OrderProfiles).To(Equal([]network.OrderProfile{
			{Tags: []network.TagValue{{ID: "orderType", Value: "express"}}, Orders: 2},
			{Tags: []network.TagValue{{ID: "orderType", Value: "bulky"}}, Orders: 1},
		}))
	})

	It("walks offline facilities too, since connections may point at them", func() {
		_, err := crawl()
		Expect(err).NotTo(HaveOccurred())

		Expect(tenant.connectionsOf(offID)).To(Equal(1))
	})

	It("sends nothing but GETs and searches", func() {
		_, err := crawl()
		Expect(err).NotTo(HaveOccurred())

		for _, r := range tenant.requests() {
			method, path, _ := strings.Cut(r, " ")
			if method == http.MethodPost {
				Expect(path).To(HaveSuffix("/search"), r)
			} else {
				Expect(method).To(Equal(http.MethodGet), r)
			}
		}
	})

	It("skips the decision logs and the orders when asked to", func() {
		s, err := crawl(func(o *network.CrawlOptions) { o.DecisionLogs = 0 })
		Expect(err).NotTo(HaveOccurred())

		Expect(s.DecisionLogs).To(BeEmpty())
		Expect(tenant.requests()).NotTo(ContainElement(HavePrefix("GET /api/routing/decisionlogs")))
		Expect(tenant.requests()).NotTo(ContainElement(HavePrefix("GET /api/orders/")))
	})

	It("writes the snapshot for its owner only and removes the journal", func() {
		_, err := crawl()
		Expect(err).NotTo(HaveOccurred())

		testsupport.ExpectOwnerOnlyFile(out)
		Expect(out + network.JournalSuffix).NotTo(BeAnExistingFile())
	})

	When("a gateway throttles", func() {
		It("pauses, longer each time, and carries on", func() {
			// Six failures: the client's own three attempts, twice over, so the crawl
			// pauses twice before the seventh gets through.
			n := 0
			tenant.fail = func(r *http.Request) (int, bool) {
				if r.URL.Path == "/api/facilities/"+ber2ID+"/connections" && n < 6 {
					n++
					return http.StatusBadGateway, true
				}
				return 0, false
			}

			s, err := crawl(func(o *network.CrawlOptions) { o.Pause = time.Second })

			Expect(err).NotTo(HaveOccurred())
			Expect(slept).To(Equal([]time.Duration{time.Second, 2 * time.Second}))
			Expect(s.Stats.Pauses).To(Equal(2))
		})

		It("gives up after the last pause, keeps the journal and says how to resume", func() {
			tenant.fail = func(r *http.Request) (int, bool) {
				return http.StatusServiceUnavailable, r.URL.Path == "/api/facilities/"+ber2ID+"/connections"
			}

			_, err := crawl(func(o *network.CrawlOptions) { o.MaxPauses = 2; o.Concurrency = 1 })

			var crawlErr *network.CrawlError
			Expect(errors.As(err, &crawlErr)).To(BeTrue())
			Expect(crawlErr.Hint()).To(ContainSubstring("--resume"))
			Expect(exitcode.FromError(err)).To(Equal(exitcode.Unavailable))
			Expect(slept).To(HaveLen(2))
			Expect(out + network.JournalSuffix).To(BeAnExistingFile())
			Expect(out).NotTo(BeAnExistingFile())
		})
	})

	When("a facility is deleted after the facility search", func() {
		It("finishes without it: a 404 on its connections is a facility with none", func() {
			tenant.fail = func(r *http.Request) (int, bool) {
				return http.StatusNotFound, r.URL.Path == "/api/facilities/"+ber2ID+"/connections"
			}

			s, err := crawl()

			// The walk used to stop here with exit 6, though --resume would have
			// recovered it: a routine deletion should not need a re-run.
			Expect(err).NotTo(HaveOccurred())
			Expect(s.Edges).NotTo(BeEmpty())
			Expect(tenant.connectionsOf(hubID)).To(Equal(1))
			Expect(tenant.connectionsOf(ber1ID)).To(Equal(1))
			Expect(slept).To(BeEmpty())
			Expect(out + network.JournalSuffix).NotTo(BeAnExistingFile())
		})
	})

	When("the session is refused", func() {
		It("stops at once with the auth exit code, without pausing", func() {
			tenant.fail = func(r *http.Request) (int, bool) {
				return http.StatusUnauthorized, strings.HasSuffix(r.URL.Path, "/connections")
			}

			_, err := crawl()

			Expect(exitcode.FromError(err)).To(Equal(exitcode.Auth))
			Expect(slept).To(BeEmpty())
		})
	})

	Describe("resuming", func() {
		// interrupted runs a crawl in which BER-02's connections never arrive,
		// one facility at a time, so the journal holds exactly the ones before it.
		interrupted := func() {
			tenant.fail = func(r *http.Request) (int, bool) {
				return http.StatusForbidden, r.URL.Path == "/api/facilities/"+ber2ID+"/connections"
			}
			_, err := crawl(func(o *network.CrawlOptions) { o.Concurrency = 1 })
			Expect(exitcode.FromError(err)).To(Equal(exitcode.Forbidden))
			tenant.fail = nil
		}

		It("does not walk the facilities the journal already holds", func() {
			interrupted()

			s, err := crawl(func(o *network.CrawlOptions) { o.Resume = true })

			Expect(err).NotTo(HaveOccurred())
			Expect(tenant.connectionsOf(hubID)).To(Equal(1))
			Expect(tenant.connectionsOf(ber2ID)).To(Equal(2))
			Expect(s.Stats.Resumed).To(Equal(2))
			Expect(s.Edges).To(HaveLen(3))
			Expect(s.ValueSets).To(HaveLen(2))
		})

		It("starts over without --resume, and says it is discarding the journal", func() {
			interrupted()
			var progress strings.Builder

			s, err := crawl(func(o *network.CrawlOptions) { o.Progress = &progress })

			Expect(err).NotTo(HaveOccurred())
			Expect(tenant.connectionsOf(hubID)).To(Equal(2))
			Expect(s.Stats.Resumed).To(BeZero())
			Expect(progress.String()).To(ContainSubstring("starting over"))
		})

		It("survives a journal whose last line was cut short", func() {
			interrupted()
			journal := out + network.JournalSuffix
			f, err := os.OpenFile(journal, os.O_APPEND|os.O_WRONLY, 0o600)
			Expect(err).NotTo(HaveOccurred())
			_, err = f.WriteString(`{"facility":"` + ber2ID + `","edges":[{"id":"c`)
			Expect(err).NotTo(HaveOccurred())
			Expect(f.Close()).To(Succeed())

			s, err := crawl(func(o *network.CrawlOptions) { o.Resume = true })

			Expect(err).NotTo(HaveOccurred())
			Expect(s.Edges).To(HaveLen(3))
		})

		It("refuses a journal written for another tenant", func() {
			interrupted()
			src = network.Source{Project: "other", BaseURL: "https://other.example"}

			_, err := crawl(func(o *network.CrawlOptions) { o.Resume = true })

			Expect(err).To(MatchError(ContainSubstring(`written for project "spec"`)))
		})
	})
})

var _ = Describe("the operations a crawl sends", func() {
	It("are all reads, by the same classification the read-only gate uses", func() {
		for _, id := range network.CrawlOperations {
			op, ok := api.LookupOperation(id)
			Expect(ok).To(BeTrue(), "%s is not in the spec", id)
			Expect(op.Mutates()).To(BeFalse(), "%s writes", id)
		}
	})
})
