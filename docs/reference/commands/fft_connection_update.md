---
title: fft connection update
---

# fft connection update

Replace a connection (PUT)

Replace a connection with the contents of a JSON file.

This is a PUT, and there is no PATCH: the connection becomes what the file says
and loses anything the file omits. Unlike a facility, you cannot reach for the
safer verb — there isn't one — so the way to change one field is to read the
whole connection, edit it, and send it back:

  fft connection get 3f9c... --facility BER-01 -o json > c.json
  $EDITOR c.json
  fft connection update 3f9c... --facility BER-01 --file c.json

fft supplies the version. The API locks optimistically and carries the version
in the body rather than in an If-Match header, so fft reads the connection first
to learn the current one and retries once if somebody wrote in between.

The connection the API returns has no top-level "type" — it lives inside
"target" — while the body the API accepts requires both. fft fills the missing
one in from the target rather than making you do it, so the round trip above
works as written.

--if-version skips the read: fft sends the version you name and the API rejects
it if it is stale. That is one request instead of two, and a clean failure
instead of a silent overwrite, which is what a CI job wants.

## Usage

```
fft connection update <id> --facility <id> --file <file> [flags]
```

## Flags

```
      --facility string   The facility the connection leaves, by tenantFacilityId or platform UUID (required)
      --file string       JSON file holding the whole connection ('-' for stdin)
      --if-version int    Send this version instead of reading the current one (fails with 409 if it is stale)
```

## See also

- [fft connection](./fft_connection.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
