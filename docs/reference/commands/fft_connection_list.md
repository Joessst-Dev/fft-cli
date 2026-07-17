---
title: fft connection list
---

# fft connection list

List the connections of a facility

List the connections that leave a facility.

These are the edges the routing engine may source along. A facility with no
connections cannot fulfil anything to anyone.

  fft connection list --facility BER-01
  fft connection list --facility BER-01 --target FRA-02
  fft connection list --facility BER-01 --all -o json | jq -r '.[].carrierKey'

This endpoint pages by id rather than by cursor, so a page of exactly --size is
always followed by one more request to prove there is nothing after it. --all
does that for you and stops at --max-items, saying so on stderr if it had to.

stdout carries the connections and nothing else. The total, the truncation
notice and every other remark go to stderr.

## Usage

```
fft connection list --facility <id> [flags]
```

## Flags

```
      --all               Page to the end and return every match, not just the first page
      --facility string   The facility the connection leaves, by tenantFacilityId or platform UUID (required)
      --max-items int     With --all, stop after this many connections (default 10000)
      --size int          Connections per page, 1–250 (default 25)
      --target string     Only connections that go to this facility, by tenantFacilityId or platform UUID
      --total             Also count the matches, and report the total on stderr
```

## See also

- [fft connection](./fft_connection.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
