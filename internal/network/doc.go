// Package network is the offline half of `fft network`: the snapshot format a
// crawl writes, and the resolver that answers "which facilities does this
// connection reach, for this kind of order?" from it.
//
// Nothing here reaches the tenant except [Crawl], and Crawl only reads: every
// operation it sends is listed in [CrawlOperations], and a spec holds each of
// them to the read-only classification in internal/api. Everything else works on
// a [Snapshot] on disk, so a check can run in CI, on a laptop on a train, or
// a hundred times against the same data while a rule is being tuned.
//
// The semantics the resolver implements — how `context` entries combine, which
// facility FACILITY and FACILITY_GROUP match, what a missing tag does — were
// settled against real networks and are recorded in the consultant skill's
// connections-semantics.md. They are not configurable: they are what the router
// does, and a linter that disagreed with the router would be worse than none.
package network
