---
title: fft network report
editLink: false
---

# fft network report

Draw a network snapshot as a self-contained HTML page

Draw a network snapshot as a page you open in a browser.

The page shows what 'fft network check' assumed (the hubs, the peer groups, the
kinds of order) and a coverage heatmap: one row per peer group of stores, one
column per kind of order, each cell the share of the group's stores a hub (or a
supplier, or any source) reaches. A gap most of a country shares, which no
outlier rule reports, is the dark row. Choose a cell to list the stores it
misses.

Choose a store to see its neighbourhood: every connection that could lead to
it, as 'fft network reach' lists them, drawn in bundles of the same kind of
source, result and reason. Where one context entry shuts out two thousand
connections, that is one line. It explains every facility a finding names and
the stores the cells list, up to --max-facilities; --facility adds others.

  fft network report
  fft network report --facility BER-01 --out gaps.html
  fft network report -o json | jq '.coverage[] | select(.kind == "hub")'

Every number is the check's own, from the same snapshot and fft-network.yaml;
the page only draws them. It is one file with nothing to fetch: it needs no
server, works offline, and its Content-Security-Policy forbids it any request.

The report holds the tenant's network, as the snapshot does. It is written
next to the snapshot, in the cache directory, readable by its owner only; keep
it out of a repository. stdout carries its path, or with -o json the data the
page draws (--out - prints the page instead, so it cannot be
combined with -o json). It sends no request, and exits 0 whatever it shows: 'fft network
check' is the gate.

## Usage

```
fft network report [flags]
```

## Flags

```
      --config string          The config file to follow, when it exists (default "fft-network.yaml")
      --facility stringArray   Explain this facility's connections too, by tenant id or platform id (repeatable)
      --max-facilities int     Explain at most this many facilities' connections (default 500)
      --out string             Write the page here instead of next to the snapshot; '-' prints it on stdout
      --snapshot string        Read this snapshot instead of the project's file in the cache directory
```

## See also

- [fft network](./fft_network.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
