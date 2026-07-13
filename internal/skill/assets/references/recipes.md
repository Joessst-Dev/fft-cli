# Recipes

## Read, change, write back

Never build a write body out of what a table showed you. `-o table` is a summary and it
drops fields; a PUT built from it would delete the fields it dropped. Round-trip the JSON:

```sh
fft facility get BER-01 -o json > facility.json
fft facility update BER-01 --file facility.json
```

Better still, when the command has a flag for what you want to change, use `patch` — it
sends only that field, and cannot drop one it never saw:

```sh
fft facility patch BER-01 --name "Berlin Warehouse"
```

## Write anything at all

Every mutating endpoint in all three tiers takes the same three steps. The sample body is
synthesized from the schema and has every required field in it:

```sh
fft api addPickJob --example > pickjob.json
fft api addPickJob --file pickjob.json
```

`--file -` and `--data -` read stdin, so a body you built with `jq` never has to touch the
disk.

## Bulk writes, and what exit 8 means

```sh
fft stock upsert --file stocks.json -o json
fft listing upsert --file listings.json -o json
```

fft chunks the file and reports one result per item. Exit **8** means some items landed and
some did not, and the ones that landed are not rolled back. Under `-o json` every item comes
back with a `status`:

- `CREATED` / `UPDATED` / `UNCHANGED` — it worked.
- `FAILED` — it did not. The `reason` says why. Fix those items and re-send **only those
  items**.
- `UNKNOWN` — **the write landed and fft could not read the answer.** Do not re-send it. A
  stock entry with no id, re-sent, is created a second time. Go and look at what is there
  instead, with `fft stock list`.

Re-sending the whole file after an exit 8 is the mistake to avoid.

## Paging a big result

```sh
fft stock list --facility BER-01 --size 200 --all --max-items 1000
```

`--all` follows the cursor, which on a real tenant can be a great many requests. It stops
at `--max-items`, and **`--max-items` defaults to 10,000** — so `--all` is already bounded,
and lowering it is how you keep an exploratory question cheap.

**Being truncated is not an error.** When `--all` hits the limit, fft warns on **stderr**
and exits **0** with the items it got. So a piped `jq` sees a perfectly good array that is
not the whole answer, and the exit code will not tell you: if it matters whether you have
everything, read stderr, or ask for a `--total` (also stderr) and compare it against what
you received.

## Ids are not numbers

`-o json` prints the API's own bytes, re-indented and never re-encoded, so a 64-bit id or
version survives exactly as it was sent. Anything that parses JSON numbers as float64 —
JavaScript, a careless `jq` expression, a spreadsheet — will silently round it, and the
resulting id addresses a different object or none. Keep ids as strings, and use `jq -r`.

## Setting up a project

Only with the user. Never invent a tenant, and never put a password on a command line —
there is no `--password` flag, deliberately:

```sh
fft project add staging --base-url https://acme-staging.api.fulfillmenttools.com --api-key AIza... --username bot --project-id acme --env staging --password-stdin
fft project list
fft project use staging
```

If the user tells you a project is production, offer to protect it:

```sh
fft project read-only prod
```

## Running in CI, or on a machine with no config

Set the environment and fft touches neither the config file nor the keychain:

```sh
FFT_BASE_URL=https://acme.api.fulfillmenttools.com FFT_FIREBASE_API_KEY=AIza... FFT_EMAIL=bot@acme.com FFT_PASSWORD=secret FFT_READ_ONLY=1 fft facility list -o json
```

Every global flag has an `FFT_*` equivalent: `--output` is `FFT_OUTPUT`, `--project` is
`FFT_PROJECT`. `FFT_READ_ONLY=1` protects whichever project fft is about to use, and cannot
be talked back down by a flag.

## Seeing what fft actually sent

```sh
fft facility get BER-01 --debug
```

The full request and response trace goes to **stderr** with the secrets redacted, so
`--debug` composes with `-o json | jq` and does not corrupt it.
