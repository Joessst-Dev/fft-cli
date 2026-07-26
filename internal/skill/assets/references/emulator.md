# The emulator

`fft emulator` is a local server that mimics the fulfillmenttools API in memory. It is
for a demo, an automated test, or trying a command out without touching a real tenant —
it makes no request to any tenant and holds all its state in memory, so it forgets
everything the moment the process exits.

Every operation the API has is reachable on it, but only the top-level collections are
*remembered*; everything else is answered from a canned response. It can also deliver
domain events to a subscription's target — a webhook on your machine, a **local** Pub/Sub
or Azure Service Bus emulator you run yourself — but never to a real cloud or a real
remote endpoint.

Run `fft emulator --help` and `fft emulator emit --help` for the flags. The notes below
are the things `--help` will not tell you.

## Installing it

The emulator is a **component** — a separate binary fft ships but does not bundle, so that
a CLI most people use only to talk to a tenant does not carry a server and two cloud SDKs.
`fft emulator` on a fresh install explains this and gives you the one command that fixes
it:

```sh
fft component install emulator
```

The container image has it pre-installed, so `docker run …/fft emulator` needs no step.
Delivering events to a **local** Pub/Sub or Azure Service Bus emulator needs one more
component each — see [Eventing](#eventing).

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

Only the **top-level REST collections** under `/api/{collection}` — facilities, stocks,
orders, users, and the like — are stateful, and only through the plain shapes the
emulator can key state by: `GET`/`POST` on `/api/{collection}`, `GET`/`PUT`/`PATCH`/
`DELETE` on `/api/{collection}/{id}`, and `POST /api/{collection}/search`. A collection
the spec shapes differently is not stateful, however top-level it looks.

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

### Listings are the exception — do not rehearse them here

Nothing can write the listings store. The spec gives `/api/listings` only a `PUT` (the
bulk upsert) and a `/search`, and the per-facility verbs live under
`/api/facilities/{facilityId}/listings`, which is nested. So the store stays empty for
the life of the process and every listing command misleads differently:

| Command | Against the emulator |
| ------------------- | ------------------------------------------------------------- |
| `fft listing list` | always empty — it searches a store nothing can fill |
| `fft listing get` | a canned sample listing (`Adidas Superstar`, version 42) that was never written |
| `fft listing set` | reports `1 updated`, stores nothing |
| `fft listing patch` | renders the canned sample listing, unchanged by your patch |
| `fft listing upsert` | exits 8 — the one-result canned response cannot answer the two listings `--example` sends |

Rehearse a read-modify-write loop against facilities, stocks or orders instead. Against a
real tenant every one of these behaves normally.

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

The emulator can deliver a domain event to a subscription's target when you mutate one of
the collections in the lifecycle table further down — **not** every stateful one, despite
what the name suggests: `stocks` and `subscriptions` are stateful and emit nothing. All
three targets the API defines work, and each is bounded so the emulator can only ever
reach your own machine:

| Target | Delivered when | Reaches |
| --- | --- | --- |
| `WEBHOOK` | always | a **local** callback URL, unless you widen it |
| `GOOGLE_CLOUD_PUB_SUB` | the `emulator-pubsub` component is installed and `--pubsub-emulator-host` is set | that **local** Pub/Sub emulator, never real Google Cloud |
| `MICROSOFT_AZURE_SERVICE_BUS` | the `emulator-servicebus` component is installed and `--servicebus-emulator-host` is set | that **local** Service Bus emulator, never real Azure |

Webhook delivery is built into the emulator; the two broker transports are **components**
of their own, because they carry the cloud SDKs that made the emulator heavy. Install the
one you need:

```sh
fft component install emulator-pubsub
fft component install emulator-servicebus
```

Startup prints which of the three are live, on stderr. A target whose transport is not
installed — or installed but not pointed at a host — is stored and matched but never
delivered to, and the startup notice says which of the two it is.

To teach the emulator a broker it has never heard of — Kafka, NATS, SQS — write your own
transport component. It is a process speaking newline-delimited JSON on stdin and stdout;
Go authors get the protocol types and loop from `github.com/Joessst-Dev/fft-cli/pkg/transportproto`.
See [components](components.md).

### Webhook targets

A webhook needs no flag: the emulator POSTs the event envelope to the subscription's
`callbackUrl` as it stands. What it *will not* do is call a callback URL that could be
somebody's production endpoint — a subscription fixture copied from a real tenant names a
real address, and fake events must not turn up there. So a callback URL is delivered only
when its host is one of:

- a loopback, private, link-local or unspecified address (`127.0.0.1`, `[::1]`, `10.1.2.3`)
- `localhost` or `host.docker.internal`
- a name ending in `.localhost`, `.local` or `.internal`
- a single-label name with no dot — a service on a Docker network, e.g. `http://app:3000`

Anything else is skipped, with the reason on stderr. `--webhook-allow-remote` calls it
anyway. The judgement is on the URL's literal host, with no DNS lookup, and redirects are
not followed — a local endpoint cannot bounce the delivery somewhere remote.

The last rule is a convenience for container networks rather than a guarantee: a
single-label name is not *necessarily* local — a few TLDs resolve on their own, and a
resolver search domain can turn any bare label into a public name. It is a guard against
the fixture that names `https://api.acme.com`, not a sandbox.

The request is a `POST` with the `WebHookEvent` body below, `Content-Type:
application/json`, and every header the target's `headers` array declares. The emulator
adds no header of its own invention. A subscription registered the deprecated way — a
top-level `callbackUrl` and `headers` and no `target` object — is read as the webhook
target it means.

### Pub/Sub targets

Install the transport, then point the emulator at a local Pub/Sub emulator. The transport
is a separate process the emulator starts; its client is pinned to that host, with
authentication disabled and an insecure transport, so it physically cannot reach anything
else:

```sh
fft component install emulator-pubsub
fft emulator --pubsub-emulator-host localhost:8085
```

The flag defaults to `$PUBSUB_EMULATOR_HOST`, so exporting that before you start works
too:

```sh
PUBSUB_EMULATOR_HOST=localhost:8085 fft emulator
```

The topic is created on first publish, so it need not exist yet.

### Service Bus targets

Install the transport, then point the emulator at a local [Azure Service Bus
emulator](https://learn.microsoft.com/en-us/azure/service-bus-messaging/test-locally-with-service-bus-emulator)
and `MICROSOFT_AZURE_SERVICE_BUS` targets are sent over AMQP:

```sh
fft component install emulator-servicebus
fft emulator --servicebus-emulator-host localhost:5672
```

It is a **host**, not a connection string, on purpose: the credentials are then fixed to
the emulator's published development key over plain AMQP, which no real Azure namespace
accepts. Two consequences worth knowing before you debug a silent target:

- **The queue or topic must already exist** in the emulator's `config.json`. Unlike
  Pub/Sub, a Service Bus emulator cannot create an entity on demand, so a send to an
  undeclared one fails and is logged.
- **The target's `tenantId`, `clientId` and `clientSecret` are ignored.** They are
  Microsoft Entra credentials, and no local emulator honours Entra auth. Only
  `queueOrTopicName` is used to address the send; `namespace` is only a label.

### Registering where an event goes

A subscription is an ordinary stateful entity — `POST /api/subscriptions`. Give it the
target you want:

```sh
fft api addSubscription --data '{"name":"orders","event":"ORDER_CREATED","target":{"type":"WEBHOOK","callbackUrl":"http://localhost:3000/hook"}}'
```

```sh
fft api addSubscription --data '{"name":"orders","event":"ORDER_CREATED","target":{"type":"GOOGLE_CLOUD_PUB_SUB","projectId":"local","topicId":"orders"}}'
```

```sh
fft api addSubscription --data '{"name":"orders","event":"ORDER_CREATED","target":{"type":"MICROSOFT_AZURE_SERVICE_BUS","namespace":"local","queueOrTopicName":"orders","tenantId":"x","clientId":"x","clientSecret":"x"}}'
```

A subscription matches an event when **its `event` field equals the event name** and its
target type has a live transport.

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

A create, update or delete on a curated set of collections emits a lifecycle event on its
own:

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
maps to no create, update or delete, so you emit those yourself:

```sh
fft emulator emit PICK_JOB_PICKING_COMMENCED --payload-file pickjob.json
```

`emit` asks the running emulator to deliver the named event, with the payload you supply
(an empty object if you omit `--payload-file`), to every subscription that matches the
event name and contexts. It reads `$FFT_BASE_URL` to find the emulator, so the exported
recipe points it automatically; `--url` overrides that. It reports the outcome on stderr:
how many targets it delivered to, or that nothing matched — in which case the emulator's
own log says whether a subscription was skipped and why.

### The envelope on the wire

Every delivery carries the same `WebHookEvent` JSON body — as the Pub/Sub message data,
the Service Bus message body, or the webhook POST body:

```json
{
  "event": "ORDER_CREATED",
  "eventId": "b1e9…",
  "payload": { "…": "the entity as stored" }
}
```

`eventId` is one UUID per emit, shared across every target the event fans out to — it is
the consumer's dedup key when one occurrence reaches several subscriptions. A Pub/Sub
message also gets an `event` **attribute**, and a Service Bus message an `event`
**application property**, carrying the event name so a consumer can filter without
decoding the body. Both are emulator conventions — they are not a claim to reproduce
production's message metadata.

## End to end: a webhook, with nothing installed

The shortest complete loop. It needs no broker at all — only something on loopback that
answers a POST with a 2xx, which the `python3` one-liner below provides. (Plain
`python3 -m http.server` will not do: it answers a POST with 501, which the emulator
reports as a failed delivery.)

**1. Start a listener** on port 3000, in its own shell:

```text
python3 -c 'from http.server import BaseHTTPRequestHandler as H, HTTPServer
H.do_POST = lambda s: (print(s.rfile.read(int(s.headers["Content-Length"])).decode()), s.send_response(200), s.end_headers())
HTTPServer(("127.0.0.1", 3000), H).serve_forever()'
```

**2. Start the emulator** and export its recipe in another shell:

```sh
fft emulator --verbose
```

```sh
export FFT_BASE_URL=http://localhost:8080
export FFT_FIREBASE_API_KEY=emulator
export FFT_EMAIL=dev@localhost
export FFT_ID_TOKEN=emulator-token
```

**3. Register a webhook subscription** pointing at the listener:

```sh
fft api addSubscription --data '{"name":"orders","event":"ORDER_CREATED","target":{"type":"WEBHOOK","callbackUrl":"http://localhost:3000/hook"}}'
```

**4. Create an order** — the create delivers `ORDER_CREATED` automatically, and the
listener logs the `POST /hook`:

```sh
fft order create --example > order.json
fft order create --file order.json
```

Swap the callback URL for `https://example.com/hook` and the emulator refuses it instead,
saying so on stderr; `--webhook-allow-remote` makes it call anyway.

## End to end: an order event through Pub/Sub

This publishes an `ORDER_CREATED` event and reads it back. It uses the Google Cloud
SDK's Pub/Sub emulator (`gcloud components install pubsub-emulator`) and talks to it over
its REST API with `curl`, so it needs no extra client library; any local Pub/Sub
emulator on the same host works.

**1. Start a local Pub/Sub emulator**, then create a topic and a pull subscription to
read from. The emulator's REST API is on the same host you started it on:

```sh
gcloud beta emulators pubsub start --host-port=localhost:8085

# in another shell, against that emulator's REST API:
curl -s -X PUT http://localhost:8085/v1/projects/local/topics/orders
curl -s -X PUT http://localhost:8085/v1/projects/local/subscriptions/reader -H 'Content-Type: application/json' -d '{"topic":"projects/local/topics/orders"}'
```

**2. Start the emulator** pointed at Pub/Sub, and export its recipe in another shell.
Install the Pub/Sub transport first if you have not — the emulator's startup notice will
tell you if it is missing:

```sh
fft component install emulator-pubsub
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

```sh
curl -s -X POST http://localhost:8085/v1/projects/local/subscriptions/reader:pull -H 'Content-Type: application/json' -d '{"maxMessages":10}'
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

```sh
docker compose up

export FFT_BASE_URL=http://localhost:8080
export FFT_FIREBASE_API_KEY=emulator
export FFT_EMAIL=dev@localhost
export FFT_ID_TOKEN=emulator-token

curl -s -X PUT http://localhost:8085/v1/projects/local/topics/orders
curl -s -X PUT http://localhost:8085/v1/projects/local/subscriptions/reader -H 'Content-Type: application/json' -d '{"topic":"projects/local/topics/orders"}'
```

From here, register the subscription and create the order as in steps 3 and 4, and pull as
in step 5. The image is distroless and carries no shell, so drive `fft` from your host with
the recipe above rather than from inside the container. State still dies with the process:
`docker compose down` forgets everything, exactly as exiting the emulator does.

### Adding Azure Service Bus

Microsoft's Service Bus emulator wants two things the Pub/Sub one does not: a SQL Edge
sidecar, and a config file declaring every queue and topic up front — it creates none on
demand, so a target naming an undeclared entity simply fails:

```yaml
services:
  emulator:
    image: ghcr.io/joessst-dev/fft:latest
    command: ["emulator", "--host", "0.0.0.0", "--servicebus-emulator-host", "servicebus:5672"]
    ports: ["8080:8080"]
    depends_on: [servicebus]
  servicebus:
    image: mcr.microsoft.com/azure-messaging/servicebus-emulator:latest
    environment:
      ACCEPT_EULA: "Y"
      MSSQL_SA_PASSWORD: "Your_password123!"
      SQL_SERVER: sqledge
    volumes: ["./servicebus-config.json:/ServiceBus_Emulator/ConfigFiles/Config.json:ro"]
    ports: ["5672:5672"]
    depends_on: [sqledge]
  sqledge:
    image: mcr.microsoft.com/azure-sql-edge:latest
    environment:
      ACCEPT_EULA: "Y"
      MSSQL_SA_PASSWORD: "Your_password123!"
```

`servicebus-config.json` declares the queue the subscription will name:

```json
{
  "UserConfig": {
    "Namespaces": [
      {
        "Name": "sbemulatorns",
        "Queues": [{ "Name": "orders", "Properties": {}, "DeadletteringOnMessageExpiration": false }]
      }
    ],
    "Logging": { "Type": "File" }
  }
}
```

Then register a Service Bus subscription naming `orders` and create an order as before.
The `namespace` in the target is only a label — the host already picks the emulator's one
namespace — so anything there is fine.

## Integration tests with Testcontainers

For a test suite that wants a fresh, disposable API per run — a random port, automatic
readiness, automatic teardown — drive the same image through
[Testcontainers](https://testcontainers.com). Two thin wrapper modules exist:

- **Go** — [`Joessst-Dev/testcontainers-fft`](https://github.com/Joessst-Dev/testcontainers-fft)
- **Java** — [`Joessst-Dev/fft-testcontainers-java`](https://github.com/Joessst-Dev/fft-testcontainers-java)

Both start `ghcr.io/joessst-dev/fft` with `emulator --host 0.0.0.0` and wait on the
**readiness signal**: `GET /api/status` answers `200` with `{"status":"UP"}` the moment
the emulator is listening, and needs **no token** — the same shape the live API answers,
so `fft ping` works against it too. A wait that asserts only the status code is still the
safer one. (The stderr line `fft emulator listening on …` is the fallback signal for a
log-based wait.)

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
and wire the fft container to it — the container-native form of the compose sandbox above.
The module image tag is pinned to a tested emulator release and is overridable.

## Known limitations

- **Delivery is best-effort.** A delivery that fails is logged and skipped; it never fails
  the mutation that triggered it. Delivery is synchronous on the request path and capped
  by a shared 10-second timeout.
- **Context matching is best-effort.** A context's declared `type` (FACILITY vs
  FACILITY_GROUP) is not distinguished — a value is matched against every facility
  reference found in the payload — and facility groups are not expanded to their member
  facilities, so a `FACILITY_GROUP` context matches only when the entity names that group
  directly.
- **A remote webhook is skipped by default.** Only a callback URL whose host can only be
  local is called, unless `--webhook-allow-remote` says otherwise.
- **Azure targets use the emulator's development credentials.** The subscription's Entra
  credentials are ignored, and the queue or topic must already be declared in the Service
  Bus emulator's own config.

For whole tasks — seeding a project, paging a large result, running in CI — see
[recipes.md](recipes.md); for the curated commands you will drive it with, see
[commands.md](commands.md).
