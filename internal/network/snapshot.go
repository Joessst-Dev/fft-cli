package network

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/Joessst-Dev/fft-cli/internal/atomicfile"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

// SchemaVersion is the snapshot format this fft writes. [Load] reads it and every
// version below it, and refuses one above it: a newer fft may have changed what a
// field means, and a check that silently misread its input would report gaps that
// are not there, or miss ones that are.
const SchemaVersion = 1

// maxSnapshot bounds what [Load] will decompress. A production-size network is
// tens of megabytes once its context values are interned; a file that inflates to
// gigabytes is not one fft wrote.
const maxSnapshot = 2 << 30

// Snapshot is a network as a crawl found it: every facility, every connection,
// and what the router needs to explain itself. It is everything `fft network
// check` reads, so a check never needs the tenant.
type Snapshot struct {
	SchemaVersion int       `json:"schemaVersion"`
	FFTVersion    string    `json:"fftVersion"`
	CreatedAt     time.Time `json:"createdAt"`
	Source        Source    `json:"source"`
	Stats         Stats     `json:"stats"`

	Facilities []Facility `json:"facilities"`
	Edges      []Edge     `json:"edges"`

	// ValueSets holds every distinct context value list once; a [ContextEntry]
	// points into it. A large network repeats the same list of a few hundred
	// facility ids on thousands of connections, and interning is what turns
	// gigabytes of raw connections into a file that loads in seconds.
	ValueSets [][]string `json:"valueSets"`

	Groups []Group `json:"groups"`
	Tags   []Tag   `json:"tags"`

	// Strategies are the routing strategies as the API returned them. They are
	// kept whole, because a strategy's tree is read by rules fft has not written
	// yet, and a projection would have to guess which fields those need.
	Strategies []json.RawMessage `json:"strategies"`

	DecisionLogs  []DecisionLog  `json:"decisionLogs"`
	OrderProfiles []OrderProfile `json:"orderProfiles"`
}

// Source says which tenant a snapshot came from, so that `--resume` cannot
// stitch two tenants into one network, and a reader can tell two snapshots apart.
type Source struct {
	Project string `json:"project"`
	BaseURL string `json:"baseUrl"`
}

// Stats is how the crawl went.
type Stats struct {
	// Duration is the wall-clock time of the run that finished the snapshot; a
	// resumed crawl's earlier runs are not in it.
	Duration time.Duration `json:"duration"`
	// Resumed is how many facilities' connections came from an earlier run.
	Resumed int `json:"resumed"`
	// Pauses is how often the crawl backed off a throttling gateway.
	Pauses int `json:"pauses"`
}

// Facility is the part of a facility a check reads.
type Facility struct {
	ID               string          `json:"id"`
	TenantFacilityID string          `json:"tenantFacilityId,omitempty"`
	Name             string          `json:"name,omitempty"`
	Type             string          `json:"type"`
	Status           string          `json:"status"`
	Country          string          `json:"country,omitempty"`
	PostalCode       string          `json:"postalCode,omitempty"`
	CustomAttributes json.RawMessage `json:"customAttributes,omitempty"`
}

// Online reports whether the facility takes part in routing.
func (f Facility) Online() bool { return f.Status == StatusOnline }

// Label names the facility for a person: its tenant id where it has one, which is
// what the people who run the network call it.
func (f Facility) Label() string {
	if f.TenantFacilityID != "" {
		return f.TenantFacilityID
	}
	return f.ID
}

// The facility and target types and the status the resolver needs to tell apart.
const (
	TypeManaged  = "MANAGED_FACILITY"
	TypeSupplier = "SUPPLIER"
	TypeCustomer = "CUSTOMER"

	StatusOnline = "ONLINE"
)

// Edge is one interfacility connection.
type Edge struct {
	ID string `json:"id"`
	// Source is the platform id of the facility whose connection list it came
	// from — authoritative, because that is where the crawl found it.
	Source  string         `json:"source"`
	Target  Target         `json:"target"`
	Context []ContextEntry `json:"context,omitempty"`
	// Transit is the connection's fallbackTransitTime, when it has one.
	Transit *Transit `json:"transit,omitempty"`
}

// Target is where a connection leads. A MANAGED_FACILITY or SUPPLIER target
// without a FacilityRef is a wildcard: every facility of that type, narrowed by
// the exclusions and by the connection's context.
type Target struct {
	Type               string   `json:"type"`
	FacilityRef        string   `json:"facilityRef,omitempty"`
	ExcludedFacilities []string `json:"excludedFacilityRefs,omitempty"`
	ExcludedGroups     []string `json:"excludedFacilityGroupRefs,omitempty"`
}

// ContextEntry is one entry of a connection's context. Values indexes
// [Snapshot.ValueSets].
type ContextEntry struct {
	Type     string `json:"type"`
	Operator string `json:"operator,omitempty"`
	Values   int    `json:"values"`
}

// The context entry types and the one operator the API defines. An entry
// without an operator is positive: one of its values must match.
const (
	ContextFacility       = "FACILITY"
	ContextFacilityGroup  = "FACILITY_GROUP"
	ContextTag            = "TAG_REFERENCE"
	ContextCategory       = "CATEGORY"
	ContextTargetPostcode = "TARGET_POSTALCODE"
	ContextTargetCountry  = "TARGET_COUNTRY"

	OperatorNot = "NOT"
)

// Transit is a connection's fallback transit time, in days.
type Transit struct {
	Min float64 `json:"minTransitDays"`
	Max float64 `json:"maxTransitDays"`
}

// Group is a facility group and its members.
type Group struct {
	ID       string   `json:"id"`
	TenantID string   `json:"tenantFacilityGroupId,omitempty"`
	Name     string   `json:"name,omitempty"`
	Members  []string `json:"facilityRefs"`
}

// Tag is a tag and the values an order may carry for it.
type Tag struct {
	ID            string   `json:"id"`
	AllowedValues []string `json:"allowedValues,omitempty"`
}

// TagValue is one tag an order carries.
type TagValue struct {
	ID    string `json:"id"`
	Value string `json:"value"`
}

// DecisionLog is the part of a routing decision log a check reads: which branch
// of the strategy the router took, and which connections it used.
type DecisionLog struct {
	ID       string `json:"id"`
	Created  string `json:"created,omitempty"`
	OrderRef string `json:"orderRef,omitempty"`
	// Evaluation is the log's routingStrategyEvaluationResult, as the API gave it.
	Evaluation json.RawMessage `json:"evaluation,omitempty"`
	// Connections are the facilityConnectionRefs of the transfers it chose.
	Connections []string `json:"connections,omitempty"`
}

// OrderProfile is a combination of tags that real orders carried, and how many
// of the sampled orders carried it. It is what "for this kind of order" means.
type OrderProfile struct {
	Tags   []TagValue `json:"tags"`
	Orders int        `json:"orders"`
}

// SnapshotError is a snapshot that cannot be read. It exits 3, like a config
// that cannot be: the command was fine, its input is not.
type SnapshotError struct {
	Path string
	Err  error
	hint string
}

func (e *SnapshotError) Error() string { return fmt.Sprintf("read the snapshot %s: %v", e.Path, e.Err) }

// Unwrap returns the cause.
func (e *SnapshotError) Unwrap() error { return e.Err }

// ExitCode is 3: the input is unusable.
func (e *SnapshotError) ExitCode() int { return exitcode.Config }

// Hint says what to do about it.
func (e *SnapshotError) Hint() string { return e.hint }

// Save writes s to path, gzipped, readable by its owner only: a snapshot is the
// tenant's whole network, and it is nobody else's business on a shared machine.
func Save(path string, s *Snapshot) error {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if err := json.NewEncoder(zw).Encode(s); err != nil {
		return fmt.Errorf("encode the snapshot: %w", err)
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("compress the snapshot: %w", err)
	}
	if err := atomicfile.Write(path, buf.Bytes()); err != nil {
		return fmt.Errorf("write the snapshot %s: %w", path, err)
	}
	return nil
}

// Load reads a snapshot [Save] wrote.
func Load(path string) (*Snapshot, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, &SnapshotError{Path: path, Err: errors.New("there is no snapshot there"),
			hint: "Take one first: fft network snapshot"}
	}
	if err != nil {
		return nil, &SnapshotError{Path: path, Err: err}
	}
	defer f.Close()

	corrupt := func(err error) error {
		return &SnapshotError{Path: path, Err: err,
			hint: "The file is not a snapshot fft wrote, or it was cut short. Take a new one: fft network snapshot"}
	}

	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, corrupt(err)
	}
	var s Snapshot
	if err := json.NewDecoder(io.LimitReader(zr, maxSnapshot)).Decode(&s); err != nil {
		return nil, corrupt(err)
	}

	switch {
	case s.SchemaVersion == 0:
		return nil, corrupt(errors.New("it has no schema version"))
	case s.SchemaVersion > SchemaVersion:
		return nil, &SnapshotError{Path: path,
			Err:  fmt.Errorf("it is format %d, and this fft reads up to %d", s.SchemaVersion, SchemaVersion),
			hint: "Upgrade fft, or take a new snapshot with this one: fft network snapshot"}
	}
	if err := s.validate(); err != nil {
		return nil, corrupt(err)
	}
	return &s, nil
}

// validate refuses a snapshot whose context entries point past its value sets,
// which would otherwise panic the resolver half-way through a check.
func (s *Snapshot) validate() error {
	for _, e := range s.Edges {
		for _, c := range e.Context {
			if c.Values < 0 || c.Values >= len(s.ValueSets) {
				return fmt.Errorf("connection %s has a context entry with no value set", e.ID)
			}
		}
	}
	return nil
}

// builder assembles a snapshot from raw API documents, interning each context
// value list as it goes. It is not safe for concurrent use; the crawl serialises
// on it.
type builder struct {
	snap  Snapshot
	index map[string]int
}

func newBuilder() *builder {
	return &builder{index: map[string]int{}}
}

// intern returns the index of values' set, and whether it is new.
func (b *builder) intern(values []string) (int, bool) {
	key := strings.Join(values, "\x00")
	if i, ok := b.index[key]; ok {
		return i, false
	}
	i := len(b.snap.ValueSets)
	b.snap.ValueSets = append(b.snap.ValueSets, slices.Clone(values))
	b.index[key] = i
	return i, true
}

// rawConnection is a connection as GET /api/facilities/{id}/connections answers
// it, reduced to what an [Edge] keeps.
type rawConnection struct {
	ID     string `json:"id"`
	Target struct {
		Type               string   `json:"type"`
		FacilityRef        string   `json:"facilityRef"`
		ExcludedFacilities []string `json:"excludedFacilityRefs"`
		ExcludedGroups     []string `json:"excludedFacilityGroupRefs"`
	} `json:"target"`
	Context []struct {
		Type     string   `json:"type"`
		Operator string   `json:"operator"`
		Values   []string `json:"values"`
	} `json:"context"`
	Transit *Transit `json:"fallbackTransitTime"`
}

// edges decodes one facility's connections, interning their context values.
// It returns the edges and the value sets that were new, in index order, which
// is what the journal needs to write before it writes the edges.
func (b *builder) edges(source string, raws []json.RawMessage) ([]Edge, []int, error) {
	out := make([]Edge, 0, len(raws))
	var fresh []int
	for _, raw := range raws {
		var c rawConnection
		if err := json.Unmarshal(raw, &c); err != nil {
			return nil, nil, fmt.Errorf("decode a connection of %s: %w", source, err)
		}
		e := Edge{
			ID:     c.ID,
			Source: source,
			Target: Target{
				Type:               c.Target.Type,
				FacilityRef:        c.Target.FacilityRef,
				ExcludedFacilities: c.Target.ExcludedFacilities,
				ExcludedGroups:     c.Target.ExcludedGroups,
			},
			Transit: c.Transit,
		}
		for _, ctx := range c.Context {
			i, isNew := b.intern(ctx.Values)
			if isNew {
				fresh = append(fresh, i)
			}
			e.Context = append(e.Context, ContextEntry{Type: ctx.Type, Operator: ctx.Operator, Values: i})
		}
		out = append(out, e)
	}
	return out, fresh, nil
}

// rawFacility is a facility as the facility search answers it.
type rawFacility struct {
	ID               string `json:"id"`
	TenantFacilityID string `json:"tenantFacilityId"`
	Name             string `json:"name"`
	Type             string `json:"type"`
	Status           string `json:"status"`
	Address          struct {
		Country    string `json:"country"`
		PostalCode string `json:"postalCode"`
	} `json:"address"`
	CustomAttributes json.RawMessage `json:"customAttributes"`
}

func decodeFacility(raw json.RawMessage) (Facility, error) {
	var f rawFacility
	if err := json.Unmarshal(raw, &f); err != nil {
		return Facility{}, fmt.Errorf("decode a facility: %w", err)
	}
	if f.ID == "" {
		return Facility{}, errors.New("the API returned a facility with no id")
	}
	attrs := f.CustomAttributes
	if string(attrs) == "null" {
		attrs = nil
	}
	return Facility{
		ID:               f.ID,
		TenantFacilityID: f.TenantFacilityID,
		Name:             f.Name,
		Type:             f.Type,
		Status:           f.Status,
		Country:          f.Address.Country,
		PostalCode:       f.Address.PostalCode,
		CustomAttributes: attrs,
	}, nil
}
