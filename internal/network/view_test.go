package network_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/network"
)

// europe is two hubs, a supplier, eleven Berlin stores and ten Vienna stores.
// HUB-01 serves every store unless the order is bulky; HUB-02 every store but
// BER-11, always. So a bulky order leaves BER-11 without a hub, which the check
// reports, and nothing else lacks one.
func europe() *world {
	w := newWorld().hub("HUB-01").hub("HUB-02").supplier("SUP-01").
		stores("BER", "DE", 11).stores("VIE", "AT", 10).
		edge("h1", "HUB-01", network.Target{Type: network.TypeManaged}, not(network.ContextTag, "bulky")).
		edge("h2", "HUB-02", except(network.TypeManaged, "BER-11")).
		edge("s1", "SUP-01", network.Target{Type: network.TypeManaged}).
		edge("st", "BER-01", network.Target{Type: network.TypeManaged, FacilityRef: id("BER-02")})
	w.s.OrderProfiles = []network.OrderProfile{
		{Tags: []network.TagValue{{ID: "orderType", Value: "standard"}}, Orders: 3},
		{Tags: []network.TagValue{{ID: "orderType", Value: "bulky"}}, Orders: 2},
	}
	return w
}

func (w *world) view() network.View {
	return network.New(&w.s).View(network.DefaultConfig(), network.ViewOptions{FFTVersion: "1.2.3"})
}

func matrixOf(v network.View, kind network.Kind) network.CoverageMatrix {
	GinkgoHelper()
	for _, m := range v.Coverage {
		if m.Kind == kind {
			return m
		}
	}
	Fail("no " + string(kind) + " matrix")
	return network.CoverageMatrix{}
}

func render(v network.View) string {
	GinkgoHelper()
	b, err := network.RenderReport(v)
	Expect(err).NotTo(HaveOccurred())
	return string(b)
}

var viewData = regexp.MustCompile(`(?s)<script type="application/json" id="view">(.*?)</script>`)

var _ = Describe("a network's view", func() {
	It("lays the groups out as rows and the kinds of order as columns, in the check's order", func() {
		v := europe().view()

		Expect(v.Peers).To(Equal([]network.ViewPeer{{Key: "DE", Size: 11}, {Key: "AT", Size: 10}}))
		Expect(v.Profiles).To(HaveLen(2))
		Expect(v.Profiles[0].Name).To(Equal("(no tags)"))
		Expect(v.Profiles[0].Orders).To(Equal(3))
		Expect(v.Profiles[1].Name).To(Equal("orderType=bulky"))
		Expect(v.Profiles[1].Tags).To(Equal([]network.TagValue{{ID: "orderType", Value: "bulky"}}))
	})

	It("counts, per group and kind of order, the stores a kind of source reaches, naming the rest", func() {
		v := europe().view()
		hub := matrixOf(v, network.KindHub)

		Expect(hub.Cells[0][0]).To(Equal(network.Cell{Have: 11}))
		Expect(hub.Cells[0][1]).To(Equal(network.Cell{Have: 10, Missing: []string{"BER-11"}, Flagged: 1}))
		Expect(hub.Cells[1][1]).To(Equal(network.Cell{Have: 10}))
		Expect(v.Facilities).To(HaveKeyWithValue("BER-11", network.ViewFacility{
			Type: network.TypeManaged, Status: network.StatusOnline, Country: "DE",
			Rules: []string{network.RulePeerCoverage}, ReachArg: "BER-11"}))
	})

	It("has a column for any source at all, which the supplier fills here", func() {
		v := europe().view()

		Expect(v.Coverage).To(HaveLen(3))
		Expect(v.Coverage[2].Kind).To(Equal(network.KindAny))
		Expect(v.Coverage[2].Cells[0][1]).To(Equal(network.Cell{Have: 11}))
	})

	It("agrees with the check: each group's worst cell is the coverage the check reports", func() {
		w := europe()
		r := network.New(&w.s).Check(network.DefaultConfig())
		v := w.view()

		Expect(r.Coverage).NotTo(BeEmpty())
		for _, c := range r.Coverage {
			m := matrixOf(v, c.Kind)
			gi := -1
			for i, p := range v.Peers {
				if p.Key == c.Group {
					gi = i
				}
			}
			Expect(gi).NotTo(Equal(-1), c.Group)
			worst := c.Size
			for _, cell := range m.Cells[gi] {
				worst = min(worst, cell.Have)
			}
			Expect(worst).To(Equal(c.Have), "%s %s", c.Kind, c.Group)
		}
	})

	It("names at most MaxMissing stores in a cell and counts the rest", func() {
		w := newWorld().stores("PAR", "FR", network.MaxMissing+5)
		v := w.view()
		cell := matrixOf(v, network.KindAny).Cells[0][0]

		Expect(cell.Have).To(BeZero())
		Expect(cell.Missing).To(HaveLen(network.MaxMissing))
		Expect(cell.More).To(Equal(5))
		Expect(cell.Flagged).To(Equal(network.MaxMissing+5), "every store is a no-source finding")
	})

	It("counts the findings by rule, and the suppressed ones", func() {
		v := europe().view()

		Expect(v.Findings).To(Equal(map[string]int{network.RulePeerCoverage: 1}))
		Expect(v.Suppressed).To(BeZero())
	})
})

var _ = Describe("the command a report offers to paste", func() {
	// The page shows 'fft network reach ...' for every store a cell misses, for a
	// user to paste into a shell. A tenant id and a tag value are text a stranger
	// may have chosen, so Go quotes them and the page only joins what it is given.
	const (
		hostileID  = "BER-11;$(id)"
		hostileTag = "same day"
	)

	// flagsOf is the columns of w's view when the config declares these kinds of
	// order, by the column's name. BER-11 stays the store a bulky order misses.
	flagsOf := func(w *world, values ...string) map[string]network.ViewProfile {
		cfg := network.DefaultConfig()
		cfg.Declared = [][]network.TagValue{{{ID: "orderType", Value: "bulky"}}}
		for _, v := range values {
			cfg.Declared = append(cfg.Declared, []network.TagValue{{ID: "orderType", Value: v}})
		}
		byName := map[string]network.ViewProfile{}
		for _, p := range network.New(&w.s).View(cfg, network.ViewOptions{}).Profiles {
			byName[p.Name] = p
		}
		return byName
	}

	hostileWorld := func() *world {
		w := europe()
		for i := range w.s.Facilities {
			if w.s.Facilities[i].TenantFacilityID == "BER-11" {
				w.s.Facilities[i].TenantFacilityID = hostileID
			}
		}
		return w
	}

	It("carries a tenant id quoted, so a shell reads it as one argument", func() {
		v := hostileWorld().view()

		Expect(v.Facilities).To(HaveKeyWithValue(hostileID, HaveField("ReachArg", "'BER-11;$(id)'")))
		Expect(v.Facilities["HUB-01"].ReachArg).To(Equal("HUB-01"))
	})

	It("carries a tag value with a space quoted, as one --tag argument", func() {
		profiles := flagsOf(europe(), hostileTag)

		Expect(profiles["orderType=same day"].ReachFlags).To(Equal(" --tag 'orderType=same day'"))
		Expect(profiles["orderType=bulky"].ReachFlags).To(Equal(" --tag orderType=bulky"))
	})

	It("marks a value no quoting keeps intact in every shell", func() {
		profiles := flagsOf(europe(), "it's")

		Expect(profiles["orderType=it's"].Unportable).To(BeTrue())
		Expect(profiles["orderType=bulky"].Unportable).To(BeFalse())
	})

	It("is joined by the page from the quoted pieces, never from the raw text", func() {
		page := render(hostileWorld().view())

		Expect(page).NotTo(MatchRegexp(`"--tag " \+ t\.`))
		Expect(page).To(ContainSubstring("facility.reachArg + profile.reachFlags"))
	})
})

var _ = Describe("a view's neighbourhoods", func() {
	It("explains the facilities the findings name and the stores the cells list", func() {
		v := europe().view()

		Expect(v.Neighbourhoods).To(HaveLen(1))
		Expect(v.Neighbourhoods).To(HaveKey("BER-11"))
		Expect(v.Omitted).To(BeZero())
	})

	It("counts, for each kind of order, the connections into a facility by kind of source, result and reason", func() {
		nb := europe().view().Neighbourhoods["BER-11"]

		Expect(nb.Role).To(Equal(network.KindStore))
		bulky := nb.Profiles[1]
		Expect(bulky).To(HaveLen(3))
		Expect(bulky[0]).To(Equal(network.Bundle{Kind: network.KindSupplier, Result: network.ResultApplies,
			Reason: "every context entry passes", Count: 1, Sources: []string{"SUP-01"}, SourceCount: 1,
			Connections: []string{"s1"}}))
		Expect([]string{bulky[1].Reason, bulky[2].Reason}).To(ConsistOf(
			"TAG_REFERENCE NOT: the order carries bulky", "the target's excludedFacilityRefs lists it"))
		Expect(bulky[1].Kind).To(Equal(network.KindHub))
		Expect(bulky[1].Result).To(Equal(network.ResultExcluded))
	})

	It("adds up to what reach lists, row for row, for every facility and kind of order", func() {
		w := europe()
		n := network.New(&w.s)
		cfg := network.DefaultConfig()
		r := n.Check(cfg)
		all := []int{}
		for i := range n.Len() {
			all = append(all, i)
		}
		v := n.View(cfg, network.ViewOptions{Facilities: all, MaxFacilities: n.Len()})

		Expect(v.Neighbourhoods).To(HaveLen(n.Len()))
		type key struct {
			kind           network.Kind
			result, reason string
		}
		for i := range n.Len() {
			label := n.Facility(i).Label()
			for k, pc := range r.Profiles {
				want := map[key]int{}
				for _, row := range n.ReachTo(i, pc.Profile, r.Roles) {
					want[key{row.Kind, row.Result, network.BundleReason(row.Reason)}]++
				}
				got := map[key]int{}
				for _, b := range v.Neighbourhoods[label].Profiles[k] {
					got[key{b.Kind, b.Result, b.Reason}] += b.Count
				}
				Expect(got).To(Equal(want), "%s for %s", label, pc.Profile)
			}
		}
	})

	It("explains the facilities asked for first, and counts the ones the cap leaves out", func() {
		w := europe()
		n := network.New(&w.s)
		vie, _ := n.Locate("VIE-01")
		v := n.View(network.DefaultConfig(), network.ViewOptions{Facilities: []int{vie}, MaxFacilities: 1})

		Expect(v.Neighbourhoods).To(HaveLen(1))
		Expect(v.Neighbourhoods).To(HaveKey("VIE-01"))
		Expect(v.Omitted).To(Equal(1))
		Expect(v.Facilities).To(HaveKey("VIE-01"))
	})

	It("names at most MaxBundleSources sources in a bundle and counts them all", func() {
		w := newWorld().hub("HUB-01").stores("BER", "DE", network.MaxBundleSources+5)
		for i := 1; i <= network.MaxBundleSources+5; i++ {
			w.edge(fmt.Sprintf("e%02d", i), fmt.Sprintf("BER-%02d", i), network.Target{Type: network.TypeManaged},
				in(network.ContextFacility, id("BER-01")))
		}
		n := network.New(&w.s)
		target, _ := n.Locate("BER-02")
		v := n.View(network.DefaultConfig(), network.ViewOptions{Facilities: []int{target}})

		b := v.Neighbourhoods["BER-02"].Profiles[0][0]
		Expect(b.Reason).To(Equal("FACILITY does not list it"))
		Expect(b.Count).To(Equal(network.MaxBundleSources + 4))
		Expect(b.SourceCount).To(Equal(network.MaxBundleSources + 4))
		Expect(b.Sources).To(HaveLen(network.MaxBundleSources))
	})
})

var _ = Describe("a rendered report", func() {
	It("is the same, byte for byte, for the same snapshot", func() {
		Expect(render(europe().view())).To(Equal(render(europe().view())))
	})

	It("loads nothing from anywhere", func() {
		// The SVG namespace is a name, not an address: nothing is fetched from it.
		page := strings.ReplaceAll(render(europe().view()), `"http://www.w3.org/2000/svg"`, "")

		Expect(page).NotTo(ContainSubstring("http://"))
		Expect(page).NotTo(ContainSubstring("https://"))
		Expect(page).To(ContainSubstring(`http-equiv="Content-Security-Policy"`))
		Expect(network.ReportCSP()).To(HavePrefix("default-src 'none';"))
	})

	It("allows exactly the script and the styles it carries", func() {
		page := render(europe().view())
		hash := func(s string) string {
			sum := sha256.Sum256([]byte(s))
			return "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
		}
		csp := html.UnescapeString(regexp.MustCompile(`Content-Security-Policy" content="([^"]*)"`).FindStringSubmatch(page)[1])
		script := regexp.MustCompile(`(?s)<script>(.*?)</script>`).FindStringSubmatch(page)[1]
		style := regexp.MustCompile(`(?s)<style>(.*?)</style>`).FindStringSubmatch(page)[1]

		Expect(csp).To(Equal(network.ReportCSP()))
		Expect(csp).To(ContainSubstring("script-src " + hash(script) + ";"))
		Expect(csp).To(ContainSubstring("style-src " + hash(style) + ";"))
	})

	It("carries the view as data, which a facility's name cannot break out of", func() {
		const hostile = `</script><img src=x onerror=alert(1)><!--`
		w := europe()
		for i := range w.s.Facilities {
			if w.s.Facilities[i].TenantFacilityID == "BER-11" {
				w.s.Facilities[i].Name = hostile
			}
		}
		page := render(w.view())

		Expect(strings.Count(page, "</script>")).To(Equal(2), "only the page's own script elements close")
		Expect(page).NotTo(ContainSubstring("<img"))
		var v network.View
		Expect(json.Unmarshal([]byte(viewData.FindStringSubmatch(page)[1]), &v)).To(Succeed())
		Expect(v.Facilities["BER-11"].Name).To(Equal(hostile))
	})
})
