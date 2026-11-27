---
"fft": patch
---

The agent skill (`fft skill`) and the docs site now cover finding routing gaps. A new network reference walks through the workflow: snapshot, check, explain a finding with `reach` and `routing strategy tree`, then pin decisions in `fft-network.yaml`. It covers every rule, the config file and gating CI on `fft network check`. It tells an agent to report findings and to propose a suppression, never add one. The CI guide gains a section on gating a job on exit 11.
