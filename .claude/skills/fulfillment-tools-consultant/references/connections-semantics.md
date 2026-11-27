# Interfacility connections — semantics

## The list response carries an undeclared top-level `type`

`GET /api/facilities/{facilityId}/connections` returns each connection with a top-level `type`
field (observed values `MANAGED_FACILITY` and `CUSTOMER`) that is a copy of `target.type`.
The vendored spec does not declare it: the `InterFacilityConnection` schema lists its properties
without a `type`, and sets `additionalProperties: false`. Re-verify with
`grep -n "^    InterFacilityConnection:" api/openapi/fft.api.swagger.yaml` and read the block.

Consequences:

- **fft is unaffected.** Connections are carried as `entityDoc` and written back as raw bytes, so the
  extra field survives a read-modify-write. Do not "fix" this by moving a command onto the
  generated model, which would drop it.
- **Anything that validates responses against the spec fails every connection** on that field.
  Treat it as upstream drift, not as a malformed response.
- **On reads, `target.type` is the discriminator** (`CUSTOMER`, `MANAGED_FACILITY`,
  `SUPPLIER`). The top-level copy in list responses is not contractual, and `SUPPLIER`
  targets are not confirmed to carry it.
- **On writes, top-level `type` is declared and required.** Every
  `InterFacilityConnectionTo*ForCreation` / `...ForUpdate` schema, and the
  `InterFacilityConnectionForCreation` / `ForUpdate` umbrella schemas, lists it in `required`.
  Do not strip it from a create or update body: that is a 400. fft's `normaliseConnectionType`
  (`cmd/fft/connection.go`) fills it in from `target.type` when a body taken from a read lacks
  it, and refuses a body where the two disagree; do not "simplify" it away on the strength of
  this note.
- **Reconciling with the code comments.** `connection_update.go`'s help text and the comment in
  `cmd/fft/connection.go` say the read model carries `type` only inside `target`. The list
  response above partly contradicts that, but the fill-in is still needed whenever a read
  lacks the field (a `SUPPLIER` target, or a single-connection GET where it has not been
  observed).

Tracked in https://github.com/Joessst-Dev/fft-cli/issues/324 (label `spec-drift`). When the field
appears in the upstream spec, `make generate` will show it and this note can go.

## Listing a whole network

- **Per facility only.** There is no tenant-wide connection list or search, so a whole network costs
  one paged GET per facility. The envelope is `{interFacilityConnections, total}`, not
  `{items, total}`; default `size` 25.
- **Targets can be wildcards.** For `MANAGED_FACILITY` and `SUPPLIER` targets, `target.facilityRef`
  is optional, beside `excludedFacilityRefs` and `excludedFacilityGroupRefs`. A connection without
  `facilityRef` points at *every* facility of that type; which ones it really reaches is decided by
  its `context`. Networks exist where no connection names a target at all — reading only
  `target.facilityRef` silently drops every edge.

## How `context` evaluates (`InterFacilityConnectionContext`)

Settled from the docs, from the connections the router actually used (each transfer of a sourcing
option names its edge in `facilityConnectionRef`), and from controlled `sourcing simulate` runs:

| Rule | Source |
| --- | --- |
| Entries combine with AND; the values of one entry with OR | [docs: Context](https://docs.fulfillmenttools.com/documentation/apis/context) |
| `FACILITY` and `FACILITY_GROUP` match the **receiving** facility | [docs: Facility connection](https://docs.fulfillmenttools.com/documentation/by-pillar/advanced-order-routing/complex-routing-with-combinatorics/network-model/facility-connection.md); every entry on used edges passes under this reading |
| `operator: NOT` with several values means **none** may match | simulation: one value from a two-value `NOT` list removed the edge |
| A tag the order lacks **passes** a `NOT` entry | simulation |
| A tag the order lacks **fails** a positive entry, exactly like a wrong value | simulation (pre-production, a tag the strategy never references) |
| `TAG_REFERENCE` values are the tag's `allowedValues`, compared with the order's `tags[].value` | every referenced value is an `allowedValue`; the form (e.g. prefixed with the tag id) is a tenant convention, not a rule |
| `CATEGORY` is the item's category (listing side, not on the order line) | docs example (dangerous goods) |
| `TARGET_POSTALCODE` / `TARGET_COUNTRY` refer to the final recipient's address | docs |
| A connection without `context` applies to every request | docs |

- The routing graph has three layers: supplier, an intermediate hop, the target
  (`ConnectionsAnalysisResult`: `canSupplyIntermediateHop`, `canBeIntermediateHop`,
  `canSupplyTarget`).
- The router's own explanation — `SourcingOptionAnalyzeAction…` with per-facility
  `ConnectionRefusedAnalysis` (`containsInvalidContexts`, `containsInvalidTargets`) — is in the
  schemas but **no path exposes it yet**. Re-grep after each spec sync: once exposed, it is the
  authoritative check for the rules above.
- Strategy-side semantics (trees, inheritance, decision logs) are in
  [routing-strategy-semantics.md](routing-strategy-semantics.md).
