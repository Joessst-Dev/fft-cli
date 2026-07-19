---
name: fft
description: Drive the fft CLI against a fulfillmenttools tenant - read and change orders, facilities, interfacility connections, listings, stock, pick jobs, pack jobs and handovers, simulate how an order would be routed, and reach any of the API's 557 operations. Use whenever the user mentions fulfillmenttools, fft, an order, a pickjob or packjob, a handover, a listing, stock levels, or a facility or warehouse in that system - and whenever they ask why an order was routed or sourced the way it was, or where an order would be fulfilled from.
---

# Driving fft

`fft` is a CLI for the fulfillmenttools API. It signs in, refreshes tokens, and reaches
every operation the API has. You do not need an HTTP client, a token, or the API key: if
you find yourself reaching for `curl`, you have taken a wrong turn.

## Before anything else

```sh
fft project current
```

That names the tenant every following command will act on. **Check it before any write.**
A project called `staging` is a claim, not a guarantee, and the CLI will delete a
production facility just as happily as a test one.

Exit code 3 means no project is configured — see [recipes](references/recipes.md), and ask
the user rather than inventing credentials.

## Reading

Pass `-o json` whenever you are going to parse the answer:

```sh
fft facility list -o json
fft stock list --facility berlin-warehouse -o json
```

**stdout is data and nothing else.** Counts, notices, warnings, prompts and the
"new version available" banner all go to stderr, so a pipe is always safe and an empty
stdout genuinely means no results. Never scrape the table format; it is for humans.

## Finding the command — do not guess it

The surface has three tiers, and the discovery commands work offline, need no project, and
cost nothing:

```sh
fft api list --search pickjob
fft api describe getPickJob
fft api list --tag picking
```

`fft api list -o json` reports, for every operation, the `command` that runs it — the
fastest route from a word the user said to a command line you can run:

```sh
fft api list --search handover -o json
```

1. **Curated** — `fft order`, `fft facility`, `fft connection`, `fft listing`, `fft stock`, `fft sourcing`.
   Typed flags, real tables. Use these when they fit; see [commands](references/commands.md).
2. **Generated** — every other operation, as `fft <group> <operation>`, e.g.
   `fft picking get-pick-job`.
3. **Escape hatch** — `fft api <operationId>`, for anything at all.

Every command's `--help` carries the endpoint's summary, the permission it requires, and a
sample body. Read it. Do not guess a flag name — a guessed flag is a usage error at best,
and at worst it is a real flag that means something else.

## Writing

The workflow is the same for all three tiers: print the sample body, edit it, send it.

```sh
fft stock create --example > stock.json
fft stock create --file stock.json
```

Rules that are not optional:

- **Show the user the command and the body, and let them confirm, before any write.** They
  can see their tenant; you cannot.
- **Versions.** The API locks optimistically, and how you send the version depends on the
  command. A handful of curated commands take `--if-version` (`facility patch|update`,
  `facility coordinates set`, `listing patch`, `stock update`) — everywhere else it is a
  field *in the body*, which the `--example` body already has. Do not add `--if-version` to
  a command that has no such flag; check `--help`.
- **Exit 7** is a version conflict: the object changed under you. Re-read it, re-apply the
  change, re-send with the version you just read. Never retry the same body — you would be
  undoing someone's work on purpose.
- **Exit 8** is a partial bulk write: some items in the file landed and some did not. Read
  the per-item results and fix those items. Do not re-send the whole file blindly.

## Exploring safely

`--read-only` on any command refuses everything that would change the tenant, before it is
sent — no token, no request. It can tighten but never loosen, so it is safe to add to any
command line you are not sure about:

```sh
fft facility patch berlin-warehouse --name "Berlin" --read-only
```

**Exit 10** means fft itself refused a write because the project is read-only. Nothing left
the machine. This is not an authentication problem and there is nothing to route around:
tell the user, and let them decide.

## Working offline

`fft emulator` runs a local server that mimics the API in memory — for a demo, a test, or
trying a command out without touching a tenant:

```sh
fft emulator --port 8080
```

It prints an `FFT_*` recipe to export in another shell; once those are set, every command
runs against the emulator. The top-level collections (facilities, listings, stocks, orders)
are stateful — a create is remembered, a get reflects it, versions and pagination work —
and every other operation answers from a response synthesized from the spec. `fft project
add` does not work against it (signing in reaches Google's identity service); the printed
`FFT_ID_TOKEN` recipe is the way in. It holds all state in memory and forgets it on exit.

The emulator can also publish domain events to a **local Google Pub/Sub emulator** you run
yourself. Point it with `--pubsub-emulator-host host:port` (or `$PUBSUB_EMULATOR_HOST`);
without one, eventing is off and nothing is published — it never publishes to real Google
Cloud. Register where an event goes with an ordinary subscription
(`POST /api/subscriptions` with a `GOOGLE_CLOUD_PUB_SUB` target of `projectId`+`topicId`,
and optional facility `contexts`). A stateful mutation then publishes the matching lifecycle
event (a create on `orders` publishes `ORDER_CREATED`, and so on). To publish an event that
no create/update/delete maps to — a picking or routing state change — use `fft emulator emit
<EVENT> --payload-file <file>`, which asks the running emulator to publish it to every
subscription that matches the event name and contexts.

## Never

- Print, echo, paste or log an ID token. `fft auth token` exists for scripts, not for chat.
- Ask for a password on a command line. There is no `--password` flag; there is
  `--password-stdin`, and that is deliberate.
- Re-encode an id. They are strings that look like numbers and are up to 64 bits wide; a
  tool that parses them as floats will corrupt them silently. fft passes the API's own bytes
  through untouched — keep it that way.

## Read when you need it

- [references/commands.md](references/commands.md) — the curated commands, and the
  addressing rules that will otherwise bite you (a listing has no id of its own).
- [references/discovery.md](references/discovery.md) — getting from a word the user said to
  a command, across all 557 operations.
- [references/recipes.md](references/recipes.md) — whole tasks: set up a project, bulk
  upsert, page a large result, run in CI.
- [references/troubleshooting.md](references/troubleshooting.md) — every exit code, and what
  to actually do about it.
