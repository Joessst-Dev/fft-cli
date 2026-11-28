---
"fft": minor
---

`fft network report` now explains single facilities as well. Choose a store, from a heatmap cell or by name, to see every connection that could lead to it for a kind of order, as `fft network reach` lists them, counted into bundles of the same kind of source, result and reason and drawn as lines into the store. Where one context entry shuts out two thousand connections, that is one line. The report explains every facility a finding names and the stores the cells list, up to `--max-facilities` (500 by default); `--facility` adds others.
