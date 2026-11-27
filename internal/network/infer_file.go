package network

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// InferFile writes a starter config file: every value a check would infer from
// the network, each with the reason it was chosen, as a comment above it.
//
// The point is to turn guesses into decisions. A value left in the file is
// pinned — a check follows it, and reports config-drift when the network moves
// away from it — and a value deleted is inferred again on every run, as it is
// without a file.
func (n *Network) InferFile(now time.Time) []byte {
	cfg := DefaultConfig()
	r := n.Check(Config{HubRatio: cfg.HubRatio, PeerBy: cfg.PeerBy, PeerMinSize: cfg.PeerMinSize,
		PeerShare: cfg.PeerShare, Rules: []string{RulePeerCoverage}})
	s := n.Snapshot()

	var b strings.Builder
	line := func(indent int, format string, args ...any) {
		b.WriteString(strings.Repeat("  ", indent) + fmt.Sprintf(format, args...) + "\n")
	}
	comment := func(indent int, format string, args ...any) {
		for l := range strings.SplitSeq(fmt.Sprintf(format, args...), "\n") {
			line(indent, "# %s", l)
		}
	}

	comment(0, "fft-network.yaml, written by 'fft network infer' on %s\n"+
		"from the snapshot of %s taken %s.\n"+
		"\n"+
		"Every value below is one the check inferred, with the reason above it. A value\n"+
		"left in is pinned: the check follows it, and reports config-drift when the\n"+
		"network moves away from it. A value deleted is inferred again on every run.\n"+
		"'fft network config schema' prints the whole format.",
		now.Format(time.DateOnly), s.Source.Project, s.CreatedAt.Format(time.DateOnly))
	line(0, "version: %d", ConfigVersion)
	b.WriteString("\n")

	line(0, "roles:")
	if hubs := r.Roles.Hubs.Count(); hubs > 0 {
		comment(1, "%d facilities' connections each reach %d or more online facilities,\n"+
			"%.1f× the next one down. Pinned, a hub that stops being one is config-drift.",
			hubs, r.Roles.HubCut, r.Roles.HubJump)
		line(1, "hub:")
		line(2, "facilities:")
		r.Roles.Hubs.Each(func(i int) { line(3, "- %s", quote(n.Facility(i).Label())) })
	} else {
		comment(1, "No facility's connections reach %g× more facilities than the next one's,\n"+
			"so the network has no hub tier. Chosen by reach until you pin some.", cfg.HubRatio)
		line(1, "hub:")
		line(2, "outDegree: %s", OutDegreeRelative)
	}
	b.WriteString("\n")

	groups := make([]string, 0, len(r.Peers))
	for _, g := range r.Peers {
		groups = append(groups, fmt.Sprintf("%s %d", g.Key, g.Size))
	}
	comment(0, "Stores are compared with the others in their country: %s.\n"+
		"Groups smaller than minSize are not compared; share is how many of a group\n"+
		"must have a kind of source before one without it is reported.", strings.Join(groups, ", "))
	line(0, "peers:")
	line(1, "by: %s", cfg.PeerBy)
	line(1, "minSize: %d", cfg.PeerMinSize)
	line(1, "share: %g", cfg.PeerShare)
	b.WriteString("\n")

	orders := 0
	names := make([]string, 0, len(r.Profiles))
	for _, p := range r.Profiles {
		orders += p.Orders
		names = append(names, p.Profile.String())
	}
	if orders > 0 {
		comment(0, "The kinds of order are taken from what was routed: %d profiles from %d\n"+
			"recent orders (%s). Declare them instead to check orders that have not\n"+
			"arrived yet; a declared list that misses a routed profile is config-drift.",
			len(r.Profiles), orders, strings.Join(names, "; "))
	} else {
		comment(0, "The snapshot has no routed orders, so each tag value a connection asks for\n"+
			"is one kind of order. Declare the real ones if you know them.")
	}
	line(0, "profiles:")
	line(1, "from: %s", ProfilesFromDecisionLogs)
	b.WriteString("\n")

	var expects []Coverage
	for _, c := range r.Coverage {
		// A group of stores with no value for the peer field cannot be selected by
		// it: a where on the placeholder would match no facility.
		if c.Group == noValue {
			continue
		}
		if c.Kind == KindHub && c.Size >= cfg.PeerMinSize && float64(c.Have)/float64(c.Size) >= cfg.PeerShare {
			expects = append(expects, c)
		}
	}
	if len(expects) > 0 {
		comment(0, "One expectation per country where a hub already reaches nearly every store.\n"+
			"The stores it misses today are its findings. Raise atLeast where stores\n"+
			"should have a second hub, and add maxTransitDays for a delivery promise.")
		line(0, "expect:")
		for _, c := range expects {
			comment(1, "Today a hub reaches %d of the %d stores in %s.", c.Have, c.Size, c.Group)
			line(1, "- name: %s", quote("hub-coverage-"+slug(c.Group)))
			line(2, "for:")
			line(3, "role: store")
			line(3, "where:")
			line(4, "%s: %s", cfg.PeerBy, quote(c.Group))
			line(2, "reachableFrom:")
			line(3, "role: hub")
			line(3, "atLeast: 1")
		}
		b.WriteString("\n")
	}

	comment(0, "A finding the team accepts for now: rule (or expectation name), the\n"+
		"finding's subject, why, and until when. An expired one suppresses nothing.\n"+
		"Only people add these; an agent may propose one, never add it to go green.")
	line(0, "suppress: []")
	return []byte(b.String())
}

var notSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slug(s string) string {
	return strings.Trim(notSlug.ReplaceAllString(strings.ToLower(s), "-"), "-")
}

// quote writes s as a YAML scalar, quoted only when it has to be.
func quote(s string) string {
	out, err := yaml.Marshal(s)
	if err != nil {
		return fmt.Sprintf("%q", s)
	}
	return strings.TrimSuffix(string(out), "\n")
}
