package network

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/Joessst-Dev/fft-cli/internal/client"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

// CrawlOperations are the operationIds a crawl sends, and the only ones. A spec
// holds every one of them to the read-only classification: `fft network
// snapshot` is a single command that sends seven kinds of request, and the
// read-only gate, which reads one annotation per command, cannot see six of
// them.
var CrawlOperations = []string{
	"searchFacility",
	"getFacilityConnections",
	"searchFacilityGroup",
	"getTags",
	"getRoutingStrategies",
	"getRoutingDecisionLogs",
	"getOrder",
}

// The crawl's defaults.
const (
	// DefaultConcurrency is how many facilities' connections are walked at once.
	// A production gateway throttled a crawl of three parallel walks about every
	// two thousand requests; more would only be throttled sooner.
	DefaultConcurrency = 3
	// MaxConcurrency caps --concurrency. The crawl shares the tenant with the
	// people running it.
	MaxConcurrency = 8
	// DefaultPause is the first back-off after a throttled request. Gateways that
	// throttle recover in about a minute; a few seconds would only be throttled
	// again.
	DefaultPause = time.Minute
	// DefaultMaxPauses is how often one request may be paused for before the
	// crawl gives up on it and keeps the journal for --resume.
	DefaultMaxPauses = 5
	// maxEntities bounds every walk: far above any real network, far below a
	// cursor that loops forever.
	maxEntities = 1 << 20
)

// CrawlOptions configures [Crawl].
type CrawlOptions struct {
	// Source names the tenant. It is written into the snapshot and the journal,
	// and a journal from another source is never resumed.
	Source     Source
	FFTVersion string
	// Out is where the snapshot goes; the journal is Out + [JournalSuffix].
	Out    string
	Resume bool

	Concurrency int
	// DecisionLogs is how many recent routing decision logs to read: one page,
	// because the endpoint cannot be paged (#318). Zero skips them, and the
	// orders with them.
	DecisionLogs int
	// Orders reads the orders the decision logs name, for the tags that make up
	// the order profiles.
	Orders bool

	Pause     time.Duration
	MaxPauses int
	// Sleep waits, or returns the context's error. Nil is a real sleep; specs
	// replace it so that a minute's back-off does not take a minute.
	Sleep func(ctx context.Context, d time.Duration) error
	// Progress receives what the crawl is doing: stderr, never stdout.
	Progress io.Writer
	Now      func() time.Time
}

func (o *CrawlOptions) defaults() {
	if o.Concurrency <= 0 {
		o.Concurrency = DefaultConcurrency
	}
	o.Concurrency = min(o.Concurrency, MaxConcurrency)
	if o.Pause <= 0 {
		o.Pause = DefaultPause
	}
	if o.MaxPauses <= 0 {
		o.MaxPauses = DefaultMaxPauses
	}
	if o.Sleep == nil {
		o.Sleep = sleep
	}
	if o.Progress == nil {
		o.Progress = io.Discard
	}
	if o.Now == nil {
		o.Now = time.Now
	}
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// CrawlError is a crawl that stopped part-way. Its exit code is the cause's —
// an expired session exits 4, a gateway that never recovered exits 9 — and its
// hint says that the work done so far is not lost.
type CrawlError struct {
	Done, Total int
	Journal     string
	Err         error
}

func (e *CrawlError) Error() string {
	return fmt.Sprintf("the crawl stopped after %d of %d facilities: %v", e.Done, e.Total, e.Err)
}

// Unwrap returns the cause.
func (e *CrawlError) Unwrap() error { return e.Err }

// Hint says how to continue.
func (e *CrawlError) Hint() string {
	return fmt.Sprintf("The connections read so far are kept in %s.\n"+
		"Run the same command with --resume to continue from there.", e.Journal)
}

// crawler is one run of [Crawl].
type crawler struct {
	c    *client.Client
	o    CrawlOptions
	b    *builder
	j    *journal
	done map[string]bool

	mu       sync.Mutex // guards b, j, done and progress output
	pauses   atomic.Int64
	walked   int
	lastNote time.Time
}

// Crawl reads the network from the tenant and writes it to o.Out as a snapshot.
// It only reads; see [CrawlOperations].
func Crawl(ctx context.Context, c *client.Client, o CrawlOptions) (*Snapshot, error) {
	o.defaults()
	start := o.Now()

	b := newBuilder()
	if _, err := os.Stat(o.Out + JournalSuffix); err == nil && !o.Resume {
		// Said, because an hour of crawling is about to be thrown away by a flag
		// somebody forgot.
		fmt.Fprintf(o.Progress, "starting over: the journal of an earlier crawl is discarded (--resume would continue it)\n")
	}
	j, done, err := openJournal(o.Out+JournalSuffix, o.Source, b, o.Resume)
	if err != nil {
		return nil, err
	}
	defer j.close()

	cr := &crawler{c: c, o: o, b: b, j: j, done: done, lastNote: start}
	resumed := len(done)
	if resumed > 0 {
		cr.notef("resuming: %d facilities' connections are already in the journal", resumed)
	}

	facilities, err := cr.facilities(ctx)
	if err != nil {
		return nil, err
	}
	if err := cr.connections(ctx, facilities); err != nil {
		return nil, err
	}

	s := &b.snap
	s.SchemaVersion = SchemaVersion
	s.FFTVersion = o.FFTVersion
	s.Source = o.Source
	s.Facilities = facilities

	steps := []struct {
		what string
		run  func(context.Context, *Snapshot) error
	}{
		{"facility groups", cr.groups},
		{"tags", cr.tags},
		{"routing strategies", cr.strategies},
	}
	if o.DecisionLogs > 0 {
		steps = append(steps, struct {
			what string
			run  func(context.Context, *Snapshot) error
		}{"decision logs", cr.decisionLogs})
	}
	for _, step := range steps {
		cr.notef("reading the %s", step.what)
		if err := step.run(ctx, s); err != nil {
			return nil, cr.stopped(len(facilities), err)
		}
	}

	s.Edges = present(s.Edges, facilities)
	s.CreatedAt = o.Now().UTC()
	s.Stats = Stats{Duration: o.Now().Sub(start), Resumed: resumed, Pauses: int(cr.pauses.Load())}

	if err := Save(o.Out, s); err != nil {
		return nil, err
	}
	if err := j.remove(); err != nil {
		return nil, err
	}
	return s, nil
}

// stopped wraps a failure after the connection walks began, so that the hint
// says the journal survives it.
func (cr *crawler) stopped(total int, err error) error {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	return &CrawlError{Done: len(cr.done), Total: total, Journal: cr.j.path, Err: err}
}

// present drops the edges of facilities that are gone: a resumed crawl may hold
// connections of a facility deleted since the run that read them.
func present(edges []Edge, facilities []Facility) []Edge {
	ids := make(map[string]bool, len(facilities))
	for _, f := range facilities {
		ids[f.ID] = true
	}
	return slices.DeleteFunc(edges, func(e Edge) bool { return !ids[e.Source] })
}

func (cr *crawler) notef(format string, args ...any) {
	fmt.Fprintf(cr.o.Progress, format+"\n", args...)
}

// patient runs fn, and while it fails because the API is throttling or a
// gateway is struggling, waits and runs it again — longer each time, and at
// most MaxPauses times. Anything else is returned at once: a 401 that survived
// the client's refresh, a 403, a 404 will not get better by waiting.
func (cr *crawler) patient(ctx context.Context, what string, fn func() error) error {
	wait := cr.o.Pause
	for pause := 0; ; pause++ {
		err := fn()
		if err == nil || pause >= cr.o.MaxPauses || !throttled(ctx, err) {
			return err
		}
		cr.pauses.Add(1)
		cr.mu.Lock()
		cr.notef("%s: %v; pausing %s", what, err, wait)
		cr.mu.Unlock()
		if err := cr.o.Sleep(ctx, wait); err != nil {
			return err
		}
		wait *= 2
	}
}

// throttled reports whether err is the API or its gateway asking for a slower
// client, as opposed to refusing the request.
func throttled(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return false
	}
	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.Status {
		case http.StatusTooManyRequests, http.StatusBadGateway,
			http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return true
		}
		return false
	}
	// A dropped connection or a request that timed out on its own: the client
	// classifies those as an unavailable upstream.
	return exitcode.FromError(err) == exitcode.Unavailable
}

// drain collects a walk, stopping at its first error.
func drain[T any](seq func(func(T, error) bool)) ([]T, error) {
	var out []T
	for item, err := range seq {
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func (cr *crawler) facilities(ctx context.Context) ([]Facility, error) {
	cr.notef("reading the facilities")
	var raws []json.RawMessage
	err := cr.patient(ctx, "facilities", func() error {
		var err error
		raws, err = drain(client.SearchAll(ctx, cr.c, client.FacilitySearch[json.RawMessage](),
			client.FacilitySearchPayload{}, client.MaxItems(maxEntities)))
		return err
	})
	if err != nil {
		return nil, err
	}
	out := make([]Facility, 0, len(raws))
	for _, raw := range raws {
		f, err := decodeFacility(raw)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

// connections walks every facility's connections — offline ones too, because a
// connection that names an offline facility is one of the things a check looks
// for — skipping those the journal already holds.
func (cr *crawler) connections(ctx context.Context, facilities []Facility) error {
	var todo []string
	for _, f := range facilities {
		if !cr.done[f.ID] {
			todo = append(todo, f.ID)
		}
	}
	cr.notef("reading the connections of %d facilities (%d at a time)", len(todo), cr.o.Concurrency)

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(cr.o.Concurrency)
	for _, id := range todo {
		g.Go(func() error {
			var raws []json.RawMessage
			err := cr.patient(gctx, "connections of "+id, func() error {
				var err error
				raws, err = drain(client.ListAll(gctx, cr.c, client.FacilityConnections(id, ""),
					client.PageSize, client.MaxItems(maxEntities)))
				return err
			})
			if err != nil {
				return err
			}
			return cr.keep(id, raws, len(todo))
		})
	}
	if err := g.Wait(); err != nil {
		// The group's context is cancelled by the first failure, so the other
		// walks report "context canceled" — the cause is the one Wait returns.
		return cr.stopped(len(facilities), err)
	}
	return nil
}

// keep adds one facility's connections to the snapshot and the journal.
func (cr *crawler) keep(id string, raws []json.RawMessage, total int) error {
	cr.mu.Lock()
	defer cr.mu.Unlock()

	edges, fresh, err := cr.b.edges(id, raws)
	if err != nil {
		return err
	}
	if err := cr.j.record(cr.b, id, edges, fresh); err != nil {
		return err
	}
	cr.b.snap.Edges = append(cr.b.snap.Edges, edges...)
	cr.done[id] = true
	cr.walked++

	if now := cr.o.Now(); now.Sub(cr.lastNote) >= 10*time.Second || cr.walked == total {
		cr.lastNote = now
		cr.notef("connections: %d of %d facilities, %d connections so far", cr.walked, total, len(cr.b.snap.Edges))
	}
	return nil
}

func (cr *crawler) groups(ctx context.Context, s *Snapshot) error {
	op := client.RawSearchOp(cr.c, "searchFacilityGroup", "search the facility groups",
		"facilityGroups", "/api/facilitygroups/search")
	var raws []json.RawMessage
	err := cr.patient(ctx, "facility groups", func() error {
		var err error
		raws, err = drain(client.SearchAll(ctx, cr.c, op,
			client.SearchPayload[struct{}, struct{}]{}, client.MaxItems(maxEntities)))
		return err
	})
	if err != nil {
		return err
	}
	return decodeAll(raws, &s.Groups, "facility group")
}

func (cr *crawler) tags(ctx context.Context, s *Snapshot) error {
	op := client.RawListOp(cr.c, "list the tags", "tags",
		client.RawRequest{Method: http.MethodGet, Path: "/api/tags"})
	var raws []json.RawMessage
	err := cr.patient(ctx, "tags", func() error {
		var err error
		raws, err = drain(client.ListAll(ctx, cr.c, op, client.PageSize, client.MaxItems(maxEntities)))
		return err
	})
	if err != nil {
		return err
	}
	return decodeAll(raws, &s.Tags, "tag")
}

func (cr *crawler) strategies(ctx context.Context, s *Snapshot) error {
	return cr.patient(ctx, "routing strategies", func() error {
		var err error
		s.Strategies, err = drain(client.ListAll(ctx, cr.c, client.RoutingStrategies(),
			client.PageSize, client.MaxItems(maxEntities)))
		return err
	})
}

// decisionLogs reads one page of the most recent decision logs and, with
// Orders, the tags of the orders they routed.
func (cr *crawler) decisionLogs(ctx context.Context, s *Snapshot) error {
	size := min(cr.o.DecisionLogs, client.MaxSize)
	var page client.ListPage[json.RawMessage]
	err := cr.patient(ctx, "decision logs", func() error {
		var err error
		page, err = client.List(ctx, cr.c, client.RoutingDecisionLogs(client.RoutingDecisionLogsFilter{}), "", size)
		return err
	})
	if err != nil {
		return err
	}

	for _, raw := range page.Items {
		l, err := decodeDecisionLog(raw)
		if err != nil {
			return err
		}
		s.DecisionLogs = append(s.DecisionLogs, l)
	}
	if !cr.o.Orders {
		return nil
	}
	return cr.orderProfiles(ctx, s)
}

func (cr *crawler) orderProfiles(ctx context.Context, s *Snapshot) error {
	var refs []string
	seen := map[string]bool{}
	for _, l := range s.DecisionLogs {
		if l.OrderRef != "" && !seen[l.OrderRef] {
			seen[l.OrderRef] = true
			refs = append(refs, l.OrderRef)
		}
	}
	cr.notef("reading the tags of %d routed orders", len(refs))

	var mu sync.Mutex
	counts := map[string]*OrderProfile{}
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(cr.o.Concurrency)
	for _, ref := range refs {
		g.Go(func() error {
			var order struct {
				Tags []TagValue `json:"tags"`
			}
			err := cr.patient(gctx, "order "+ref, func() error {
				var err error
				order, err = client.Fetch[struct {
					Tags []TagValue `json:"tags"`
				}](gctx, cr.c, "get the order "+ref, func(ctx context.Context) (*http.Response, error) {
					return cr.c.API().GetOrder(ctx, ref)
				})
				return err
			})
			var apiErr *client.APIError
			if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
				// Deleted since it was routed. Its decision log still counts; its
				// tags are simply not known any more.
				return nil
			}
			if err != nil {
				return err
			}

			tags := slices.Clone(order.Tags)
			slices.SortFunc(tags, func(a, b TagValue) int {
				return strings.Compare(a.ID+"\x00"+a.Value, b.ID+"\x00"+b.Value)
			})
			key := NewProfile(tags).String()

			mu.Lock()
			defer mu.Unlock()
			if p, ok := counts[key]; ok {
				p.Orders++
			} else {
				counts[key] = &OrderProfile{Tags: tags, Orders: 1}
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}

	for _, p := range counts {
		s.OrderProfiles = append(s.OrderProfiles, *p)
	}
	// Most common first, then by name, so that two crawls of the same tenant
	// write the same file.
	slices.SortFunc(s.OrderProfiles, func(a, b OrderProfile) int {
		if a.Orders != b.Orders {
			return b.Orders - a.Orders
		}
		return strings.Compare(NewProfile(a.Tags).String(), NewProfile(b.Tags).String())
	})
	return nil
}

func decodeAll[T any](raws []json.RawMessage, out *[]T, what string) error {
	for _, raw := range raws {
		var v T
		if err := json.Unmarshal(raw, &v); err != nil {
			return fmt.Errorf("decode a %s: %w", what, err)
		}
		*out = append(*out, v)
	}
	return nil
}

func decodeDecisionLog(raw json.RawMessage) (DecisionLog, error) {
	var l struct {
		ID          string `json:"id"`
		Created     string `json:"created"`
		RelatedRefs struct {
			OrderRef string `json:"orderRef"`
		} `json:"relatedRefs"`
		Evaluation      json.RawMessage `json:"routingStrategyEvaluationResult"`
		SourcingOptions any             `json:"sourcingOptions"`
	}
	if err := json.Unmarshal(raw, &l); err != nil {
		return DecisionLog{}, fmt.Errorf("decode a decision log: %w", err)
	}
	var refs []string
	collect(l.SourcingOptions, "facilityConnectionRef", &refs)
	slices.Sort(refs)
	return DecisionLog{
		ID:          l.ID,
		Created:     l.Created,
		OrderRef:    l.RelatedRefs.OrderRef,
		Evaluation:  l.Evaluation,
		Connections: slices.Compact(refs),
	}, nil
}

// collect gathers every string value of key anywhere under v. A sourcing option
// nests its transfers a level or two deep depending on how the order was split,
// and the connection a transfer used is all a check needs from it.
func collect(v any, key string, out *[]string) {
	switch v := v.(type) {
	case map[string]any:
		for k, child := range v {
			if s, ok := child.(string); ok && k == key && s != "" {
				*out = append(*out, s)
				continue
			}
			collect(child, key, out)
		}
	case []any:
		for _, child := range v {
			collect(child, key, out)
		}
	}
}
