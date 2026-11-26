# Listings and stocks API semantics

Non-obvious behaviour of the Inventory listings and stocks endpoints. Grep for the quoted schema
names to find them; do not trust remembered line numbers.

## Bulk listing upsert — `PUT /api/listings` (`ListingBulkUpsertPayload`)

- The discriminator is `targetingStrategy`: `SINGLE_FACILITY` → `ListingForBulkUpsertByFacility`
  (one `facility` object) or `MULTI_SELECTOR` → `ListingForBulkUpsertBySelector`, whose field is
  **`selector` — singular name, an array per entry**.
- The size limit is prose only, on the `listings` array's description: *"Number of entries times
  selectors must not exceed 25."* No `maxItems` enforces it. The budget counts (listing, facility)
  pairs: Σ len(entry.selector) for `MULTI_SELECTOR`, plus 1 per `SINGLE_FACILITY` entry. Chunk
  client-side or expect a 400.
- **There is no partial success.** `ListingBulkUpsertResponse` is
  `{listings: [successes only], summary: {created, updated, unchanged}}`: no failed count, no
  per-item reason. Failures are all-or-nothing at the HTTP level (400/409). `FAILED` exists only in
  `BulkOperationResultStatus`, which the legacy per-facility PUT uses.
- The same holds for `PUT /api/stocks`: `StockUpsertOperationResult.status` is only
  `UPDATED | CREATED | UNCHANGED`.

## Bare-array responses (no envelope)

- `PUT /api/facilities/{facilityId}/listings` → an array of `ListingBulkOperationResult`.
- `PUT /api/stocks` → an array of `StockUpsertOperationResult`.
- `PATCH /api/facilities/{facilityId}/listings/{tenantArticleId}` → **an array of `Listing`, even
  for a single patch.**

## Pagination

- GET lists: `startAfterId` + `size`. Listings: default 25, **no declared maximum**. Stocks: default
  25, maximum 1000. Stock **summaries** paginate by the article's `tenantArticleId`, not by an id.
- `POST …/search`: cursor `after`/`before`/`last` with `pageInfo.endCursor`; `size` defaults to 20,
  maximum 250; `total` only with `options.withTotal: true`. `startAfterId` on search payloads is
  deprecated.
- `GET /api/stocks` returns a `pageInfo` envelope although it only accepts `startAfterId`.

These two models are kept separate in `internal/client/list.go` and `internal/client/search.go` on
purpose; see CLAUDE.md.

## Versionless vs versioned writes

`PUT /api/stocks/{stockId}` and `StocksForUpsert` choose the branch by the **absence** of `version`
(`not: {required: [version]}`). A Go struct with a non-pointer `Version` and no `omitempty`
serialises `"version": 0`, lands in the versioned branch, and gets a 409. Hand-written bodies must
make `version` a pointer, or omit it.

## Smaller traps

- `explode` differs between the twin filters: listings' `tenantArticleIds` is comma-separated
  (`explode: false`); stocks' `tenantArticleId` is an array sent as a repeated parameter.
- `StrippedListing` lists `facilityId` in `required` but never defines the property. It is a spec
  bug; the field is not on the wire.
- `StockVersionlessUpsertActionResult.operationResults` is a single object despite the plural name.
