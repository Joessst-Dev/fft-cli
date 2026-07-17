---
title: fft facility coordinates
---

# fft facility coordinates

Set or remove a facility's coordinates

Set or remove a facility's geographic coordinates.

These are the only two things POST /api/facilities/{id}/actions can do — it is
not a state machine, and there is no transition hiding behind it. To change a
facility's state, use 'fft facility patch --status'.

Coordinates carry a version like any other mutation, so fft reads the facility
first unless --if-version tells it what the version is.

## Usage

```
fft facility coordinates
```

## Subcommands

- [fft facility coordinates remove](./fft_facility_coordinates_remove.md) — Remove a facility's coordinates
- [fft facility coordinates set](./fft_facility_coordinates_set.md) — Set a facility's coordinates

## See also

- [fft facility](./fft_facility.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
