---
title: fft connection
---

# fft connection

Manage interfacility connections

Manage the connections that leave a facility.

A connection is an edge of the fulfillment graph: an outbound lane from one
facility to a SUPPLIER, to another MANAGED_FACILITY, or to the CUSTOMER. The
routing engine can only source along an edge that exists — no connection means
that fulfillment path is not reachable at all, which makes this the first place
to look when an order routes somewhere surprising, or refuses to route.

A connection belongs to the facility it leaves, so every command needs
--facility as well as the connection's own id:

  fft connection list --facility BER-01
  fft connection get 3f9c1e77-2b4a-4f0e-9d61-8a2c5b7e4d10 --facility BER-01

--facility takes your own tenantFacilityId or the platform's UUID.

'fft sourcing simulate' is the other half of this: it shows which of these edges
the router would actually use, and names each one by its id.

## Usage

```
fft connection
```

## Subcommands

- [fft connection create](./fft_connection_create.md) — Create a connection
- [fft connection delete](./fft_connection_delete.md) — Delete a connection
- [fft connection get](./fft_connection_get.md) — Show one connection
- [fft connection list](./fft_connection_list.md) — List the connections of a facility
- [fft connection update](./fft_connection_update.md) — Replace a connection (PUT)

## See also

- [fft](./fft.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
