---
title: fft listing purge
---

# fft listing purge

Delete every listing of a facility (destructive)

Delete EVERY listing of a facility.

This is DELETE /api/facilities/{id}/listings, and it does exactly what it says:
the facility's entire catalog is removed. There is no undo, and there is no
partial form — you cannot purge "the inactive ones".

Because the blast radius is the whole facility, purge is its own verb. It never
shares 'delete' with the single-listing case: a command that wipes a catalog
should not be one forgotten argument away from one that removes a single
article.

Before it asks, fft counts what it is about to destroy and shows you the
facility and the number:

  Purge all 4813 listings of facility urn:fft:facility:tenantFacilityId:BER-01?

--yes bypasses the question. On a non-interactive terminal without --yes, fft
refuses (exit 2) rather than assuming yes: a prompt nobody can see is not
consent.

Stock is a separate entity and is not touched — see 'fft stock'.

## Usage

```
fft listing purge --facility <id> [flags]
```

## Flags

```
      --facility string   The facility, by tenantFacilityId or platform UUID (required)
```

## See also

- [fft listing](./fft_listing.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
