# Routing-strategy and decision-log semantics

How routing strategies, decision logs and `sourcing simulate` behave, as established against a
production-size and a pre-production network. The spec gives the shapes; most of what follows is
semantics it does not state. Grep for the quoted schema names; do not trust remembered line numbers.
Connection-side semantics (`context`, wildcard targets) are in
[connections-semantics.md](connections-semantics.md).

## Routing strategies (`RoutingStrategy`, `RoutingStrategyNode`, `RoutingStrategyCondition`)

- **The tree is a linked list of conditions.** A node's children are `nextCondition` → its
  `nextCondition` → …, each condition leading to its child through `nextNode`.
- **First match wins, in chain order.** Siblings are evaluated from the first; the first true
  condition's `nextNode` is taken and its later siblings are not evaluated (confirmed on 1,000+ levels
  of real decision logs). If none matches, "the last positively evaluated node serves as the final
  aggregation point" ([docs: Routing strategy](https://docs.fulfillmenttools.com/documentation/by-pillar/advanced-order-routing/routing-strategy)).
- **Config is inherited along the whole path**, not only from the root: "all settings from a previous
  node are inherited by the next one" and may be overridden or extended. Toolkit fences and ratings
  are matched by `referenceId`; standard ones by `implementation` (implied by the docs' example, not
  stated). Judge a node's effective fences and ratings on the merged path, never on the node alone.
- Inactive nodes are skipped with their configuration; deactivating a node also deactivates the
  nodes linked under it. A node applies only when `active` and an `ActivationTimeFrame` is valid.
- **Standard fences reference no facility.** `RoutingStrategyStandardFence.implementation` is an
  enum (`FenceImplementation`: stock, country, capacity, carrier availability…) evaluated at routing
  time — "a facility fenced out by every node" cannot be decided from configuration.
- **Toolkit rules can hard-code facility ids** — as `expectedValue`, and inside JSONPath filters of
  `propertyPath` (`$.customAttributes.<attr>[?(@.tenantFacilityId == '…')]`). A dangling-id check
  must read both, and must not treat a numeric `expectedValue` next to such a filter as an id.

## Decision logs

- Two kinds: `GET /api/routing/decisionlogs` (`RoutingDecisionLog`: `sourcingOptions` with
  `transfers[].facilityConnectionRef`, plus `routingStrategyEvaluationResult`) and
  `GET /api/routingplans/{routingPlanId}/decisionlogs/{routingRun}` (`DecisionLog`: per-facility
  `FacilityDecision` with `orderFences`, `orderLineItemFences`, `orderRatings`, `rank`).
- `routingStrategyEvaluationResult` = `{evaluatedPath, evaluatedConfig}`. Path elements carry
  `evaluationResult` `CONDITION_TRUE` / `CONDITION_FALSE` / `NODE_ACTIVE`; `evaluatedConfig` is the
  merged configuration the router used.
- `GET /api/routing/decisionlogs` **cannot be paged past the first page**: the API answers its own
  UUID ids as `startAfterId` with 404 "Invalid ObjectId string" (#318). fft reads one page as large
  as it allows and says so on stderr (#326); to see more, use the endpoint's filters (`orderRef`,
  `tenantOrderId`, `routingPlanRef`, `sourcingOptionsRef`).
- **`sourcing simulate` (`createSourcingOptionsRequest`) leaves no decision log**, neither by
  `tenantOrderId` nor by `sourcingOptionsRef`. A simulate experiment cannot read the strategy branch
  it took — keep the strategy out of it by varying only attributes the strategy never references.
- A simulate run stores a sourcing-options record (readable with `fft sourcing get`); it creates no
  order, reservation or carrier booking.

## Large tenants

- `POST /api/orders/search` with a date filter or a sort answers 400 "Query too complex" on a large
  order history, and unfiltered it returns the oldest orders first; fft explains the 400 (#327).
  Recent routings are easier to reach through decision logs.
- `GET /api/categories` caps `size` at 100. fft's generated list commands now enforce the spec's
  `size` bounds and offer `--all` (#330).
