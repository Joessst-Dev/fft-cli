---
"fft": minor
---

`fft network report` draws a network snapshot as one HTML page you open in a browser: what `fft network check` assumed (the hubs, the peer groups, the kinds of order) and a coverage heatmap with one row per peer group of stores and one column per kind of order. Each cell shows the share of the group's stores that a hub, a supplier or any source reaches, so a gap a whole country shares shows up as a dark row. Choose a cell to list the stores it misses, the findings about each one, and the `fft network reach` command that explains it. The page is self-contained, works offline and is barred from making any request; it is written next to the snapshot, readable by you only. `-o json` prints the data the page draws.
