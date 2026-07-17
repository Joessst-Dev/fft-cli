---
title: fft facility get
---

# fft facility get

Show one facility

Show one facility.

&lt;id> is either the platform's UUID or your own tenantFacilityId. fft wraps the
latter as urn:fft:facility:tenantFacilityId:&lt;id> — a form every facility
endpoint accepts — so you can address a facility by the id you gave it:

  fft facility get 0090000042
  fft facility get 8f14e45f-ceea-467a-9575-25a1b5c8b3a1 -o json

-o json prints the API's own JSON, in full. The table is a summary.

## Usage

```
fft facility get <id>
```

## See also

- [fft facility](./fft_facility.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
