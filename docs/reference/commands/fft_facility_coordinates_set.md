---
title: fft facility coordinates set
---

# fft facility coordinates set

Set a facility's coordinates

Set a facility's geographic coordinates.

  fft facility coordinates set BER-01 --lat 52.5219 --lon 13.4132

Latitude runs from -90 to 90 and longitude from -180 to 180; fft checks that
before sending, because the API's complaint about a swapped pair is not obvious.

## Usage

```
fft facility coordinates set <id> --lat <latitude> --lon <longitude> [flags]
```

## Flags

```
      --if-version int   Send this version instead of reading the current one (fails with 409 if it is stale)
      --lat float        Latitude, -90 to 90
      --lon float        Longitude, -180 to 180
```

## See also

- [fft facility coordinates](./fft_facility_coordinates.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
