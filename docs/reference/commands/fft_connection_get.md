---
title: fft connection get
---

# fft connection get

Show one connection

Show one connection of a facility.

&lt;id> is the connection's own id — the one 'fft connection list' prints, and the
one 'fft sourcing simulate' names as the facilityConnectionRef of every transfer
it chose. So an answer to "why did the router send this through Frankfurt?" ends
here, at the edge it used.

  fft connection get 3f9c1e77-2b4a-4f0e-9d61-8a2c5b7e4d10 --facility BER-01

-o json prints the API's own JSON, in full: the cutoff times, the non-delivery
days, the fallback costs and the context rules the table only counts.

## Usage

```
fft connection get <id> --facility <id> [flags]
```

## Flags

```
      --facility string   The facility the connection leaves, by tenantFacilityId or platform UUID (required)
```

## See also

- [fft connection](./fft_connection.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
