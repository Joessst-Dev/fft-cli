---
title: Network
source: internal/skill/assets/references/network.md
---

# Network: finding routing gaps

Orders reach the wrong facility, or no facility, mostly because the connection rules have
**gaps**: a store no hub reaches, a `NOT` filter that shuts out one kind of order, a
connection to a facility that has gone offline. The API reports none of these, and no
single request can show them: there is no tenant-wide connection list, and most connections
name no target at all, only a `context` that decides which facilities they reach.

`fft network` reads the whole network once into a local snapshot and works on that,
offline. Everything it does is a read. **Fixing a gap is a change to where real orders go,
and that is the network owner's decision, not yours.**

## The workflow

```sh
fft network snapshot
fft network check
fft network check -o json
fft network reach --to BER-01 --tag orderType=express
fft routing strategy tree --order 0190d6e8-8c3a-7f1b-9d2e-4a6b8c0d1e2f
fft network infer --out -
fft network config validate
```

1. **`snapshot`** crawls every facility and every facility's connections, plus the facility
   groups, tags, routing strategies, node config categories, and one page of recent routing
   decision logs with the tags of the orders they routed.
   - It is one paged request per facility, so expect tens of minutes on a large network.
   - It allows itself 4h unless `--timeout` is set, reads `--concurrency` facilities at a
     time (3 by default), and backs off when a gateway throttles.
   - If it stops (an expired session, a gateway that will not recover, Ctrl-C), run the
     same command with `--resume`. A run without `--resume` discards the earlier progress,
     and says so.
   - The file goes to the cache directory, one per project and base URL, readable by its
     owner only. **Never commit a snapshot or paste it anywhere: it is the tenant's whole
     network.**
2. **`check`** runs the rules over the snapshot and sends no request. **Exit 0 means no
   problems; exit 11 means it ran and found some.** Any other code means it could not run,
   for example 3 when there is no snapshot.
3. **`reach`** explains one finding: every connection that could lead to a facility, for an
   order with the tags you give, and the first thing that rules each one out.
4. **`routing strategy tree`** explains the strategy side: the tree the router walks and,
   with `--order`, the branch it took for a real order.
5. **`infer`** and **`fft-network.yaml`** turn what the check guessed into what the team
   decided (see below).

## Reading a check

stderr says what the check **assumed**. Read it before the findings, because every
finding is only as good as these:

- **Hubs.** These are the facilities whose connections reach far more facilities than
  anybody else's. The cut goes at the largest jump in reach between neighbours, if that
  jump is at least 5×. Reach counts facilities, not connections: one wildcard connection
  reaches thousands.
- **Stores and suppliers.** Stores are the other online managed facilities; suppliers are
  the online `SUPPLIER` facilities.
- **Peer groups.** Stores are compared with the others in their country, and groups
  smaller than 10 are not compared.
- **Order profiles.** These are the tag combinations of recently routed orders, reduced to
  the tags some connection reads.
- **Coverage.** For each country, how many stores a hub and a supplier reach. **A gap most
  of a country shares is no outlier, so no finding reports it; only this line shows it.**

`-o json` gives each finding as `{rule, subject, facilities, profiles, detail, evidence,
next}`. Branch on `rule`, report `detail`, and start from `next`.

| Rule | What it finds |
| --- | --- |
| `no-source` | a store no online source reaches, for some order profile; the most severe |
| `peer-coverage` | a store no hub (or supplier) reaches, though one reaches at least 90% of its peers |
| `expect` | a declared expectation a store does not meet (needs a config) |
| `dangling-ref` | a target, exclusion or `context` value naming a facility, group or tag value that does not exist |
| `offline-ref` | an offline facility that connections still lead to, once per facility |
| `duplicate-edge` | connections of one source that are the same connection |
| `strategy-in-use` | no routing strategy in use, or more than one |
| `strategy-category` | a node's `categoryRef` that names no category |
| `strategy-inactive` | an inactive condition or node with nodes below it |
| `strategy-expired` | a node or condition whose time frames have all ended |
| `strategy-fallback` | a fallback facility that is missing or offline |
| `strategy-no-rating` | a path whose merged config has no active rating |
| `toolkit-hardcoded-id` | a facility id in a toolkit rule that names no facility |
| `config-drift` | the network has moved away from what `fft-network.yaml` pins |

A connection whose `context` depends on an item's category or the recipient's address is
**conditional**: it is never counted as coverage, and a finding says when one exists.

## Explaining a finding

```sh
fft network reach --to BER-11
fft network reach --to BER-11 --tag orderType=bulky --result excluded -o json
```

`reach` lists every connection from an online source whose target could be the facility:
one that names it, or a wildcard of its type.

- **`RESULT`** is `applies`, `conditional` or `excluded`.
- **`REASON`** names the first thing that rules the facility out, in the order the router
  meets it: the target, its exclusions, then each `context` entry
  (`context[2] FACILITY_GROUP does not list it`).
- **stderr** counts the results and names the reasons that exclude the most connections.
  On a real network one entry typically excludes thousands, and that line is the answer to
  give the user.

The `context` rules `reach` applies are the router's:

- entries combine with AND, and the values of one entry with OR;
- `FACILITY` and `FACILITY_GROUP` match the **receiving** facility;
- `NOT` means none of its values may match;
- a tag the order lacks fails a positive entry and passes a `NOT`.

## fft-network.yaml

```sh
fft network infer
fft network config validate
fft network config schema
```

`infer` writes a starter file next to the code, with the reason for every value as a
comment above it. It pins the hubs it found, the peer grouping, where the order profiles
come from, and one `expect` per country a hub already covers.

- **What stays in the file is pinned.** `check` follows it, and reports `config-drift` when
  the network moves away from it, so a changed result is a reviewed change to the file.
- **What is deleted is inferred again** on every run.
- `check` reads `./fft-network.yaml` when it exists. A file named with `--config` must
  exist, so CI cannot silently run without it.

```yaml
version: 1
roles:
  hub:
    facilities: [HUB-01, HUB-02]
peers:
  by: address.country
  minSize: 10
expect:
  - name: hub-coverage-de
    for: {role: store, where: {address.country: DE}}
    reachableFrom: {role: hub, atLeast: 2, maxTransitDays: 2}
suppress:
  - rule: hub-coverage-de
    facility: BER-11
    reason: opens 2027-01, hub connections are added at go-live
    until: 2027-01-15
```

- Run **`config validate`** before you propose any change to the file. It lists every
  problem with its line: unknown fields, values out of range, expectation names that clash,
  expired suppressions, and, with a snapshot, names that match nothing in the network.
  Exit 3 means the file has problems.
- `config schema` prints the JSON Schema, for an editor or before you write YAML.
- **A suppression needs a `reason` and an `until` date.** It applies through that day and
  then suppresses nothing; `check` warns about it and the finding comes back. Suppressed
  findings are counted on stderr, never hidden silently.

## In CI

Commit `fft-network.yaml`, take a snapshot in the job, and gate on the check:

```sh
fft network snapshot --out network.json.gz
fft network check --snapshot network.json.gz --config fft-network.yaml
```

Exit 11 fails the job with the findings on stdout. Anything else non-zero is the job
itself failing, not the network. Give the job a password-based session (see
[recipes](./recipes.md)), not a fixed `FFT_ID_TOKEN`: a fixed token cannot be renewed, and a
large crawl can outlast it. Keep the snapshot out of the job's artifacts.

## What you must not do

- **Never add a `suppress` entry, or loosen an `expect`, to turn a check green.** Propose it
  to the user with the reason and an expiry date. A person decides whether a gap is
  accepted.
- **Never change connections or the routing strategy to fix a finding** unless the user
  asks for that exact change. A finding describes the network; it does not authorise
  editing it.
- Do not read a `no-source` or `peer-coverage` finding as "the store gets no orders". A
  store fulfils from its own stock to the customer without any inbound connection. The
  finding says nothing **supplies** it, for the order profiles listed.
- Do not report tenant facility ids, group ids or tag values outside the user's own
  conversation.
