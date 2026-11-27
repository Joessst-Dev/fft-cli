---
title: fft network snapshot
editLink: false
---

# fft network snapshot

Read every facility and connection into a local snapshot

Read the whole fulfillment network into a local snapshot file.

The snapshot holds every facility, every facility's connections, the facility
groups, the tags, the routing strategies, and the most recent routing decision
logs together with the tags of the orders they routed. The other network
commands work on it offline, so a check costs no requests and can run in CI.

  fft network snapshot
  fft network snapshot --out network.json.gz --concurrency 2
  fft network snapshot --resume

There is no tenant-wide connection list, so a crawl is one paged request per
facility: a large network takes an hour or more. It reads --concurrency
facilities at a time, backs off for a minute (then longer) when a gateway
throttles, and records each facility as it finishes. If it stops — an expired
session, a gateway that will not recover, Ctrl-C — run it again with --resume to
continue where it stopped.

The global --timeout bounds the whole crawl, and its 30-second default would end
any real one, so unless you set --timeout yourself the crawl allows itself 4h.

The snapshot is written to the cache directory, one file per project, readable
by you only. Keep it out of a repository: it is the tenant's whole network.

stdout carries a summary of what was read (with -o json, a document holding the
snapshot's path); progress goes to stderr. Every request is a read.

## Usage

```
fft network snapshot [flags]
```

## Flags

```
      --concurrency int          How many facilities' connections to read at once (at most 8) (default 3)
      --decision-logs int        How many recent routing decision logs to read; 0 skips them and the orders (default 250)
      --no-orders[=true|false]   Do not read the orders the decision logs name (their tags make up the order profiles)
      --out string               Write the snapshot here instead of the project's file in the cache directory
      --resume[=true|false]      Continue a crawl that stopped, from the facilities it had already read
```

## See also

- [fft network](./fft_network.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
