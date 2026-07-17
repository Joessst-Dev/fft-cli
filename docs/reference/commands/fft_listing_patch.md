---
title: fft listing patch
---

# fft listing patch

Change some fields of one listing

Change some fields of one listing, leaving the rest alone.

  fft listing patch --facility BER-01 4711 --status INACTIVE
  fft listing patch --facility BER-01 4711 --title "Adidas Superstar" --price 89.95

Taking a listing INACTIVE removes it from the catalog of that facility without
deleting it, and without touching its stock. That is usually what you want when
an article should stop being offered.

fft reads the listing, applies your changes, and writes them back with the
version it just read — the API's optimistic locking lives in the request body,
not in an If-Match header, so a mutation is always a read-then-write. If someone
else wrote in between, the API answers 409 and fft reads again and retries,
once. A second 409 is not retried: at that point something is writing faster
than fft can read, and saying so is more useful than trying again.

--if-version skips the read: fft sends the version you name and the API answers
409 if it is stale. That is one request instead of two. (It is --if-version and
never --version: cobra owns --version on the root command.)

## Usage

```
fft listing patch --facility <id> <tenantArticleId> [flags]
```

## Flags

```
      --facility string   The facility, by tenantFacilityId or platform UUID (required)
      --if-version int    Send this version instead of reading the current one (fails with 409 if it is stale)
      --price float       New price
      --status string     New state: ACTIVE, INACTIVE
      --title string      New title
```

## See also

- [fft listing](./fft_listing.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
