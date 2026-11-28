package network

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"github.com/Joessst-Dev/fft-cli/internal/shellquote"
)

// KindAny is a coverage matrix's column for every kind of source at once: the
// view of the no-source rule, where the others are the view of peer-coverage.
const KindAny Kind = "any"

// MaxMissing is how many unreached stores a coverage cell names. A country
// a hub misses entirely would otherwise put thousands of labels in every cell
// of its row, and a report is opened in a browser, not paged through.
const MaxMissing = 200

// View is what `fft network report` draws, worked out in full here: the page
// only lays it out, so the picture cannot disagree with the check that the
// same snapshot and config give.
//
// It holds no wall-clock time. The same snapshot and config give the same
// view, byte for byte, so two reports can be diffed.
type View struct {
	Project    string    `json:"project"`
	SnapshotAt time.Time `json:"snapshotAt"`
	FFTVersion string    `json:"fftVersion"`

	Counts  ViewCounts  `json:"counts"`
	Decided ViewDecided `json:"decided"`

	// Peers are the matrix's rows and Profiles its columns, in the order the
	// check uses: largest group first, most common kind of order first.
	Peers    []ViewPeer    `json:"peers"`
	Profiles []ViewProfile `json:"profiles"`
	// Coverage is one matrix per kind of source, hubs first.
	Coverage []CoverageMatrix `json:"coverage"`

	// Findings counts the check's findings by rule; Suppressed is how many a
	// suppression accepted, shown so that a report cannot hide them either.
	Findings   map[string]int `json:"findings"`
	Suppressed int            `json:"suppressed"`

	// Facilities describes every facility the view names, by label.
	Facilities map[string]ViewFacility `json:"facilities"`

	// Neighbourhoods explains, for the facilities a reader is likely to ask
	// about, which connections lead there and why the others do not: the
	// --facility ones, every finding's subject, then the stores the coverage
	// cells name. Omitted counts the ones MaxFacilities left out; 'fft network
	// reach' explains any of them.
	Neighbourhoods map[string]Neighbourhood `json:"neighbourhoods"`
	Omitted        int                      `json:"omitted,omitempty"`
}

// Neighbourhood is every connection that could lead to one facility, counted
// into bundles: Profiles[k] for the kind of order View.Profiles[k].
type Neighbourhood struct {
	// Role is the facility's own role as a source, empty when it is none.
	Role     Kind       `json:"role,omitempty"`
	Profiles [][]Bundle `json:"profiles"`
}

// ViewCounts is the size of the network, and of each role in it.
type ViewCounts struct {
	Facilities  int `json:"facilities"`
	Online      int `json:"online"`
	Connections int `json:"connections"`
	Hubs        int `json:"hubs"`
	Stores      int `json:"stores"`
	Suppliers   int `json:"suppliers"`
}

// ViewDecided is what the check assumed, which every number in the view rests
// on: which facilities are hubs, how stores are grouped, which kinds of order
// there are.
type ViewDecided struct {
	// Hubs names the hubs, up to MaxMissing of them.
	Hubs    []string `json:"hubs"`
	HubCut  int      `json:"hubCut,omitempty"`
	HubJump float64  `json:"hubJump,omitempty"`
	PeerBy  string   `json:"peerBy"`
	// FromFile says a config file decided some of it, rather than inference
	// alone.
	FromFile bool `json:"fromFile"`
}

// ViewPeer is one row of the coverage matrix: a peer group of stores.
type ViewPeer struct {
	Key  string `json:"key"`
	Size int    `json:"size"`
}

// ViewProfile is one column of the coverage matrix: a kind of order, and how
// many routed orders in the snapshot carried it.
type ViewProfile struct {
	Name   string     `json:"name"`
	Tags   []TagValue `json:"tags"`
	Orders int        `json:"orders"`
	// ReachFlags is the --tag flags that put this kind of order to 'fft network
	// reach', shell-quoted: a tag value is tenant text, and the page shows this
	// line for a user to paste. Unportable is set when no quoting keeps a value
	// intact in every shell.
	ReachFlags string `json:"reachFlags"`
	Unportable bool   `json:"unportable,omitempty"`
}

// CoverageMatrix is how many of each peer group's stores one kind of source
// reaches, for each kind of order. Cells is indexed [peer][profile], in the
// order of [View.Peers] and [View.Profiles].
type CoverageMatrix struct {
	Kind  Kind     `json:"kind"`
	Cells [][]Cell `json:"cells"`
}

// Cell is one peer group and one kind of order.
type Cell struct {
	// Have is how many of the group's stores a source reaches whatever the
	// order's items and recipient; Conditional how many more one reaches only
	// for some.
	Have        int `json:"have"`
	Conditional int `json:"conditional,omitempty"`
	// Missing names the stores Have leaves out, up to MaxMissing; More counts
	// the rest.
	Missing []string `json:"missing,omitempty"`
	More    int      `json:"more,omitempty"`
	// Flagged is how many of the missing stores the check reported for this
	// kind of source and order. A cell can miss stores no finding names: a gap
	// most of a group shares is no outlier, and the matrix is what shows it.
	Flagged int `json:"flagged,omitempty"`
}

// ViewFacility describes one facility the view names.
type ViewFacility struct {
	Name    string `json:"name,omitempty"`
	Type    string `json:"type"`
	Status  string `json:"status"`
	Country string `json:"country,omitempty"`
	// Rules are the rules with a finding about it, in the check's rule order.
	Rules []string `json:"rules,omitempty"`
	// ReachArg is the label as 'fft network reach --to' takes it, shell-quoted
	// for the line the page offers to paste: a facility id is tenant text.
	// Unportable is set when no quoting keeps it intact in every shell.
	ReachArg   string `json:"reachArg"`
	Unportable bool   `json:"unportable,omitempty"`
}

// DefaultMaxFacilities is how many neighbourhoods a view explains unless told
// otherwise. Each is a few kilobytes, and a report is one file a browser loads.
const DefaultMaxFacilities = 500

// ViewOptions is what a view needs that the network does not hold.
type ViewOptions struct {
	// FFTVersion is the version of fft that draws the report.
	FFTVersion string
	// Facilities are explained first, whatever the check found: the ones the
	// reader asked for, by index.
	Facilities []int
	// MaxFacilities caps the neighbourhoods; zero or less is
	// DefaultMaxFacilities.
	MaxFacilities int
}

// View runs the check and works out everything a report draws.
func (n *Network) View(cfg Config, opts ViewOptions) View {
	r := n.Check(cfg)
	s := n.snap

	v := View{
		Project:    s.Source.Project,
		SnapshotAt: s.CreatedAt,
		FFTVersion: opts.FFTVersion,
		Counts: ViewCounts{
			Facilities:  len(s.Facilities),
			Connections: len(s.Edges),
			Hubs:        r.Roles.Hubs.Count(),
			Stores:      r.Roles.Stores.Count(),
			Suppliers:   r.Roles.Suppliers.Count(),
		},
		Decided: ViewDecided{
			Hubs:     []string{},
			HubCut:   r.Roles.HubCut,
			HubJump:  r.Roles.HubJump,
			PeerBy:   cmp.Or(cfg.PeerBy, DefaultConfig().PeerBy),
			FromFile: cfg.FromFile,
		},
		Peers:          make([]ViewPeer, len(r.Peers)),
		Profiles:       make([]ViewProfile, len(r.Profiles)),
		Coverage:       []CoverageMatrix{},
		Findings:       map[string]int{},
		Suppressed:     len(r.Suppressed),
		Facilities:     map[string]ViewFacility{},
		Neighbourhoods: map[string]Neighbourhood{},
	}
	for i := range s.Facilities {
		if s.Facilities[i].Online() {
			v.Counts.Online++
		}
	}
	name := func(i int) string {
		f := n.Facility(i)
		if _, ok := v.Facilities[f.Label()]; !ok {
			arg, portable := shellquote.Quote(f.Label())
			v.Facilities[f.Label()] = ViewFacility{
				Name: f.Name, Type: f.Type, Status: f.Status, Country: f.Country,
				ReachArg: arg, Unportable: !portable,
			}
		}
		return f.Label()
	}
	r.Roles.Hubs.Each(func(i int) {
		if len(v.Decided.Hubs) < MaxMissing {
			v.Decided.Hubs = append(v.Decided.Hubs, name(i))
		}
	})
	for i, pg := range r.Peers {
		v.Peers[i] = ViewPeer{Key: pg.Key, Size: pg.Size}
	}
	for i, pc := range r.Profiles {
		tags := pc.Profile.Tags
		if tags == nil {
			tags = []TagValue{}
		}
		flags, portable := reachFlags(tags)
		v.Profiles[i] = ViewProfile{Name: pc.Profile.String(), Tags: tags, Orders: pc.Orders, ReachFlags: flags, Unportable: !portable}
	}

	// flagged answers "did the check report this store, for this kind of
	// source and this kind of order?" A no-source finding is the any column's;
	// a peer-coverage finding names its kind in the evidence.
	type flag struct {
		kind    Kind
		label   string
		profile string
	}
	flagged := map[flag]bool{}
	for _, f := range r.Findings {
		v.Findings[f.Rule]++
		var kind Kind
		switch f.Rule {
		case RuleNoSource:
			kind = KindAny
		case RulePeerCoverage:
			k, _ := f.Evidence["kind"].(string)
			kind = Kind(k)
		default:
			continue
		}
		for _, p := range f.Profiles {
			flagged[flag{kind, f.Subject, p}] = true
		}
	}

	kinds := []Kind{}
	if !r.Roles.Hubs.Empty() {
		kinds = append(kinds, KindHub)
	}
	if !r.Roles.Suppliers.Empty() {
		kinds = append(kinds, KindSupplier)
	}
	kinds = append(kinds, KindAny)
	for _, k := range kinds {
		m := CoverageMatrix{Kind: k, Cells: make([][]Cell, len(r.Peers))}
		for i := range m.Cells {
			m.Cells[i] = make([]Cell, len(r.Profiles))
		}
		v.Coverage = append(v.Coverage, m)
	}

	groups := n.coverGroups(r.Roles)
	for pi, pc := range r.Profiles {
		pr := n.reachFor(groups, pc.Profile)
		profile := pc.Profile.String()
		for ki, k := range kinds {
			reach, conditional := pr.any(), pr.conditional
			if k != KindAny {
				reach, conditional = pr.reach[k], pr.conditionalBy[k]
			}
			for gi, pg := range r.Peers {
				c := &v.Coverage[ki].Cells[gi][pi]
				missing := pg.members.Clone()
				missing.AndNot(reach)
				c.Have = pg.Size - missing.Count()
				missing.Each(func(i int) {
					if conditional.Has(i) {
						c.Conditional++
					}
					label := n.Facility(i).Label()
					if flagged[flag{k, label, profile}] {
						c.Flagged++
					}
					if len(c.Missing) < MaxMissing {
						c.Missing = append(c.Missing, name(i))
					} else {
						c.More++
					}
				})
			}
		}
	}

	n.explain(&v, r, opts)

	// The findings come sorted by rule, so each facility's rules do too.
	for _, f := range r.Findings {
		vf, ok := v.Facilities[f.Subject]
		if !ok || slices.Contains(vf.Rules, f.Rule) {
			continue
		}
		vf.Rules = append(vf.Rules, f.Rule)
		v.Facilities[f.Subject] = vf
	}
	return v
}

// reachFlags spells tags as the --tag flags of 'fft network reach', each
// quoted, with a leading space apiece so the string appends to a command line.
func reachFlags(tags []TagValue) (flags string, portable bool) {
	var b strings.Builder
	portable = true
	for _, t := range tags {
		arg, ok := shellquote.Quote(t.ID + "=" + t.Value)
		b.WriteString(" --tag " + arg)
		portable = portable && ok
	}
	return b.String(), portable
}

// facilityRules are the rules whose subject is a facility a reader will want
// explained: one that is not reached, or one connections lead to in vain.
var facilityRules = []string{RuleNoSource, RulePeerCoverage, RuleExpect, RuleOfflineRef}

// explain fills the view's neighbourhoods: the facilities asked for, then the
// findings' subjects, then the stores the coverage cells name, up to the cap.
func (n *Network) explain(v *View, r Report, opts ViewOptions) {
	limit := opts.MaxFacilities
	if limit <= 0 {
		limit = DefaultMaxFacilities
	}
	seen := map[int]bool{}
	var chosen []int
	choose := func(i int) {
		if seen[i] {
			return
		}
		seen[i] = true
		if len(chosen) < limit {
			chosen = append(chosen, i)
		} else {
			v.Omitted++
		}
	}
	for _, i := range opts.Facilities {
		choose(i)
	}
	for _, f := range r.Findings {
		if !slices.Contains(facilityRules, f.Rule) {
			continue
		}
		if i, ok := n.Locate(f.Subject); ok {
			choose(i)
		}
	}
	for _, m := range v.Coverage {
		for _, row := range m.Cells {
			for _, c := range row {
				for _, label := range c.Missing {
					if i, ok := n.Locate(label); ok {
						choose(i)
					}
				}
			}
		}
	}

	classes := n.edgeClasses(r.Roles)
	for _, i := range chosen {
		f := n.Facility(i)
		role, _ := r.Roles.kind(i)
		nb := Neighbourhood{Role: role, Profiles: make([][]Bundle, len(r.Profiles))}
		for k, pc := range r.Profiles {
			nb.Profiles[k] = n.bundlesTo(classes, i, pc.Profile, r.Roles)
		}
		v.Neighbourhoods[f.Label()] = nb
		if _, ok := v.Facilities[f.Label()]; !ok {
			arg, portable := shellquote.Quote(f.Label())
			v.Facilities[f.Label()] = ViewFacility{
				Name: f.Name, Type: f.Type, Status: f.Status, Country: f.Country,
				ReachArg: arg, Unportable: !portable,
			}
		}
	}
}
