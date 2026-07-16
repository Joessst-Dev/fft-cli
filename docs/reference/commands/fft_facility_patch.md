---
title: fft facility patch
---

# fft facility patch

Change some fields of a facility

Change some fields of a facility, leaving the rest alone.

Unlike 'fft facility update', this never deletes a field you did not mention.

fft reads the facility, applies your changes, and writes it back with the version
it just read — the API's optimistic locking lives in the body, not in an
If-Match header, so a mutation is always a read-then-write. If someone else wrote
in between, the API answers 409 and fft reads again and retries, once. A second
409 is not retried: at that point something is writing faster than fft can read,
and saying so is more useful than trying again.

  fft facility patch BER-01 --name "Berlin Mitte"
  fft facility patch BER-01 --status SUSPENDED

--if-version skips the read. Because the PATCH body is discriminated on the
facility's type and the read is where fft would have learned it, --type must then
be given too.

## Usage

```
fft facility patch <id> [flags]
```

## Flags

```
      --if-version int              Send this version instead of reading the current one (fails with 409 if it is stale)
      --name string                 New name
      --status string               New state: ONLINE, SUSPENDED, OFFLINE
      --tenant-facility-id string   New tenantFacilityId
      --type string                 The facility's type, required only with --if-version: MANAGED_FACILITY or SUPPLIER
```

## See also

- [fft facility](./fft_facility.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
