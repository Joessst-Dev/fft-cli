---
title: fft routing decision-logs
---

# fft routing decision-logs

Show routing decision logs

Show why the router chose what it chose.

A decision log records one routing run: the strategy it evaluated, the sourcing
options it produced, and the refs — order, process, routing plan — that run belonged
to. It is the audit trail behind 'fft sourcing simulate' and every real order.

Filter by the thing you are investigating; the filters are exact-match ids, not
searches:

  fft routing decision-logs --order 0190d6e8-8c3a-7f1b-9d2e-4a6b8c0d1e2f
  fft routing decision-logs --tenant-order-id R456728546
  fft routing decision-logs --routing-plan &lt;ref> -o json | jq '.[].routingStrategyEvaluationResult'

The table is a summary; -o json carries each log in full. This endpoint pages by id,
so --all fetches every match and stops at --max-items.

## Usage

```
fft routing decision-logs [flags]
```

## Flags

```
      --all                       Page to the end and return every match, not just the first page
      --max-items int             With --all, stop after this many decision logs (default 10000)
      --order string              Only logs for this orderRef
      --process string            Only logs for this processRef
      --routing-plan string       Only logs for this routingPlanRef
      --size int                  Decision logs per page, 1–250 (default 25)
      --sourcing-option string    Only logs for this sourcingOptionRef
      --sourcing-options string   Only logs for this sourcingOptionsRef (the spec marks this filter deprecated; prefer --sourcing-option)
      --tenant-order-id string    Only logs for this tenantOrderId
      --total                     Also count the matches, and report the total on stderr
```

## See also

- [fft routing](./fft_routing.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
