package api

import "net/http"

// Whether an operation writes is a fact about the spec, so it is answered here,
// next to the rest of the spec metadata, rather than in the command that happens
// to ask. See readonly.go in cmd/fft for the gate that consumes it.

// Mutates reports whether the operation changes state on the tenant.
//
// The method decides — except for POST, which the fulfillmenttools API uses for
// both halves of its world. POST /api/facilities/search is a *read*: a cursor
// search cannot express its query in a query string, so it posts one, and the
// whole list tier of the CLI is built on that. POST /api/facilities creates a
// facility. The method alone therefore cannot answer the question.
//
// Nor can the path: "does it end in /search?" catches 31 of the 43 reads and
// misses the rest. Nor can the status code: 41 of the mutating POSTs answer 200
// rather than 201, so a "200 means read" heuristic would wave through creates.
//
// So a POST is a mutation unless its operationId is in [readPOSTs], and that list
// is hand-curated. A POST the spec grows tomorrow is a mutation until a human says
// otherwise, and the census in access_test.go fails the build until one does.
// Fail closed: the cost of a wrong "read" is a write that fft promised not to make.
func (o Operation) Mutates() bool {
	switch o.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	case http.MethodPost:
		return !readPOSTs[o.ID]
	default:
		// PUT, PATCH, DELETE — and any method the spec has not used yet.
		return true
	}
}

// readPOSTs are the POST operations that compute an answer without changing
// anything, keyed by the operationId exactly as the spec spells it. Three ids in
// the spec contain spaces, so nothing may derive a Go identifier from one: these
// are map keys and nothing else.
//
// What earns a place here:
//
//   - the 31 search* operations — POST-bodied cursor searches, the read path
//     behind every list command fft has.
//   - the promise calculators. They compute delivery and collect options; the
//     docs are explicit that only a partial routing is performed and the
//     fulfilling facility is not decided, so nothing is reserved.
//   - the routing dry-runs, which return an evaluation result and create no
//     resource.
//   - validatePostalCode, getNeedsPacking, downloadMergedDocuments (it merges
//     documents it is given and hands back the PDF), and createSourcingOptionsRequest
//     (it reserves no stock).
//
// What is deliberately absent, despite reading like a read — named here so that
// nobody adds them later on the strength of the name alone:
//
//   - postDeliveryPromise sits in the /promises family with the calculators, and
//     is not one of them: it performs a routing decision, reserves the stock and
//     creates a PROMISED order.
//   - createSimulationOrder persists the simulation. There is a GET for it, and a
//     search over them.
//   - createLookupRecord is an upsert that answers 200.
//   - executeGraphQLCommand is mutation-capable by construction, and fft cannot
//     see into the document to know which it is this time. It is the one operation
//     whose classification is refused rather than guessed.
//   - calculateBestCarrier reads like a pure calculator — "Calculate best carriers",
//     a request body carrying only a product value, a response carrying only
//     rankings — and the spec nonetheless guards it with CARRIER_WRITE. It may well
//     be a mis-assigned permission. But the doctrine of this file is that a POST is a
//     write until a human can say otherwise, and "the only evidence available says
//     write" is not otherwise. A read-scoped token would be refused it anyway.
var readPOSTs = map[string]bool{
	// Cursor searches.
	"searchCategory":                 true,
	"searchFacility":                 true,
	"searchFacilityGroup":            true,
	"searchHandoverJob":              true,
	"searchInboundProcess":           true,
	"searchLinkedServiceJobs":        true,
	"searchListing":                  true,
	"searchLookupRecordItem":         true,
	"searchNotification":             true,
	"searchOrder":                    true,
	"searchOrderRecord":              true,
	"searchPackJob":                  true,
	"searchParcel":                   true,
	"searchParcelInformation":        true,
	"searchPickJob":                  true,
	"searchProcess":                  true,
	"searchRemoteConfiguration":      true,
	"searchReservation":              true,
	"searchRole":                     true,
	"searchRoutingPlan":              true,
	"searchScopedAvailabilityConfig": true,
	"searchShipment":                 true,
	"searchShippingInformation":      true,
	"searchSimulationOrder":          true,
	"searchStock":                    true,
	"searchStorageLocation":          true,
	"searchStowJob":                  true,
	"searchTag":                      true,
	"searchUser":                     true,
	"searchWorkflow":                 true,
	"searchZone":                     true,

	// Promise calculators.
	"postCheckoutOptions":              true,
	"checkoutoptionCollectEarliest":    true,
	"checkoutoptionDeliveryEarliest":   true,
	"checkoutoptionDeliveryTimePeriod": true,
	"checkoutOptionsTimepoint":         true,

	// Routing dry-runs.
	"evaluateRoutingStrategy":     true,
	"evaluateRoutingStrategyNode": true,

	// Validators, calculators and one export.
	"validatePostalCode":           true,
	"getNeedsPacking":              true,
	"downloadMergedDocuments":      true,
	"createSourcingOptionsRequest": true,
}
