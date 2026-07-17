---
title: fft facility list
---

# fft facility list

List facilities

List the facilities of your tenant.

This is POST /api/facilities/search with a cursor: it returns whole facilities
and pages properly, which the legacy GET list does not — that one returns a
reduced projection and cannot express these filters.

By default you get the first page. --all follows the cursor to the end, and says
so on stderr if it stops early rather than pretending it reached it.

  fft facility list --status ONLINE --type SUPPLIER
  fft facility list --all --size 250 -o json | jq -r '.[].tenantFacilityId'
  fft facility list --sort name:asc --total

stdout carries the facilities and nothing else. The total, the truncation notice
and every other remark go to stderr, so a pipe into jq is never contaminated.

## Usage

```
fft facility list [flags]
```

## Flags

```
      --all                         Page to the end and return every match, not just the first page
      --max-items int               With --all, stop after this many facilities (default 10000)
      --size int                    Facilities per page, 1–250 (default 20)
      --sort string                 Sort by one field, as field:asc or field:desc (id, name, status, type, tenantFacilityId, locationType, lastModified)
      --status strings              Only facilities in these states: ONLINE, SUSPENDED, OFFLINE
      --tenant-facility-id string   Only the facility with this tenantFacilityId
      --total                       Also count the matches, and report the total on stderr
      --type string                 Only facilities of this type: MANAGED_FACILITY or SUPPLIER
```

## See also

- [fft facility](./fft_facility.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
