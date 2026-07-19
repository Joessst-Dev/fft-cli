# The emulator

`fft emulator` is a local server that mimics the fulfillmenttools API in memory. It is
for a demo, an automated test, or trying a command out without touching a real tenant —
it makes no request to any tenant and holds all its state in memory, so it forgets
everything the moment the process exits.

Every operation the API has is reachable on it, but only the top-level collections are
*remembered*; everything else is answered from a canned response. It can also publish
domain events to a **local** Google Pub/Sub emulator you run yourself — never to real
Google Cloud.

Run `fft emulator --help` and `fft emulator emit --help` for the flags. The notes below
are the things `--help` will not tell you.

## Starting it

```sh
fft emulator
fft emulator --port 9090
fft emulator --host 0.0.0.0 --port 8080
```

On startup it prints a recipe of `FFT_*` variables to **stderr** (stdout stays the data
contract, even here). Export them in another shell and every command runs against the
emulator instead of a tenant:

```text
fft emulator listening on http://localhost:8080

Point fft at it from another shell:
  export FFT_BASE_URL=http://localhost:8080
  export FFT_FIREBASE_API_KEY=emulator
  export FFT_EMAIL=dev@localhost
  export FFT_ID_TOKEN=emulator-token
```

- **`fft project add` does not work against the emulator.** Signing in reaches Google's
  identity service, which a local server cannot stand in for. The `FFT_ID_TOKEN` recipe
  above is the headless way in: a static token the emulator accepts and never verifies.
- `--host` defaults to `127.0.0.1`. The emulator has **no auth** — it accepts every
  request, token or not — so only widen it (`--host 0.0.0.0`, e.g. inside a container)
  when you mean to.
- `--verbose` logs every request (`METHOD URL -> STATUS (N bytes in)`) to stderr.

There is a container image too, `ghcr.io/joessst-dev/fft`. It runs the same binary, so
`emulator` and its flags are the arguments — and `--host 0.0.0.0` is not optional here,
since the default `127.0.0.1` answers only inside the container and the mapped port would
be dead:

```text
docker run --rm -p 8080:8080 ghcr.io/joessst-dev/fft emulator --host 0.0.0.0
```

Once the recipe is exported, drive it like any tenant:

```sh
fft facility create --file facility.json
fft facility list
```

## What is stateful, and what is synthesized

Only the **top-level REST collections** under `/api/{collection}` — facilities,
listings, stocks, orders, subscriptions, and the like — are stateful:

- A create is remembered, a get reflects it, an update bumps its `version`, a delete
  removes it. IDs are minted UUID-shaped, so `fft <noun> get <id>` addresses what you
  just created.
- **Optimistic locking is real.** An update whose body names a `version` that no longer
  matches is refused with a 409, exactly as the API does it — so you can rehearse the
  read-modify-write-retry loop offline.
- **Both pagination models work.** The plain GET lists page by `startAfterId` with a
  `{items, total}` envelope; the `POST /{collection}/search` endpoints page by opaque
  cursor. `--size`, `--all` and `--total` behave as they do against a tenant.
- **URN path parameters resolve.** A `urn:fft:facility:tenantFacilityId:<id>` in a path
  is matched against the stored entity, the same shorthand a real facility accepts.

Every *other* operation — nested resources, calculators, action endpoints — is answered
from a response synthesized from the spec: reachable, and shaped right, but not
remembered. All of it lives in memory and dies with the process.

## Seeding fixtures

Preload state from a directory of JSON files, one per collection, named
`<collection>.json`:

```sh
fft emulator --seed ./fixtures
```

So `fixtures/facilities.json` seeds the `facilities` collection, `fixtures/orders.json`
seeds `orders`, and so on. Each file is either a single object or an array of objects.
Unlike a live create, a seeded document **keeps the `id` and `version` it carries** — so
a fixture can pin the exact ids your test asserts on. There is no schema check: whatever
JSON you provide is stored as-is.

## Eventing

The emulator can publish a domain event to a Pub/Sub topic whenever you mutate a
stateful collection. It publishes to a **local Pub/Sub emulator you run yourself** and
**never to real Google Cloud** — its Pub/Sub client is pinned to the host you give it,
with authentication disabled and an insecure transport, so it physically cannot reach
anything else.

Point it at that host and eventing turns on:

```sh
fft emulator --pubsub-emulator-host localhost:8085
```

The flag defaults to `$PUBSUB_EMULATOR_HOST`, so exporting that before you start works
too:

```sh
PUBSUB_EMULATOR_HOST=localhost:8085 fft emulator
```

**Without a host, eventing is off**: subscriptions are still stored and matched, but
nothing is published, and startup says so on stderr.

### Registering where an event goes

A subscription is an ordinary stateful entity — `POST /api/subscriptions`. Give it a
`GOOGLE_CLOUD_PUB_SUB` target naming a `projectId` and a `topicId`; the topic is created
on first publish, so it need not exist yet:

```sh
fft api addSubscription --data '{"name":"orders","event":"ORDER_CREATED","target":{"type":"GOOGLE_CLOUD_PUB_SUB","projectId":"local","topicId":"orders"}}'
```

A subscription matches an event when **its `event` field equals the event name** and its
target is `GOOGLE_CLOUD_PUB_SUB`. Only that target type is delivered — webhook and Azure
Service Bus targets are stored but skipped.

Optional facility `contexts` narrow it further. Contexts are **AND-combined**: every
context must be satisfied by at least one of its own values, and a value matches when it
appears as a facility reference anywhere in the event payload:

```json
{
  "name": "berlin-orders",
  "event": "ORDER_CREATED",
  "contexts": [{ "type": "FACILITY", "values": ["berlin-warehouse"] }],
  "target": { "type": "GOOGLE_CLOUD_PUB_SUB", "projectId": "local", "topicId": "orders" }
}
```

A subscription with no `contexts` matches every occurrence of its event.

### What fires automatically, and what you emit by hand

A create, update or delete on a curated set of collections publishes a lifecycle event
on its own:

| Collection      | created                   | updated                   | deleted                   |
| --------------- | ------------------------- | ------------------------- | ------------------------- |
| facilities      | `FACILITY_CREATED`        | `FACILITY_UPDATED`        | `FACILITY_DELETED`        |
| facilitygroups  | `FACILITY_GROUP_CREATED`  | `FACILITY_GROUP_UPDATED`  | `FACILITY_GROUP_DELETED`  |
| users           | `USER_CREATED`            | `USER_UPDATED`            | `USER_DELETED`            |
| orders          | `ORDER_CREATED`           | `ORDER_MODIFIED`          | —                         |
| pickjobs        | `PICK_JOB_CREATED`        | —                         | —                         |
| packjobs        | `PACK_JOB_CREATED`        | `PACK_JOB_UPDATED`        | —                         |
| handoverjobs    | `HANDOVERJOB_CREATED`     | —                         | —                         |
| shipments       | `SHIPMENT_CREATED`        | `SHIPMENT_UPDATED`        | —                         |
| itemreturnjobs  | `ITEM_RETURN_JOB_CREATED` | `ITEM_RETURN_JOB_UPDATED` | —                         |
| stowjobs        | `STOW_JOB_CREATED`        | —                         | —                         |
| servicejobs     | `SERVICE_JOB_CREATED`     | —                         | —                         |

An empty cell, or a collection not in the table, emits nothing. The long tail of
state-transition events — a pickjob starting to be picked, a routing plan being routed —
maps to no create, update or delete, so you publish those yourself:

```sh
fft emulator emit PICK_JOB_PICKING_COMMENCED --payload-file pickjob.json
```

`emit` asks the running emulator to publish the named event, with the payload you supply
(an empty object if you omit `--payload-file`), to every subscription that matches the
event name and contexts. It reads `$FFT_BASE_URL` to find the emulator, so the exported
recipe points it automatically; `--url` overrides that. It reports the outcome on
stderr: how many subscriptions it published to, or that eventing is off, or that nothing
matched.

### The envelope on the wire

Each published Pub/Sub message carries a `WebHookEvent` JSON body:

```json
{
  "event": "ORDER_CREATED",
  "eventId": "b1e9…",
  "payload": { "…": "the entity as stored" }
}
```

`eventId` is one UUID per emit, shared across every target the event fans out to — it is
the consumer's dedup key when one occurrence reaches several subscriptions. Every message
also gets an `event` message **attribute** carrying the event name, so a consumer can
filter without decoding the body. That attribute is an emulator convention — it is not a
claim to reproduce production's message attributes.

## End to end: an order event, start to finish

This publishes an `ORDER_CREATED` event and reads it back. It uses the Google Cloud
SDK's Pub/Sub emulator (`gcloud components install pubsub-emulator`) and talks to it over
its REST API with `curl`, so it needs no extra client library; any local Pub/Sub
emulator on the same host works.

**1. Start a local Pub/Sub emulator**, then create a topic and a pull subscription to
read from. The emulator's REST API is on the same host you started it on:

```text
gcloud beta emulators pubsub start --host-port=localhost:8085

# in another shell, against that emulator's REST API:
curl -s -X PUT http://localhost:8085/v1/projects/local/topics/orders
curl -s -X PUT http://localhost:8085/v1/projects/local/subscriptions/reader \
  -H 'Content-Type: application/json' \
  -d '{"topic":"projects/local/topics/orders"}'
```

**2. Start the emulator** pointed at Pub/Sub, and export its recipe in another shell:

```sh
fft emulator --pubsub-emulator-host localhost:8085
```

```sh
export FFT_BASE_URL=http://localhost:8080
export FFT_FIREBASE_API_KEY=emulator
export FFT_EMAIL=dev@localhost
export FFT_ID_TOKEN=emulator-token
```

**3. Register a subscription** for the event:

```sh
fft api addSubscription --data '{"name":"orders","event":"ORDER_CREATED","target":{"type":"GOOGLE_CLOUD_PUB_SUB","projectId":"local","topicId":"orders"}}'
```

**4. Create an order** — the create publishes `ORDER_CREATED` automatically:

```sh
fft order create --example > order.json
fft order create --file order.json
```

**5. Pull the published event** from the Pub/Sub emulator:

```text
curl -s -X POST http://localhost:8085/v1/projects/local/subscriptions/reader:pull \
  -H 'Content-Type: application/json' \
  -d '{"maxMessages":10}'
```

Each `receivedMessages[].message` has an `attributes.event` of `ORDER_CREATED` and a
base64 `data` field that decodes to the `{event, eventId, payload}` envelope. For a state-transition event that no mutation triggers, publish it by
hand instead of step 4:

```sh
fft emulator emit PICK_JOB_PICKING_COMMENCED --payload-file pickjob.json
```

## A docker-compose sandbox

The walkthrough above starts the two servers by hand. This compose file starts both at
once — the fft emulator and a Pub/Sub emulator on one network — so `docker compose up`
gives you the whole eventing sandbox with nothing installed but Docker. It stands in for
steps 1 and 2; steps 3 to 5 (register a subscription, create an order, pull the event) are
run from your host, unchanged.

```yaml
services:
  emulator:
    image: ghcr.io/joessst-dev/fft:latest
    command: ["emulator", "--host", "0.0.0.0", "--pubsub-emulator-host", "pubsub:8085"]
    ports: ["8080:8080"]
    depends_on: [pubsub]
  pubsub:
    image: gcr.io/google.com/cloudsdktool/cloud-sdk:emulators
    command: ["gcloud", "beta", "emulators", "pubsub", "start", "--host-port=0.0.0.0:8085", "--project=local"]
    ports: ["8085:8085"]
```

Two details earn their keep. The fft emulator binds **`--host 0.0.0.0`** — its default
`127.0.0.1` answers only inside its own container, so the published port would be dead —
and it reaches Pub/Sub at **`pubsub:8085`**, the service name on the compose network, not
`localhost`. The Pub/Sub emulator binds `0.0.0.0:8085` for the same reason.

Bring it up, then point your host at both — the published ports make this identical to the
walkthrough, so the `FFT_*` recipe targets `http://localhost:8080` and the Pub/Sub REST
API is on `localhost:8085`:

```text
docker compose up

export FFT_BASE_URL=http://localhost:8080
export FFT_FIREBASE_API_KEY=emulator
export FFT_EMAIL=dev@localhost
export FFT_ID_TOKEN=emulator-token

curl -s -X PUT http://localhost:8085/v1/projects/local/topics/orders
curl -s -X PUT http://localhost:8085/v1/projects/local/subscriptions/reader \
  -H 'Content-Type: application/json' \
  -d '{"topic":"projects/local/topics/orders"}'
```

From here, register the subscription and create the order as in steps 3 and 4, and pull as
in step 5. The image is distroless and carries no shell, so drive `fft` from your host with
the recipe above rather than from inside the container. State still dies with the process:
`docker compose down` forgets everything, exactly as exiting the emulator does.

## Integration tests with Testcontainers

For a test suite that wants a fresh, disposable API per run — a random port, automatic
readiness, automatic teardown — drive the same image through
[Testcontainers](https://testcontainers.com). Two thin wrapper modules exist:

- **Go** — [`Joessst-Dev/testcontainers-fft`](https://github.com/Joessst-Dev/testcontainers-fft)
- **Java** — [`Joessst-Dev/fft-testcontainers-java`](https://github.com/Joessst-Dev/fft-testcontainers-java)

Both start `ghcr.io/joessst-dev/fft` with `emulator --host 0.0.0.0` and wait on the
**readiness signal**: `GET /api/status` answers `200` the moment the emulator is
listening, and needs **no token**. So the wait asserts the status code, not the body —
the emulator answers `/api/status` from its collection store, not with the live API's
`{"status":"UP"}`. (The stderr line `fft emulator listening on …` is the fallback signal
for a log-based wait.)

Go:

```go
ctx := context.Background()
c, err := fft.Run(ctx, fft.DefaultImage, fft.WithSeed("testdata/fixtures"))
defer testcontainers.TerminateContainer(c)
base, _ := c.BaseURL(ctx) // http://host:<mapped-port>
```

Java (JUnit 5):

```java
@Container
static final FftEmulatorContainer FFT =
        new FftEmulatorContainer().withSeed(Path.of("src/test/resources/fixtures"));
// FFT.getBaseUrl() -> http://host:<mapped-port>
```

`WithSeed` / `withSeed` copy a directory of `<collection>.json` fixtures into the
container and pass `--seed`, so a test seeds pinned ids the same way `--seed` does above.
For eventing tests, both modules can start a Pub/Sub emulator sidecar on a shared network
and wire the fft container to it — the container-native form of the compose sandbox below.
The module image tag is pinned to a tested emulator release and is overridable.

## Known limitations

- **Delivery is best-effort.** A publish that fails is logged and skipped; it never fails
  the mutation that triggered it. Publishing is synchronous on the request path and
  capped by a shared 10-second timeout.
- **Context matching is best-effort.** A context's declared `type` (FACILITY vs
  FACILITY_GROUP) is not distinguished — a value is matched against every facility
  reference found in the payload — and facility groups are not expanded to their member
  facilities, so a `FACILITY_GROUP` context matches only when the entity names that group
  directly.
- **Only `GOOGLE_CLOUD_PUB_SUB` targets are delivered.** Webhook and Azure Service Bus
  targets can be registered, but the emulator does not call them.

For whole tasks — seeding a project, paging a large result, running in CI — see
[recipes.md](recipes.md); for the curated commands you will drive it with, see
[commands.md](commands.md).
