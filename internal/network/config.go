package network

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/Joessst-Dev/fft-cli/internal/client"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

// ConfigVersion is the config file format this fft reads and writes.
const ConfigVersion = 1

// DefaultConfigFile is the file `fft network check` reads when --config names
// none: the one next to the code that is checked into the team's repository.
const DefaultConfigFile = "fft-network.yaml"

// maxConfig caps the config file. It is read from a working copy, which may be
// anyone's clone; a file of megabytes is not one a person wrote.
const maxConfig = 4 << 20

// File is fft-network.yaml: what a team has pinned about its network's shape,
// what it expects of it, and which findings it has accepted for now.
//
// Every field is optional. What the file does not say, the check infers, and
// says on stderr that it did.
type File struct {
	Version  int            `yaml:"version" json:"version"`
	Rules    []string       `yaml:"rules,omitempty" json:"rules,omitempty"`
	Roles    RoleSelectors  `yaml:"roles,omitempty" json:"roles,omitzero"`
	Peers    *PeerSettings  `yaml:"peers,omitempty" json:"peers,omitempty"`
	Profiles *ProfileSource `yaml:"profiles,omitempty" json:"profiles,omitempty"`
	Expect   []Expectation  `yaml:"expect,omitempty" json:"expect,omitempty"`
	Suppress []Suppression  `yaml:"suppress,omitempty" json:"suppress,omitempty"`
}

// RoleSelectors say which facilities are hubs, stores and suppliers. A nil
// selector means the default: hubs by relative reach, stores are the other
// online managed facilities, suppliers the online SUPPLIER facilities.
type RoleSelectors struct {
	Hub      *Selector `yaml:"hub,omitempty" json:"hub,omitempty"`
	Store    *Selector `yaml:"store,omitempty" json:"store,omitempty"`
	Supplier *Selector `yaml:"supplier,omitempty" json:"supplier,omitempty"`
}

// Selector picks facilities. Its fields combine with AND; an empty selector
// picks every facility.
type Selector struct {
	// Type is MANAGED_FACILITY or SUPPLIER.
	Type string `yaml:"type,omitempty" json:"type,omitempty"`
	// Facilities pins the selection to these facilities, by tenant id or
	// platform id.
	Facilities []string `yaml:"facilities,omitempty" json:"facilities,omitempty"`
	// Where matches facility fields: address.country, address.postalCode,
	// group (a facility group's id it belongs to), customAttributes.<key>.
	Where map[string]string `yaml:"where,omitempty" json:"where,omitempty"`
	// OutDegree "relative" selects hubs the way the heuristics do: those whose
	// connections reach MinJump times more facilities than the next one's. Only
	// a hub selector may say it.
	OutDegree string  `yaml:"outDegree,omitempty" json:"outDegree,omitempty"`
	MinJump   float64 `yaml:"minJump,omitempty" json:"minJump,omitempty"`
}

// OutDegreeRelative is the one OutDegree a selector may name.
const OutDegreeRelative = "relative"

// PeerSettings say which stores are compared with each other.
type PeerSettings struct {
	// By is a facility field: address.country (the default), address.postalCode
	// or customAttributes.<key>.
	By string `yaml:"by,omitempty" json:"by,omitempty"`
	// MinSize and Share are pointers so that an explicit 0 can be told from
	// "not set": a 0 silently replaced by the default would answer a question
	// the writer did not ask.
	MinSize *int     `yaml:"minSize,omitempty" json:"minSize,omitempty"`
	Share   *float64 `yaml:"share,omitempty" json:"share,omitempty"`
}

// ProfileSource says where the kinds of order come from.
type ProfileSource struct {
	// From is "decision-logs" (the default: the tags of the orders the
	// snapshot's decision logs routed) or "declared".
	From string `yaml:"from,omitempty" json:"from,omitempty"`
	// Declared are the profiles, each a map of tag id to value, when From is
	// "declared".
	Declared []map[string]string `yaml:"declared,omitempty" json:"declared,omitempty"`
}

// The profile sources.
const (
	ProfilesFromDecisionLogs = "decision-logs"
	ProfilesDeclared         = "declared"
)

// Expectation is an invariant the team declares: every store a selector picks
// is reachable from at least AtLeast sources of a role.
type Expectation struct {
	Name          string     `yaml:"name" json:"name"`
	For           ExpectFor  `yaml:"for" json:"for"`
	ReachableFrom ExpectFrom `yaml:"reachableFrom" json:"reachableFrom"`
}

// ExpectFor is which facilities an expectation is about.
type ExpectFor struct {
	Role  string            `yaml:"role" json:"role"`
	Where map[string]string `yaml:"where,omitempty" json:"where,omitempty"`
}

// ExpectFrom is what must reach them.
type ExpectFrom struct {
	Role    string `yaml:"role" json:"role"`
	AtLeast int    `yaml:"atLeast" json:"atLeast"`
	// MaxTransitDays, when set, counts only connections whose fallback transit
	// time is at most this many days. A connection without one does not count.
	MaxTransitDays *float64 `yaml:"maxTransitDays,omitempty" json:"maxTransitDays,omitempty"`
}

// Suppression accepts one finding until a date. It needs a reason, so the next
// person can judge whether it still holds, and an expiry, so that it is judged.
type Suppression struct {
	// Rule is a rule id, or the name of an expectation.
	Rule string `yaml:"rule" json:"rule"`
	// Facility is the finding's subject: a facility's tenant id, the reference
	// a dangling-ref finding names, or, for config-drift, an order profile
	// such as orderType=express.
	Facility string `yaml:"facility" json:"facility"`
	Reason   string `yaml:"reason" json:"reason"`
	Until    Date   `yaml:"until" json:"until"`
}

// Date is a calendar day, written 2027-01-15.
type Date struct{ time.Time }

// UnmarshalYAML reads a YYYY-MM-DD date.
func (d *Date) UnmarshalYAML(n *yaml.Node) error {
	t, err := time.Parse(time.DateOnly, n.Value)
	if err != nil {
		return fmt.Errorf("line %d: %q is not a date: write it as YYYY-MM-DD", n.Line, n.Value)
	}
	d.Time = t
	return nil
}

// Passed reports whether the day after d has begun on now's calendar, in now's
// own location. d is parsed as UTC midnight, so comparing it with the instant
// now would end a suppression at 16:00 on its last day west of UTC and an hour
// into the next day east of it; "through that day" is a calendar statement, and
// the caller's calendar is the one that counts.
func (d Date) Passed(now time.Time) bool {
	y, m, day := now.Date()
	return !time.Date(y, m, day, 0, 0, 0, 0, time.UTC).Before(d.AddDate(0, 0, 1))
}

// MarshalYAML writes the date as YYYY-MM-DD.
func (d Date) MarshalYAML() (any, error) { return d.Format(time.DateOnly), nil }

// MarshalJSON writes the date as YYYY-MM-DD.
func (d Date) MarshalJSON() ([]byte, error) { return json.Marshal(d.Format(time.DateOnly)) }

// Roles the config may name.
var roleNames = []string{string(KindHub), string(KindStore), string(KindSupplier)}

// Problem is one thing wrong with a config file.
type Problem struct {
	Line    int    `json:"line,omitempty"`
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
}

func (p Problem) String() string {
	var b strings.Builder
	if p.Line > 0 {
		fmt.Fprintf(&b, "line %d: ", p.Line)
	}
	if p.Path != "" {
		b.WriteString(p.Path)
		b.WriteString(": ")
	}
	b.WriteString(p.Message)
	return b.String()
}

// ConfigError is a config file that cannot be used. It exits 3, like any
// config fft cannot use.
type ConfigError struct {
	Path     string
	Problems []Problem
}

func (e *ConfigError) Error() string {
	parts := make([]string, len(e.Problems))
	for i, p := range e.Problems {
		parts[i] = p.String()
	}
	return fmt.Sprintf("%s: %s", e.Path, strings.Join(parts, "; "))
}

// ExitCode is 3: the config is unusable.
func (e *ConfigError) ExitCode() int { return exitcode.Config }

// Hint says where to look.
func (e *ConfigError) Hint() string {
	return "fft network config validate lists every problem; fft network config schema prints the format."
}

// LoadedFile is a config file and the YAML tree it was read from, which is what
// lets a problem found after decoding still name its line.
type LoadedFile struct {
	File
	Path string
	root *yaml.Node
}

// ReadConfig reads and checks a config file. A missing file is fs.ErrNotExist,
// so a caller can tell "there is no config" from "the config is wrong".
func ReadConfig(path string) (*LoadedFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	data, err := io.ReadAll(io.LimitReader(f, maxConfig+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if len(data) > maxConfig {
		return nil, &ConfigError{Path: path, Problems: []Problem{{Message: "the file is larger than 4 MiB"}}}
	}
	return ParseConfig(data, path)
}

// ParseConfig decodes a config file strictly and checks what it says. path
// names it in errors.
func ParseConfig(data []byte, path string) (*LoadedFile, error) {
	lf := &LoadedFile{Path: path}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, &ConfigError{Path: path, Problems: yamlProblems(err)}
	}
	lf.root = &root

	dec := yaml.NewDecoder(bytes.NewReader(data))
	// A field this fft does not know is a file written for another version, or a
	// typo — `suppres:` — that would otherwise switch a suppression off silently.
	dec.KnownFields(true)
	if err := dec.Decode(&lf.File); err != nil && !errors.Is(err, io.EOF) {
		return nil, &ConfigError{Path: path, Problems: yamlProblems(err)}
	}

	if problems := lf.Validate(); len(problems) > 0 {
		return nil, &ConfigError{Path: path, Problems: problems}
	}
	return lf, nil
}

var yamlLine = regexp.MustCompile(`^(?:yaml: )?line (\d+): (.*)$`)

// yamlProblems turns the YAML library's errors into problems with lines.
func yamlProblems(err error) []Problem {
	var te *yaml.TypeError
	msgs := []string{err.Error()}
	if errors.As(err, &te) {
		msgs = te.Errors
	}
	out := make([]Problem, 0, len(msgs))
	for _, m := range msgs {
		if g := yamlLine.FindStringSubmatch(m); g != nil {
			line, _ := strconv.Atoi(g[1])
			out = append(out, Problem{Line: line, Message: g[2]})
			continue
		}
		out = append(out, Problem{Message: strings.TrimPrefix(m, "yaml: ")})
	}
	return out
}

// roleSelectors lists the role selectors in a fixed order. Ranging over a map
// of them would make the order of the problems, and so of the message `check`
// prints, change from run to run.
func (lf *LoadedFile) roleSelectors() []struct {
	name string
	sel  *Selector
} {
	return []struct {
		name string
		sel  *Selector
	}{{"hub", lf.Roles.Hub}, {"store", lf.Roles.Store}, {"supplier", lf.Roles.Supplier}}
}

// sortProblems orders problems by line, then path, so the same file always
// gives the same report: two runs can be diffed.
func sortProblems(ps []Problem) {
	slices.SortStableFunc(ps, func(a, b Problem) int {
		return cmp.Or(a.Line-b.Line, strings.Compare(a.Path, b.Path))
	})
}

// Validate reports what is wrong with the file on its own, without a
// snapshot: values out of range, names that clash, a suppression with no
// reason. Expired suppressions are not among them — see [LoadedFile.Expired].
func (lf *LoadedFile) Validate() []Problem {
	var out []Problem
	bad := func(path, format string, args ...any) {
		out = append(out, Problem{Line: lf.line(path), Path: path, Message: fmt.Sprintf(format, args...)})
	}
	f := lf.File

	if f.Version != ConfigVersion {
		bad("version", "must be %d, the format this fft reads; got %d", ConfigVersion, f.Version)
	}
	for i, r := range f.Rules {
		if !slices.Contains(RuleIDs(), r) {
			bad(fmt.Sprintf("rules[%d]", i), "unknown rule %q: want one of %s", r, strings.Join(RuleIDs(), ", "))
		}
	}

	for _, r := range lf.roleSelectors() {
		name, sel := r.name, r.sel
		if sel == nil {
			continue
		}
		path := "roles." + name
		if sel.Type != "" && sel.Type != TypeManaged && sel.Type != TypeSupplier {
			bad(path+".type", "must be %s or %s, not %q", TypeManaged, TypeSupplier, sel.Type)
		}
		if sel.OutDegree != "" && sel.OutDegree != OutDegreeRelative {
			bad(path+".outDegree", "must be %q, not %q", OutDegreeRelative, sel.OutDegree)
		}
		if sel.OutDegree != "" && name != "hub" {
			bad(path+".outDegree", "only the hub role is chosen by reach")
		}
		if sel.OutDegree != "" && len(sel.Facilities) > 0 {
			bad(path, "pin the facilities or choose them by reach, not both")
		}
		if sel.MinJump != 0 && (sel.OutDegree == "" || sel.MinJump < 1) {
			bad(path+".minJump", "needs outDegree: relative and a value of at least 1")
		}
		for key := range sel.Where {
			if !validField(key) {
				bad(path+".where."+key, "unknown field: want address.country, address.postalCode, group or customAttributes.<key>")
			}
		}
	}

	if p := f.Peers; p != nil {
		if p.By != "" && (!validField(p.By) || p.By == "group") {
			bad("peers.by", "must be address.country, address.postalCode or customAttributes.<key>, not %q", p.By)
		}
		if p.MinSize != nil && *p.MinSize < 1 {
			bad("peers.minSize", "must be at least 1")
		}
		if p.Share != nil && (*p.Share <= 0 || *p.Share > 1) {
			bad("peers.share", "must be a fraction above 0 and at most 1, such as 0.9")
		}
	}

	if p := f.Profiles; p != nil {
		switch p.From {
		case "", ProfilesFromDecisionLogs:
			if len(p.Declared) > 0 {
				bad("profiles.declared", "is read only with from: declared")
			}
		case ProfilesDeclared:
			if len(p.Declared) == 0 {
				bad("profiles.declared", "from: declared needs at least one profile")
			}
		default:
			bad("profiles.from", "must be %s or %s, not %q", ProfilesFromDecisionLogs, ProfilesDeclared, p.From)
		}
	}

	names := map[string]bool{}
	for i, e := range f.Expect {
		path := fmt.Sprintf("expect[%d]", i)
		switch {
		case e.Name == "":
			bad(path+".name", "every expectation needs a name, which its findings and suppressions use")
		case slices.Contains(RuleIDs(), e.Name):
			bad(path+".name", "%q is a rule id; name the expectation something else", e.Name)
		case names[e.Name]:
			bad(path+".name", "%q names two expectations", e.Name)
		}
		names[e.Name] = true
		if !slices.Contains(roleNames, e.For.Role) {
			bad(path+".for.role", "must be one of %s, not %q", strings.Join(roleNames, ", "), e.For.Role)
		}
		for key := range e.For.Where {
			if !validField(key) {
				bad(path+".for.where."+key, "unknown field: want address.country, address.postalCode, group or customAttributes.<key>")
			}
		}
		if !slices.Contains(roleNames, e.ReachableFrom.Role) {
			bad(path+".reachableFrom.role", "must be one of %s, not %q", strings.Join(roleNames, ", "), e.ReachableFrom.Role)
		}
		if e.ReachableFrom.AtLeast < 1 {
			bad(path+".reachableFrom.atLeast", "must be at least 1")
		}
		if d := e.ReachableFrom.MaxTransitDays; d != nil && *d < 0 {
			bad(path+".reachableFrom.maxTransitDays", "must not be negative")
		}
	}

	for i, s := range f.Suppress {
		path := fmt.Sprintf("suppress[%d]", i)
		if !slices.Contains(RuleIDs(), s.Rule) && !names[s.Rule] {
			bad(path+".rule", "%q is neither a rule id nor the name of an expectation", s.Rule)
		}
		if strings.TrimSpace(s.Facility) == "" {
			bad(path+".facility", "name the finding's subject: the facility's tenant id, or the reference")
		}
		if strings.TrimSpace(s.Reason) == "" {
			bad(path+".reason", "say why the finding is accepted, so the next person can judge whether it still is")
		}
		if s.Until.IsZero() {
			bad(path+".until", "give the date it stops applying, as YYYY-MM-DD")
		}
	}
	sortProblems(out)
	return out
}

// Expired is every suppression whose date has passed on now: they no longer
// suppress anything, and validate reports them so they are renewed or removed.
func (lf *LoadedFile) Expired(now time.Time) []Problem {
	var out []Problem
	for i, s := range lf.Suppress {
		if !s.Until.IsZero() && s.Until.Passed(now) {
			path := fmt.Sprintf("suppress[%d].until", i)
			out = append(out, Problem{Line: lf.line(path), Path: path,
				Message: fmt.Sprintf("expired on %s: renew it with a reason that still holds, or remove it", s.Until.Format(time.DateOnly))})
		}
	}
	return out
}

// Unmatched is every selector, pinned facility and suppression that names
// nothing in the snapshot: almost always a typo, or a facility since retired.
func (lf *LoadedFile) Unmatched(n *Network) []Problem {
	var out []Problem
	miss := func(path, format string, args ...any) {
		out = append(out, Problem{Line: lf.line(path), Path: path, Message: fmt.Sprintf(format, args...)})
	}
	for _, r := range lf.roleSelectors() {
		name, sel := r.name, r.sel
		if sel == nil {
			continue
		}
		for i, ref := range sel.Facilities {
			if _, ok := n.lookupAny(ref); !ok {
				miss(fmt.Sprintf("roles.%s.facilities[%d]", name, i), "%s is not a facility in the snapshot", ref)
			}
		}
		if n.Select(sel).Empty() {
			miss("roles."+name, "selects no facility in the snapshot")
		}
		if g, ok := sel.Where["group"]; ok && !n.HasGroup(g) {
			miss("roles."+name+".where.group", "%s is not a facility group in the snapshot", g)
		}
	}
	for i, e := range lf.Expect {
		if n.Select(&Selector{Where: e.For.Where}).Empty() {
			miss(fmt.Sprintf("expect[%d].for.where", i), "selects no facility in the snapshot")
		}
	}
	for i, s := range lf.Suppress {
		_, isFacility := n.lookupAny(s.Facility)
		known := isFacility || n.mentions(s.Facility) ||
			(s.Rule == RuleConfigDrift && lf.isProfile(n, s.Facility))
		if !known {
			miss(fmt.Sprintf("suppress[%d].facility", i), "%s is neither a facility nor a reference in the snapshot", s.Facility)
		}
	}
	sortProblems(out)
	return out
}

// isProfile reports whether subject is an order profile a config-drift finding
// can be about. drift reports only a profile that was routed and that
// profiles.declared does not list, so the subject is always one the snapshot's
// decision logs routed; a profile the file declares, or one a tag implies that
// no order carried, can never be a subject.
func (lf *LoadedFile) isProfile(n *Network, subject string) bool {
	for _, pc := range n.InferProfiles(Config{}) {
		if pc.Orders > 0 && pc.Profile.String() == subject {
			return true
		}
	}
	return false
}

// declaredProfiles are the profiles profiles.declared lists, as tags.
func (lf *LoadedFile) declaredProfiles() [][]TagValue {
	p := lf.Profiles
	if p == nil || p.From != ProfilesDeclared {
		return nil
	}
	out := make([][]TagValue, 0, len(p.Declared))
	for _, d := range p.Declared {
		tags := make([]TagValue, 0, len(d))
		for _, id := range sortedStrings(d) {
			tags = append(tags, TagValue{ID: id, Value: d[id]})
		}
		out = append(out, tags)
	}
	return out
}

func validField(key string) bool {
	switch key {
	case "address.country", "address.postalCode", "group":
		return true
	}
	return strings.HasPrefix(key, "customAttributes.") && len(key) > len("customAttributes.")
}

// line finds the line a dotted path (roles.hub.facilities[2]) is written on,
// or 0 when the file does not spell it out.
func (lf *LoadedFile) line(path string) int {
	if lf.root == nil || len(lf.root.Content) == 0 {
		return 0
	}
	node := lf.root.Content[0]
	last := node.Line
	for _, part := range splitPath(path) {
		switch {
		case node.Kind == yaml.MappingNode && part.key != "":
			var next *yaml.Node
			for i := 0; i+1 < len(node.Content); i += 2 {
				if node.Content[i].Value == part.key {
					last, next = node.Content[i].Line, node.Content[i+1]
					break
				}
			}
			if next == nil {
				return last
			}
			node = next
		case node.Kind == yaml.SequenceNode && part.index >= 0 && part.index < len(node.Content):
			node = node.Content[part.index]
			last = node.Line
		default:
			return last
		}
	}
	return last
}

type pathPart struct {
	key   string
	index int
}

// splitPath splits roles.hub.facilities[2] into its keys and indices. A where
// key is itself dotted (address.country), so everything after "where." is one
// key.
func splitPath(path string) []pathPart {
	var out []pathPart
	for path != "" {
		if rest, ok := strings.CutPrefix(path, "where."); ok {
			out = append(out, pathPart{key: "where", index: -1}, pathPart{key: rest, index: -1})
			return out
		}
		seg, rest, _ := strings.Cut(path, ".")
		path = rest
		key, idx, hasIdx := strings.Cut(seg, "[")
		out = append(out, pathPart{key: key, index: -1})
		if hasIdx {
			i, _ := strconv.Atoi(strings.TrimSuffix(idx, "]"))
			out = append(out, pathPart{index: i})
		}
	}
	return out
}

// Settings turns the file into what a check runs with, over the defaults.
func (lf *LoadedFile) Settings(now time.Time) Config {
	cfg := DefaultConfig()
	cfg.FromFile = true
	cfg.Rules = lf.Rules
	cfg.Hub, cfg.Store, cfg.Supplier = lf.Roles.Hub, lf.Roles.Store, lf.Roles.Supplier
	if h := lf.Roles.Hub; h != nil && h.MinJump > 0 {
		cfg.HubRatio = h.MinJump
	}
	if p := lf.Peers; p != nil {
		if p.By != "" {
			cfg.PeerBy = p.By
		}
		if p.MinSize != nil {
			cfg.PeerMinSize = *p.MinSize
		}
		if p.Share != nil {
			cfg.PeerShare = *p.Share
		}
	}
	cfg.Declared = append(cfg.Declared, lf.declaredProfiles()...)
	cfg.Expect = lf.Expect
	for _, s := range lf.Suppress {
		if !s.Until.Passed(now) {
			cfg.Suppress = append(cfg.Suppress, s)
		}
	}
	return cfg
}

// lookupAny finds a facility by platform id, tenant id, or URN.
func (n *Network) lookupAny(ref string) (int, bool) {
	if i, ok := n.refs[ref]; ok {
		return i, true
	}
	i, ok := n.refs[client.FacilityRef(ref)]
	return i, ok
}

// mentions reports whether any connection names ref, which is what a
// dangling-ref finding's subject is.
func (n *Network) mentions(ref string) bool {
	for _, e := range n.snap.Edges {
		if e.Target.FacilityRef == ref || slices.Contains(e.Target.ExcludedFacilities, ref) ||
			slices.Contains(e.Target.ExcludedGroups, ref) {
			return true
		}
		for _, c := range e.Context {
			if slices.Contains(n.Values(c), ref) {
				return true
			}
		}
	}
	return false
}

// Select is the facilities a selector picks, online or not. OutDegree is the
// check's business, not the selector's: it is ignored here.
func (n *Network) Select(sel *Selector) Bitset {
	out := n.empty()
	if sel == nil {
		return out
	}
	var pinned map[int]bool
	if len(sel.Facilities) > 0 {
		pinned = map[int]bool{}
		for _, ref := range sel.Facilities {
			if i, ok := n.lookupAny(ref); ok {
				pinned[i] = true
			}
		}
	}
	for i := range n.snap.Facilities {
		f := n.Facility(i)
		if sel.Type != "" && f.Type != sel.Type {
			continue
		}
		if pinned != nil && !pinned[i] {
			continue
		}
		if !n.matches(i, sel.Where) {
			continue
		}
		out.Set(i)
	}
	return out
}

// matches reports whether the facility at i has every field where names.
func (n *Network) matches(i int, where map[string]string) bool {
	for key, want := range where {
		if n.field(i, key) != want {
			if key == "group" && n.groups[want].Has(i) {
				continue
			}
			return false
		}
	}
	return true
}

// field is a facility's value for a config field name.
func (n *Network) field(i int, key string) string {
	f := n.Facility(i)
	switch key {
	case "address.country":
		return f.Country
	case "address.postalCode":
		return f.PostalCode
	}
	if attr, ok := strings.CutPrefix(key, "customAttributes."); ok && len(f.CustomAttributes) > 0 {
		var attrs map[string]any
		if json.Unmarshal(f.CustomAttributes, &attrs) == nil {
			if v, ok := attrs[attr]; ok && v != nil {
				if s, ok := v.(string); ok {
					return s
				}
				b, _ := json.Marshal(v)
				return string(b)
			}
		}
	}
	return ""
}

// IsNotExist reports whether err is a config file that is not there.
func IsNotExist(err error) bool { return errors.Is(err, fs.ErrNotExist) }
