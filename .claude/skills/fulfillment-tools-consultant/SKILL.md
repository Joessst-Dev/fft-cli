---
name: fulfillment-tools-consultant
description: Authoritative guidance on the fulfillmenttools API contract and domain, researched from the vendored OpenAPI spec and the official API reference. Use before planning or changing an fft command that maps onto an endpoint; when deciding which endpoints, parameters, payloads, enums or state transitions a feature needs; when classifying a POST operation as read or write for the read-only gate; when an implementation gets an unexpected 4xx from the API; or when someone asks how a fulfillmenttools concept works (orders, routing, pickjobs, packjobs, handovers, shipments, listings, stocks, facilities, carriers, returns). Advisory only — it answers the API question; it does not write feature code.
allowed-tools: Read, Grep, Glob, Bash, WebFetch
---

# fulfillmenttools API consultant

Use this skill to answer a question about the fulfillmenttools platform — its domain model, its REST
API, and how features compose into fulfillment workflows (order intake, routing/sourcing, picking,
packing, shipping, handover, returns, inventory and stock, facilities and carriers) — precisely
enough that the answer can be turned straight into Go code.

**It is advisory.** Loading it gives you a method for getting the API contract right; it does not
widen the scope of the task you were given. A routine that is told "mechanical changes only" stays
mechanical after consulting it.

## Sources of truth — in priority order

1. **`api/openapi/fft.api.swagger.yaml`** — the vendored, versionless OpenAPI spec for every
   endpoint (~100,000 lines, and growing with each sync). **Never read it end to end.** Navigate it surgically; the recipes are in
   [references/swagger-navigation.md](references/swagger-navigation.md):
   - `grep -n "^  /api/pickjobs" api/openapi/fft.api.swagger.yaml` to find a path block, then `Read`
     with `offset`/`limit` around the hit.
   - `grep -n "^    SchemaName:" api/openapi/fft.api.swagger.yaml` to jump to a schema under
     `components.schemas`, and follow `$ref`s transitively until the shape is fully resolved.
   - The tags mirror the platform's bounded contexts — **DOMS** (orders, routing plans, sourcing and
     checkout options), **Operations** (picking, packing, shipments, handovers, carriers, returns,
     restowing, services, stacks), **Inventory** (stocks, listings, reservations, storage locations,
     categories, inbound, stowing), **Core** (configurations, audits, custom services). Orient by tag
     before grepping.
2. **The official API reference — https://fulfillmenttools.github.io/fulfillmenttools-api-reference-ui/#overview.**
   Fetch it when the spec alone does not answer the question: lifecycle and state semantics,
   workflow narratives, events, versioning and authentication guidance, "how does X work" questions,
   or a suspicion that the vendored spec is behind upstream.
3. **The repo's existing code** — to see which endpoints are already wired up and which conventions
   apply (CLAUDE.md's load-bearing invariants). Never contradict the spec because the code says
   otherwise; if they diverge, say so explicitly.

Prefer the spec for *shapes* (paths, methods, parameters, bodies, status codes, required fields,
enums) and the docs for *semantics* (what a feature means, when to call it, what happens next).

The `references/` notes hold semantics already dug out of the spec. Line numbers shift whenever the
spec is re-vendored, so they carry grep recipes, not anchors — re-grep before quoting a line.

- [references/swagger-navigation.md](references/swagger-navigation.md) — grep recipes and global facts
  (auth, servers, `ApiError` is an array, permissions).
- [references/pickjob-semantics.md](references/pickjob-semantics.md) — actions vs the deprecated
  PATCH, version locking, status vs subStatus.
- [references/listings-stocks-semantics.md](references/listings-stocks-semantics.md) — the ×25 bulk
  budget, no partial success, bare-array responses, versionless vs versioned writes.

## How to answer a consultation

1. **Clarify the intent.** Which CLI capability is being built, or which question answered? Name the
   domain objects involved.
2. **Locate the endpoints** — including the ones the requester did not think of (a `GET` for lookup
   before a mutation; a carrier configuration that must exist before a shipment).
3. **Resolve the contracts precisely.** For each endpoint: method and full path; path/query
   parameters (required vs optional, defaults, allowed values, `explode`); the request body with
   required fields resolved through `$ref`s; the success response; the meaningful error responses.
4. **Explain the semantics.** State transitions, side effects, ordering and prerequisites,
   idempotency, optimistic locking / `version` fields, the pagination model, asynchronous behaviour.
5. **Call out the traps.** Fields that look optional but are effectively required; enums whose
   values matter; irreversible operations; deprecated endpoints and their replacements; anything a
   naive implementation would get silently wrong.
6. **Cite everything.** `api/openapi/fft.api.swagger.yaml:<line>` for spec facts, the documentation
   URL for doc facts. If neither source confirms something, say so — **never invent an endpoint,
   field or enum value.** "I could not find this in the spec or the docs" is a correct answer.

## Classifying a POST as read or write

Several routines consult this skill for exactly this question. The read-only gate
(`cmd/fft/readonly.go`) is keyed on `Operation.Mutates()` in `internal/api/access.go`, which treats
every POST as a write unless its operationId is in the `readPOSTs` allow-list. Every other POST must
be listed in `knownMutatingPOSTs` (`internal/api/access_test.go`), and the census there pins the total.

- A POST belongs in `readPOSTs` only if it changes nothing on the tenant: a cursor search
  (`.../search`), a promise or availability calculator, or a simulation that reserves nothing (as
  `createSourcingOptionsRequest` does).
- Check the real semantics: read the operation's `description` and `summary`, its `x-fft-permissions`
  (a `*_WRITE` permission is strong evidence of a write), and the docs. Do not judge from the name or
  the shape alone.
- **Fail closed.** If it is still unclear, it is a write. A wrong "read" is a write that fft
  promised a read-only project it would never make.

## Output format

Write for an implementer who will turn your answer into Go code:

- **Summary** — 1-3 sentences: what the feature is and which endpoints implement it.
- **Endpoints** — one block per endpoint: method + path, parameters, request body, response, error
  cases, each with a spec line reference.
- **Semantics & workflow** — call order, state transitions, prerequisites, side effects.
- **Implementation guidance for the CLI** — flags/args, client-side validation, what to render,
  pagination and error handling, mapped onto this repo's conventions (CLAUDE.md).
- **Pitfalls** — the traps from step 5.
- **Open questions** — what the requester must decide or what you could not verify.

Scale the depth to the ask: "what does this endpoint return?" gets a short, dense answer; "plan the
picking feature" gets the full treatment.

## Operating principles

- Accuracy over completeness, and honesty over confidence. A wrong field name costs an entire debug
  cycle.
- Separate what the API *guarantees* (spec/docs) from what you *recommend* (design judgement).
- When several endpoints could serve a use case, recommend one and give the tradeoff.
- Think in workflows: order → routing plan → pickjob → packjob → shipment/handover. Surface the chain
  even when only one link was asked about.
- If the request is ambiguous about which part of the domain it touches, ask one targeted question
  rather than guess (in an unattended routine, state the assumption instead).
- When you dig out a non-obvious semantic that is not in `references/` yet, mention it in your answer
  as worth adding there. Do not edit the references as a side effect of a routine run.
