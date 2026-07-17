---
title: fft facility update
---

# fft facility update

Replace a facility (PUT)

Replace a facility with the contents of a JSON file.

This is a PUT: the facility becomes what the file says and loses anything the
file omits. To change one field and leave the rest alone, use 'fft facility
patch'.

The API has no If-Match header — optimistic locking travels in the body as a
"version" field — so fft reads the facility first to learn its current version,
sends that version back, and retries once if someone wrote in between. Your file
does not need a version; fft supplies it.

  fft facility get BER-01 -o json > facility.json
  $EDITOR facility.json
  fft facility update BER-01 --file facility.json

--if-version skips the read: fft sends the version you name and the API answers
409 if it is stale. That is one request instead of two, and a clean failure
instead of a silent overwrite — which is what a CI job wants. (It is
--if-version and never --version: cobra owns --version on the root command.)

## Usage

```
fft facility update <id> --file <file> [flags]
```

## Flags

```
      --example          Print a sample request body and exit
      --file string      JSON file holding the whole facility ('-' for stdin)
      --if-version int   Send this version instead of reading the current one (fails with 409 if it is stale)
```

## See also

- [fft facility](./fft_facility.md) — parent command

> This command also accepts the [global flags](./fft.md#flags).
