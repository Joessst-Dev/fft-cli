---
title: fft facility
---

# fft facility

Manage facilities

Manage the facilities of your tenant.

A facility is a place that fulfills orders: a store, a warehouse, or a supplier
you do not operate yourself. Facilities are polymorphic — every one is either a
MANAGED_FACILITY (you run it, it has picking times, capacity and services) or a
SUPPLIER (someone else runs it) — and the type is fixed at creation.

Every command that takes an &lt;id> accepts either the platform's UUID or your own
tenantFacilityId: fft wraps the latter as
urn:fft:facility:tenantFacilityId:&lt;id>, which every facility endpoint accepts.
So 'fft facility get BER-01' works without you ever looking up a UUID.

Reading is cheap and writing is versioned. Every mutation reads the facility
first to learn its current version and sends that version back — the API rejects
a write that carries a stale one. Pass --if-version to skip that read when you
already know the version; you will get a clean 409 instead of a silent
overwrite if you were wrong.

## Usage

```
fft facility [flags]
```

## Subcommands

- [fft facility coordinates](./fft_facility_coordinates.md) — Set or remove a facility's coordinates
- [fft facility create](./fft_facility_create.md) — Create a facility
- [fft facility delete](./fft_facility_delete.md) — Delete a facility
- [fft facility get](./fft_facility_get.md) — Show one facility
- [fft facility list](./fft_facility_list.md) — List facilities
- [fft facility patch](./fft_facility_patch.md) — Change some fields of a facility
- [fft facility search](./fft_facility_search.md) — Search facilities with a JSON query
- [fft facility update](./fft_facility_update.md) — Replace a facility (PUT)

## See also

- [fft](./fft.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
