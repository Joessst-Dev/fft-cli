package network

import (
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Strategy is a routing strategy's tree, decoded for walking. The API writes it
// as a linked list: a node's children are its nextCondition, that condition's
// nextCondition, and so on, each leading to its child through nextNode. Strategy
// turns that into ordered children, keeping the order, because the router takes
// the first condition that holds.
type Strategy struct {
	ID       string
	Name     string
	InUse    bool
	Revision int64
	Root     *StrategyNode
}

// StrategyNode is one node: where the router stops when none of its conditions
// holds, and what configuration it adds on the way.
type StrategyNode struct {
	ID          string
	Name        string
	Active      bool
	CategoryRef string
	Frames      []TimeFrame
	Config      NodeConfig
	Conditions  []*StrategyCondition
}

// StrategyCondition leads from one node to the next when its rule holds.
type StrategyCondition struct {
	ID     string
	Name   string
	Active bool
	Frames []TimeFrame
	Rule   json.RawMessage
	Next   *StrategyNode
}

// TimeFrame is an activation time frame. A node or condition applies only
// within one of its frames, when it has any.
type TimeFrame struct {
	From       string `json:"activeFrom"`
	Until      string `json:"activeUntil"`
	Recurrence string `json:"recurrence"`
}

// NodeConfig is the part of a node's configuration a check reads.
type NodeConfig struct {
	Fences          []ConfigItem
	Ratings         []ConfigItem
	PriorityRatings []ConfigItem
	Fallback        *Fallback
	// raw is the whole config, for the rules that search it.
	raw json.RawMessage
}

// ConfigItem is one fence or rating.
type ConfigItem struct {
	Type           string `json:"type"`
	Implementation string `json:"implementation"`
	ReferenceID    string `json:"referenceId"`
	Name           string `json:"name"`
	Active         bool   `json:"active"`
}

// key is what a child's item overrides its parent's by: the referenceId of a
// toolkit item, the implementation of a standard one.
func (c ConfigItem) key() string {
	switch {
	case c.ReferenceID != "":
		return "ref:" + c.ReferenceID
	case c.Implementation != "":
		return "impl:" + c.Implementation
	default:
		return "name:" + c.Type + "/" + c.Name
	}
}

// Label names the item for a person.
func (c ConfigItem) Label() string {
	switch {
	case c.Name != "":
		return c.Name
	case c.Implementation != "":
		return c.Implementation
	case c.ReferenceID != "":
		return c.ReferenceID
	default:
		return c.Type
	}
}

// Fallback is a node's fallback facility.
type Fallback struct {
	Active     bool     `json:"active"`
	Facilities []string `json:"facilityRefs"`
	After      string   `json:"fallbackAfterTime"`
}

type rawNamed struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	NameLocalized map[string]string `json:"nameLocalized"`
	Active        *bool             `json:"active"`
	Frames        []TimeFrame       `json:"activationTimeFrames"`
}

func (r rawNamed) name() string {
	if r.Name != "" {
		return r.Name
	}
	if v := r.NameLocalized["en_US"]; v != "" {
		return v
	}
	for _, k := range slices.Sorted(maps.Keys(r.NameLocalized)) {
		if r.NameLocalized[k] != "" {
			return r.NameLocalized[k]
		}
	}
	return r.ID
}

// active is true unless the API says false: a node written without the field
// is one nobody switched off.
func (r rawNamed) active() bool { return r.Active == nil || *r.Active }

type rawNode struct {
	rawNamed
	CategoryRef   string          `json:"categoryRef"`
	Config        json.RawMessage `json:"config"`
	NextCondition *rawCondition   `json:"nextCondition"`
}

type rawCondition struct {
	rawNamed
	Rule          json.RawMessage `json:"rule"`
	NextNode      *rawNode        `json:"nextNode"`
	NextCondition *rawCondition   `json:"nextCondition"`
}

// maxDepth bounds the tree. Real strategies are a handful of levels deep; a
// document nested thousands deep is not one the API wrote.
const maxDepth = 256

// ParseStrategy decodes a strategy as GET /api/routing/strategies answers it.
func ParseStrategy(raw json.RawMessage) (*Strategy, error) {
	var s struct {
		rawNamed
		InUse    bool     `json:"inUse"`
		Revision int64    `json:"revision"`
		RootNode *rawNode `json:"rootNode"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("decode the routing strategy: %w", err)
	}
	out := &Strategy{ID: s.ID, Name: s.name(), InUse: s.InUse, Revision: s.Revision}
	if s.RootNode != nil {
		root, err := convertNode(s.RootNode, 0)
		if err != nil {
			return nil, fmt.Errorf("routing strategy %s: %w", s.ID, err)
		}
		out.Root = root
	}
	return out, nil
}

func convertNode(r *rawNode, depth int) (*StrategyNode, error) {
	if depth > maxDepth {
		return nil, fmt.Errorf("the tree is more than %d levels deep", maxDepth)
	}
	n := &StrategyNode{ID: r.ID, Name: r.name(), Active: r.active(), CategoryRef: r.CategoryRef, Frames: r.Frames}
	if len(r.Config) > 0 && string(r.Config) != "null" {
		var c struct {
			Fences          []ConfigItem `json:"fences"`
			Ratings         []ConfigItem `json:"ratings"`
			PriorityRatings []ConfigItem `json:"priorityRatings"`
			Fallback        *Fallback    `json:"fallbackFacility"`
		}
		if err := json.Unmarshal(r.Config, &c); err != nil {
			return nil, fmt.Errorf("node %s: decode its config: %w", r.ID, err)
		}
		n.Config = NodeConfig{Fences: c.Fences, Ratings: c.Ratings, PriorityRatings: c.PriorityRatings,
			Fallback: c.Fallback, raw: r.Config}
	}
	for c := r.NextCondition; c != nil; c = c.NextCondition {
		if len(n.Conditions) > maxDepth {
			return nil, fmt.Errorf("node %s has more than %d conditions", r.ID, maxDepth)
		}
		cond := &StrategyCondition{ID: c.ID, Name: c.name(), Active: c.active(), Frames: c.Frames, Rule: c.Rule}
		if c.NextNode != nil {
			child, err := convertNode(c.NextNode, depth+1)
			if err != nil {
				return nil, err
			}
			cond.Next = child
		}
		n.Conditions = append(n.Conditions, cond)
	}
	return n, nil
}

// Count is the number of nodes in the subtree under n, n included.
func (n *StrategyNode) Count() int {
	if n == nil {
		return 0
	}
	total := 1
	for _, c := range n.Conditions {
		total += c.Next.Count()
	}
	return total
}

// Merge is the configuration in force at the end of a path: each node's fences
// and ratings override the ones above it with the same key and add the rest,
// because "all settings from a previous node are inherited by the next one".
func Merge(path []*StrategyNode) NodeConfig {
	var out NodeConfig
	merge := func(into []ConfigItem, items []ConfigItem) []ConfigItem {
		for _, it := range items {
			if i := slices.IndexFunc(into, func(c ConfigItem) bool { return c.key() == it.key() }); i >= 0 {
				into[i] = it
			} else {
				into = append(into, it)
			}
		}
		return into
	}
	for _, n := range path {
		out.Fences = merge(out.Fences, n.Config.Fences)
		out.Ratings = merge(out.Ratings, n.Config.Ratings)
		out.PriorityRatings = merge(out.PriorityRatings, n.Config.PriorityRatings)
		if n.Config.Fallback != nil {
			out.Fallback = n.Config.Fallback
		}
	}
	return out
}

// Paths calls fn with every path from the root to a node the router can reach
// at all: through active conditions and active nodes only.
func (s *Strategy) Paths(fn func(path []*StrategyNode)) {
	var walk func(n *StrategyNode, path []*StrategyNode)
	walk = func(n *StrategyNode, path []*StrategyNode) {
		path = append(slices.Clip(path), n)
		fn(path)
		for _, c := range n.Conditions {
			if c.Active && c.Next != nil && c.Next.Active {
				walk(c.Next, path)
			}
		}
	}
	if s.Root != nil {
		walk(s.Root, nil)
	}
}

// expired reports whether every time frame has ended by now, so that the
// element can never apply again. Only a one-off frame ends: the API writes
// recurrence as YEARLY or NONRECURRING, and a frame repeating every year never
// does. A missing value reads as one-off (older snapshots), but any other value,
// one the API adds later included, stays "never ends" so the rule fails quiet.
func expired(frames []TimeFrame, now time.Time) bool {
	if len(frames) == 0 {
		return false
	}
	for _, f := range frames {
		if f.Recurrence != "" && f.Recurrence != "NONRECURRING" {
			return false
		}
		until, err := time.Parse(time.RFC3339, f.Until)
		if f.Until == "" || err != nil || until.After(now) {
			return false
		}
	}
	return true
}

// The strategy rule ids.
const (
	RuleStrategyInUse     = "strategy-in-use"
	RuleStrategyCategory  = "strategy-category"
	RuleStrategyInactive  = "strategy-inactive"
	RuleStrategyExpired   = "strategy-expired"
	RuleStrategyFallback  = "strategy-fallback"
	RuleStrategyNoRating  = "strategy-no-rating"
	RuleToolkitHardcodeID = "toolkit-hardcoded-id"
)

// strategyRead is one strategy of the snapshot. Which one is in use is read from
// the document on its own, so a strategy whose tree fft cannot decode still
// counts as live and is never swapped for the drafts.
type strategyRead struct {
	ID    string
	Name  string
	InUse bool
	// S is nil when the tree could not be decoded, and Err says why.
	S   *Strategy
	Err error
}

func (n *Network) strategies() []strategyRead {
	var out []strategyRead
	for _, raw := range n.snap.Strategies {
		var head struct {
			rawNamed
			InUse bool `json:"inUse"`
		}
		// A document that is not even an object has no id or flag to keep; it
		// is read as a draft that fails to decode.
		_ = json.Unmarshal(raw, &head)
		r := strategyRead{ID: head.ID, Name: head.name(), InUse: head.InUse}
		if r.Name == "" {
			r.Name = "(unreadable strategy)"
		}
		r.S, r.Err = ParseStrategy(raw)
		out = append(out, r)
	}
	return out
}

// strategyTreeRules are the rules that walk a strategy's tree, and so need it
// decoded.
var strategyTreeRules = []string{RuleStrategyCategory, RuleStrategyInactive, RuleStrategyExpired,
	RuleStrategyFallback, RuleStrategyNoRating, RuleToolkitHardcodeID}

// checked is the strategies the strategy rules judge: the one in use, or every
// strategy when none is — a network with no live strategy still has a draft
// someone means to switch on. An unreadable strategy is judged like any other,
// so an unreadable live one keeps the drafts out.
func checked(all []strategyRead) []strategyRead {
	live := slices.DeleteFunc(slices.Clone(all), func(s strategyRead) bool { return !s.InUse })
	if len(live) == 0 {
		return all
	}
	return live
}

// strategyRules runs the strategy-* rules.
func (n *Network) strategyRules(run func(string) bool) []Finding {
	all := n.strategies()
	var out []Finding
	now := n.snap.CreatedAt

	if run(RuleStrategyInUse) {
		var live []string
		for _, s := range all {
			if s.InUse {
				live = append(live, s.Name)
			}
		}
		if len(live) != 1 && len(all) > 0 {
			detail := "no routing strategy is in use"
			if len(live) > 1 {
				detail = fmt.Sprintf("%d routing strategies are in use at once: %s", len(live), strings.Join(live, ", "))
			}
			out = append(out, Finding{Rule: RuleStrategyInUse, Subject: "(strategies)", Detail: detail,
				Evidence: map[string]any{"inUse": len(live), "strategies": len(all)},
				Next:     "Exactly one strategy routes orders. Activate the intended one: fft routing strategy activate <id>."})
		}
	}

	categories := map[string]bool{}
	for _, c := range n.snap.Categories {
		categories[c.ID] = true
	}

	// An unreadable strategy is reported under the first tree rule the user
	// selected, and not at all when none was: the finding stands for the checks
	// that could not run.
	treeRule := ""
	for _, r := range strategyTreeRules {
		if run(r) {
			treeRule = r
			break
		}
	}

	for _, read := range checked(all) {
		if read.S == nil {
			if treeRule != "" {
				out = append(out, Finding{Rule: treeRule, Subject: read.Name,
					Detail:   "fft cannot read this routing strategy's tree, so its tree rules did not run: " + read.Err.Error(),
					Evidence: map[string]any{"strategy": read.ID},
					Next:     "Take a new snapshot, and report it if it persists."})
			}
			continue
		}
		s := read.S
		if s.Root == nil {
			continue
		}
		subject := func(name string) string { return s.Name + " › " + name }

		var visit func(node *StrategyNode, live bool)
		visit = func(node *StrategyNode, live bool) {
			if run(RuleStrategyCategory) && n.snap.CategoriesRead && node.CategoryRef != "" && !categories[node.CategoryRef] {
				out = append(out, Finding{Rule: RuleStrategyCategory, Subject: subject(node.Name),
					Detail:   fmt.Sprintf("the node's categoryRef %s names no node config category", node.CategoryRef),
					Evidence: map[string]any{"strategy": s.ID, "node": node.ID, "categoryRef": node.CategoryRef},
					Next:     "Point it at an existing category (fft routing category list), or remove the reference."})
			}
			if run(RuleStrategyExpired) && live && node.Active && expired(node.Frames, now) {
				out = append(out, Finding{Rule: RuleStrategyExpired, Subject: subject(node.Name),
					Detail:   "every activation time frame of this node has ended, so it never applies again",
					Evidence: map[string]any{"strategy": s.ID, "node": node.ID},
					Next:     "Extend the time frame if the node is still meant to apply, or remove the node."})
			}
			if f := node.Config.Fallback; run(RuleStrategyFallback) && f != nil && f.Active {
				for _, ref := range f.Facilities {
					i, ok := n.lookupAny(ref)
					switch {
					case !ok:
						out = append(out, Finding{Rule: RuleStrategyFallback, Subject: subject(node.Name),
							Detail:   fmt.Sprintf("the fallback facility %s is not a facility in the network", ref),
							Evidence: map[string]any{"strategy": s.ID, "node": node.ID, "facility": ref},
							Next:     "Orders that cannot be routed are sent to the fallback; point it at a facility that exists."})
					case !n.Facility(i).Online():
						out = append(out, Finding{Rule: RuleStrategyFallback, Subject: subject(node.Name),
							Facilities: []string{n.Facility(i).Label()},
							Detail: fmt.Sprintf("the fallback facility %s is %s", n.Facility(i).Label(),
								strings.ToLower(n.Facility(i).Status)),
							Evidence: map[string]any{"strategy": s.ID, "node": node.ID, "facility": ref},
							Next:     "Orders that cannot be routed are sent to the fallback; point it at an online facility."})
					}
				}
			}
			for _, c := range node.Conditions {
				switch {
				case !c.Active:
					if below := c.Next.Count(); run(RuleStrategyInactive) && live && below > 0 {
						out = append(out, Finding{Rule: RuleStrategyInactive, Subject: subject(c.Name),
							Detail:   fmt.Sprintf("the condition is inactive, and with it the %s below it", plural(below, "node")),
							Evidence: map[string]any{"strategy": s.ID, "condition": c.ID, "nodes": below},
							Next:     "If the branch is retired, remove it; if it is meant to route, activate the condition."})
					}
				case c.Next != nil && !c.Next.Active:
					if run(RuleStrategyInactive) && live {
						out = append(out, Finding{Rule: RuleStrategyInactive, Subject: subject(c.Next.Name),
							Detail: fmt.Sprintf("the node is inactive, so an order that meets %q stops above it; %s below it with it",
								c.Name, plural(c.Next.Count(), "node")),
							Evidence: map[string]any{"strategy": s.ID, "node": c.Next.ID, "nodes": c.Next.Count()},
							Next:     "If the branch is retired, remove it; if it is meant to route, activate the node."})
					}
				case run(RuleStrategyExpired) && live && expired(c.Frames, now):
					out = append(out, Finding{Rule: RuleStrategyExpired, Subject: subject(c.Name),
						Detail:   "every activation time frame of this condition has ended, so its branch is never taken again",
						Evidence: map[string]any{"strategy": s.ID, "condition": c.ID},
						Next:     "Extend the time frame if the branch is still meant to route, or remove it."})
				}
				if c.Next != nil {
					visit(c.Next, live && c.Active && c.Next.Active)
				}
			}
		}
		visit(s.Root, s.Root.Active)

		if run(RuleStrategyNoRating) {
			s.Paths(func(path []*StrategyNode) {
				leaf := path[len(path)-1]
				merged := Merge(path)
				if slices.ContainsFunc(merged.Ratings, func(c ConfigItem) bool { return c.Active }) {
					return
				}
				names := make([]string, len(path))
				for i, p := range path {
					names[i] = p.Name
				}
				out = append(out, Finding{Rule: RuleStrategyNoRating, Subject: subject(leaf.Name),
					Detail:   fmt.Sprintf("an order that ends here (%s) is routed with no active rating", strings.Join(names, " › ")),
					Evidence: map[string]any{"strategy": s.ID, "node": leaf.ID, "ratings": len(merged.Ratings)},
					Next:     "Without a rating, every facility the fences let through ranks the same. Activate a rating on this path."})
			})
		}

		if run(RuleToolkitHardcodeID) {
			out = append(out, n.hardcodedIDs(s)...)
		}
	}
	return out
}

// facilityFilter finds facility ids hard-coded in a JSONPath filter, such as
// $.customAttributes.x[?(@.tenantFacilityId == 'BER-01')].
var facilityFilter = regexp.MustCompile(`@\.(tenantFacilityId|facilityRef|facilityId)\s*==\s*['"]([^'"]+)['"]`)

// facilityField is a propertyPath that ends in a facility id.
var facilityField = regexp.MustCompile(`(?i)(tenantFacilityId|facilityRef|facilityId)$`)

// hardcodedIDs finds facility ids written into a strategy's toolkit rules — as
// an expectedValue compared with a facility id field, or inside a JSONPath
// filter — that name no facility. A numeric expectedValue next to a filter is
// a value, not an id, and is left alone.
func (n *Network) hardcodedIDs(s *Strategy) []Finding {
	type hit struct{ where []string }
	hits := map[string]*hit{}
	note := func(id, where string) {
		if _, ok := n.lookupAny(id); ok {
			return
		}
		h, ok := hits[id]
		if !ok {
			h = &hit{}
			hits[id] = h
		}
		if !slices.Contains(h.where, where) {
			h.where = append(h.where, where)
		}
	}

	var search func(v any, where string)
	search = func(v any, where string) {
		switch v := v.(type) {
		case map[string]any:
			if path, ok := v["propertyPath"].(string); ok {
				for _, m := range facilityFilter.FindAllStringSubmatch(path, -1) {
					note(m[2], where)
				}
				if s, ok := v["expectedValue"].(string); ok && facilityField.MatchString(path) && s != "" {
					note(s, where)
				}
			}
			for _, k := range slices.Sorted(maps.Keys(v)) {
				search(v[k], where)
			}
		case []any:
			for _, child := range v {
				search(child, where)
			}
		}
	}
	decode := func(raw json.RawMessage, where string) {
		var v any
		if len(raw) > 0 && json.Unmarshal(raw, &v) == nil {
			search(v, where)
		}
	}

	var visit func(node *StrategyNode)
	visit = func(node *StrategyNode) {
		decode(node.Config.raw, node.Name)
		for _, c := range node.Conditions {
			decode(c.Rule, c.Name)
			if c.Next != nil {
				visit(c.Next)
			}
		}
	}
	if s.Root != nil {
		visit(s.Root)
	}

	var out []Finding
	for _, id := range sortedStrings(hits) {
		out = append(out, Finding{Rule: RuleToolkitHardcodeID, Subject: id,
			Detail: fmt.Sprintf("the routing strategy %s compares with the facility id %s, which names no facility (in %s)",
				s.Name, id, strings.Join(hits[id].where, ", ")),
			Evidence: map[string]any{"strategy": s.ID, "in": hits[id].where},
			Next:     "The rule can never match that facility. Correct the id, or remove the comparison."})
	}
	return out
}
