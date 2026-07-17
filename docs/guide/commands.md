---
title: Commands
---

# The curated commands

Hand-written commands for the entities most people touch, plus the core commands that
have nothing to do with the API. Everything else lives in
[discovery.md](./discovery.md).

Run `--help` on any of them. It tells you the endpoint, the permission it needs, and a
sample body. The notes below are the things `--help` will *not* tell you.

## Orders

An order is the demand the platform fulfills, addressed by its platform id (the one
`list`, `search` and `get` print — orders have no tenantFacilityId-style shorthand).

```sh
fft order list --consumer-id C-4711
fft order get 7f3ce2a4-1f00-4c6a-9a2b-5d1f0b7a1c33
fft order search --status OPEN --sort orderDate:desc
fft order search --since 2026-07-01 --until 2026-07-15
fft order create --example > order.json
fft order create --file order.json
fft order update 7f3ce2a4-1f00-4c6a-9a2b-5d1f0b7a1c33 --file order.json
fft order cancel 7f3ce2a4-1f00-4c6a-9a2b-5d1f0b7a1c33 --reason-id out-of-stock
fft order unlock 7f3ce2a4-1f00-4c6a-9a2b-5d1f0b7a1c33
```

- `list` is the plain GET: it returns a *stripped* order (id, status, orderDate, line
  count, version) and filters only on `--tenant-order-id` and `--consumer-id`. For status,
  a date range or a sort, use `search`.
- `search` is a **BETA** endpoint guarded by `ADMIN_MODULES_READ`, not `ORDER_READ` — a
  token that lists orders fine may be refused here. Give it a narrow `--since`/`--until`; it
  pages far faster when the date range is bounded.
- Neither `list` nor `search` filters by facility. An order routes to a facility through its
  pickjobs, so filter pickjobs by facility instead.
- `update` is a PATCH, but `orderLineItems` is a **full replacement**: send them all or the
  ones you omit are deleted. Read the order, edit, send it back.
- `cancel --force` sends FORCE_CANCEL, which only works if the tenant permits it; it takes no
  `--reason-id`. Cancelling cannot be undone, so fft asks first (`--yes` answers).

## Facilities

A facility is addressed by its id.

```sh
fft facility list --status ACTIVE
fft facility get berlin-warehouse
fft facility create --example > facility.json
fft facility create --file facility.json
fft facility patch berlin-warehouse --name "Berlin Warehouse"
fft facility delete berlin-warehouse
fft facility coordinates set berlin-warehouse --lat 52.52 --lon 13.405
fft facility search --file query.json
```

- `patch` changes named fields; `update` replaces the whole object from a file. Prefer
  `patch` — it cannot silently drop a field you did not know about.
- `delete` is destructive and irreversible. Ask first, every time.

## Connections

A connection is an **edge of the fulfillment graph**: an outbound lane from one facility
to a `SUPPLIER`, to another `MANAGED_FACILITY`, or to the `CUSTOMER`. The router can only
source along an edge that exists, so this is the first place to look when an order routes
somewhere surprising — or refuses to route at all.

A connection belongs to the facility it leaves, so every command needs `--facility` as
well as the connection's own id:

```sh
fft connection list --facility berlin-warehouse
fft connection list --facility berlin-warehouse --target frankfurt-warehouse
fft connection get 3f9c1e77-2b4a-4f0e-9d61-8a2c5b7e4d10 --facility berlin-warehouse
fft connection create --facility berlin-warehouse --example --type SUPPLIER
fft connection delete 3f9c1e77-2b4a-4f0e-9d61-8a2c5b7e4d10 --facility berlin-warehouse
```

- **`update` is a PUT and there is no `patch`.** The connection becomes what the file says
  and loses anything the file omits — so unlike a facility, you cannot reach for the safer
  verb. Read the whole thing, edit it, send it back. fft supplies the `version`.
- The connection the API *returns* carries its type only inside `target`; the body it
  *accepts* wants one at the top level too. fft fills that in, so `fft connection get … -o
  json` piped back into `--file` works. Do not hand-edit it in.
- `--example` prints a body per type: `--type SUPPLIER`, `MANAGED_FACILITY` or `CUSTOMER`.
  A `CUSTOMER` target names no facility, and is not missing one.
- **`delete` removes a routing edge.** Orders stop being sourced along it immediately, and
  if it was the only way to reach a target, that target becomes unreachable. Ask first.

## Sourcing

**`fft sourcing simulate` changes nothing.** It is a POST, and it is a *read*: it runs the
router against a hypothetical order and hands back the ways it could be fulfilled. No order
is created, no stock is reserved, nothing moves. It works on a read-only project, and fft
knows it does — so do not refuse to run it on the grounds that POST means write.

```sh
fft sourcing simulate --example > order.json
fft sourcing simulate --file order.json --results 3
fft sourcing get 284f32cc-b106-487d-b633-f90d93d8c251
```

- **An empty answer is not "no matches".** It means the router could not fulfil the order
  at all, from anywhere. Say that, rather than reporting that the search came back empty.
- **The penalty is a penalty: lower is better.** The table is sorted so the router's
  favourite is first. Never present it as a score.
- **`UNSOURCED` is the partial-failure column.** An option can look healthy and still have
  quietly dropped items. If it is not zero, that option will not fulfil the whole order.
- `--results` defaults to 3 because the *API's* default is 1 — one option is an answer, not
  a set of options.
- Every transfer carries a `facilityConnectionRef`. That is the join to `fft connection get`,
  and it is how you answer "why *there*?" — see the recipe in
  [recipes.md](./recipes.md).

## Listings

**A listing has no id of its own.** It is addressed by the facility it is in *and* the
article it is for, so every command that addresses a single listing needs `--facility`
(the bulk `upsert` is the exception: it reads the facility from the file):

```sh
fft listing list --facility berlin-warehouse
fft listing get --facility berlin-warehouse ARTICLE-123
fft listing patch --facility berlin-warehouse ARTICLE-123 --status ACTIVE
fft listing delete --facility berlin-warehouse ARTICLE-123
```

- `fft listing set --facility <id> --file listings.json` is a **PUT**, and *all or nothing*:
  one bad listing rejects the whole file. But it is not a replacement of the catalog —
  listings the file does not mention are **left alone**. You do not have to send a catalog
  back to keep it.
- `fft listing upsert --file listings.json` is the bulk path, and the one you almost always
  want. It is chunked, and it reports per item — so it can exit 8 with some items written,
  where `set` would have written none.
- `fft listing purge --facility <id>` deletes every listing in a facility. There is no
  narrower way to say "I meant that".

## Stock

Stock has an id of its own, but you will nearly always arrive at it through the facility and
the article.

```sh
fft stock list --facility berlin-warehouse --tenant-article-id ARTICLE-123
fft stock get 7f3ce2a4-1f00-4c6a-9a2b-5d1f0b7a1c33
fft stock summary --facility berlin-warehouse
fft stock upsert --file stocks.json
fft stock actions --file action.json
```

- `stock summary` answers "how much of this do we have" without paging every stock record.
  Reach for it before you reach for `list`.
- `stock actions` is how stock is *moved and changed* (pick, book in, correct) rather than
  overwritten. If the user is describing a real-world event, this is usually the endpoint.
- `stock upsert` is bulk and chunked; exit 8 means some items landed.

## Paging

Every list and search takes the same flags:

```sh
fft stock list --facility berlin-warehouse --size 100
fft stock list --facility berlin-warehouse --all --max-items 500
fft facility list --total
```

- The default is one page. `--all` pages to the end, stopping at `--max-items` — which
  **defaults to 10,000**, so `--all` is bounded whether you say so or not. Lower it when the
  question is exploratory; a real tenant is a great many requests.
- The API has **two** paging models and they have different page sizes. The searches behind
  `facility`, `listing` and `stock` default to **20** per page; `fft connection list` is a
  plain GET and defaults to **25**. `--size` overrides either. Do not assume a page size —
  count the rows, or pass `--size`.
- Hitting that limit is **not an error**: fft warns on stderr and exits 0 with what it got,
  so the JSON alone cannot tell you the answer was cut short.
- `--total` prints the count **to stderr**, never into the JSON. Only on a single page does
  it ask the API to count: under `--all` fft prints the number it actually fetched, and — if
  the run was truncated — prints nothing at all, because then it cannot know the total.

## Core commands

```sh
fft project list
fft project current
fft project use staging
fft project read-only prod
fft auth whoami
fft ping
fft version
```

- `fft auth whoami` prints the permissions the current credentials actually have. When
  something exits 5, this is the command that explains why.
- `fft ping` needs no credentials at all. It is the way to tell "the tenant is down" apart
  from "my token is wrong".
- `fft project read-only prod` marks a project read-only for good, in the config file. It is
  the right suggestion whenever a user tells you a project is production.
