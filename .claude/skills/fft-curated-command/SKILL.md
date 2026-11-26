---
name: fft-curated-command
description: The procedure for adding a hand-written (Tier-1) fft command — a new curated noun, a new verb under an existing one, or promoting a generated Tier-2 operation to a curated command. Use before writing any new file under cmd/fft/ that addresses an API operation, or when asked to "curate", "promote", or "give X a proper command". Covers the file set, the operationId annotation that shadows the generated twin, entityDoc reads and raw-byte writes, versioned mutations, choosing between the cursor and the startAfterId pagination, id resolution, the output contract, the docs to update, and the gates that must pass.
---

# Adding a curated fft command

A curated command is a pure upgrade over its generated twin: same operation, better UX, and the
twin disappears from the tree on its own. Everything below exists so that the upgrade never breaks
a guard spec, never drops fields from a document, and never breaks the output contract.

Read the facility set before writing anything. It is the model, and the names below point into it:
`cmd/fft/facility.go`, `facility_list.go`, `facility_get.go`, `facility_update.go`,
`facility_patch.go`, `facility_test.go`.

## 1. Know the operation before writing code

Load the **`fulfillment-tools-consultant`** skill. Settle the operationId(s), the path and query
parameters, the body schema, the enums, and the versioning. For a **POST**, it also settles whether
the call is a read. `Operation.Mutates()` (`internal/api/access.go`) treats every POST as a write
unless it is listed in `readPOSTs`, and the census in `internal/api/access_test.go` fails until a
newly added POST is classified there. Fail closed: if you are unsure, it is a write.

## 2. The file set

| File | Holds |
|---|---|
| `cmd/fft/<noun>.go` | `<noun>Long`, `new<Noun>Cmd(deps *Deps)` (parent, `RunE: cmd.Help`), the `get<Noun>`/`get<Noun>Doc` readers, the view struct, `<noun>List()` (a `listView`), `<noun>Rows`, `render<Noun>` |
| `cmd/fft/<noun>_<verb>.go` | `new<Noun><Verb>Cmd(deps *Deps)` + `<noun><Verb>Long` with runnable examples |
| `cmd/fft/<noun>_test.go` | the specs (see the **`fft-specs`** skill) |
| `cmd/fft/testdata/<noun>*.json` | fixtures the specs serve through `fixture(...)` |

Register the parent in the `AddCommand` list in `cmd/fft/root.go`, next to `newFacilityCmd(deps)`.
Every command uses `Args: usageArgs(...)`, so a wrong argument count exits 2 and does not reach
cobra's own error path.

## 3. The annotation is not optional

```go
Annotations: map[string]string{annotationOperationID: "replaceFacility"},
```

Every command that reaches the tenant carries one. It does two things:

- **Shadowing.** `addGeneratedCommands` (`cmd/fft/generated.go`) skips any operation a curated
  command has claimed, so the generated twin disappears, and its generated siblings land under the
  same noun. `generated_test.go` checks both.
- **The read-only gate.** `cmd/fft/readonly.go` decides per operation, and `readonly_test.go` walks
  the whole tree and fails on any unannotated command. A command that makes no request at all (pure
  local work) is excused by adding it to `commandsWithoutOperation` in `readonly_test.go` with a
  reason. That spec also fails when an excuse outlives its command.

## 4. Reads are documents, not models

- Read the raw bytes (`c.Do(ctx, "<what>", func(ctx) (*http.Response, error) { return c.API().GetX(ctx, ...) })`)
  and keep them. `-o json|yaml` prints *those bytes* through `Printer.RenderRaw` /
  `RenderDocument`, never fft's re-encoding of them.
- For the table, decode into a small hand-written view struct holding only the columns.
- For a read-then-write, decode into `entityDoc` with `decodeDoc` / `decodeVersioned`
  (`cmd/fft/entity.go`). They use `UseNumber`, so 64-bit ids and versions survive.
- **Never** unmarshal into a generated `api.*` model and send it back. The spec is versionless,
  and a struct silently drops every field the API added since the last `make generate`. A PUT built
  from it deletes those fields on the tenant.

## 5. Writes are raw bytes through `...WithBody`

Send with the generated `c.API().<Op>WithBody(ctx, ..., contentTypeJSON, body)` variant, never the
typed one.

**Versioned mutations** (PUT/PATCH-style entity writes) go through `putVersioned` (`entity.go`),
which wraps `client.UpdateVersioned`. It reads for the version, stamps it into the body, and
retries once on conflict:

```go
var version versionFlag           // registers --if-version (never --version: cobra owns it)
...
if err := version.check(); err != nil { return err }
get := func(ctx context.Context) (entityDoc, int, error) { return getFacilityDoc(ctx, c, ref) }
raw, err := putVersioned(ctx, c, "update facility "+ref, get, replaceWith(body), version.value(),
    func(ctx context.Context, body io.Reader) (*http.Response, error) {
        return c.API().ReplaceFacilityWithBody(ctx, ref, contentTypeJSON, body)
    })
```

For a patch, pass a mutation that merges into the document it read (see `facility_patch.go`)
instead of `replaceWith`. A stale `--if-version` must end in exit 7 with the current version named
on stderr.

A write that takes a body offers `--file` (`-` for stdin) and `--example`, made mutually
exclusive. `--example` is answered first, before any config or network
(`printCommandExample(cmd)`), so `Args` is `MaximumNArgs` and the id is demanded in `RunE`.
specgen synthesizes the example. It cannot do so for a discriminated `oneOf`, so hand-write one
there (see `fft stock create`, `fft connection create`), checked against a real tenant whose
identity stays out of the repo.

## 6. Lists: pick the pagination the endpoint has

| Endpoint | Runner | Client | Defaults |
|---|---|---|---|
| `POST /api/<entity>/search` | `runSearch` (`cmd/fft/search.go`) | `client.Search`/`SearchAll`, `internal/client/search.go` | cursor, size 20 |
| plain `GET /api/<entity>` | `runList` | `client.List`/`ListAll`, `internal/client/list.go` | `startAfterId`+`size` 25, `{items,total}` envelope |

Do not unify them, and do not invent a default size: those belong to the API. Register the paging
flags with `page.register(f, "<nouns>", client.DefaultSize)` for a search, or
`client.DefaultListSize` for a GET list. `--all` is bounded by `--max-items`
and always says so on stderr when it stops short (`TruncatedError`). An empty list prints
`Printer.Empty(noun)` (`[]` under `-o json`). A single-object command must never call `Empty`.

Validate enum flags locally with `enumValue` and add `registerEnumCompletion`, because the API's
400 for a bad enum does not name the field. Sort flags go through `parseSort` (one field, as
`field:asc|desc`).

## 7. Ids: path params and query filters differ

- **Path parameter:** `client.FacilityRef(arg)` wraps a tenantFacilityId as
  `urn:fft:facility:tenantFacilityId:<id>`, with no lookup.
- **Query filter:** the API answers a URN it cannot resolve with an empty 200, so resolve to a
  platform UUID first with `resolveFacilityID(ctx, c, value)` (one GET, skipped for a UUID).

`fft connection list` shows both: `--facility` (path, through `registerFacilityFlag`) and
`--target` (filter, resolved in its `listOpFn`).

## 8. The output contract

stdout is data and nothing else. Notices (`deps.Printer.Notef`), warnings (`Warnf`), totals,
prompts and truncation all go to stderr. An empty stdout means no results. Errors are typed
(`exitcode.UsageError{...}`, the client's HTTP errors), and `exitcode.FromError` maps them: 2 usage,
4 auth, 5 forbidden, 6 not found, 7 conflict, 8 partial bulk, 9 upstream, 10 read-only refusal.
A destructive command confirms through `deps.Prompt`, naming the entity in a way a human can check
(see `facilityIdentity`).

Comments explain **why**, never what. The existing files set the bar.

## 9. Documentation that is not drift-gated in this direction

`skill_drift_test.go` only checks that the agent skill's `fft ...` lines resolve. A new command
can stay undocumented and green, so do this by hand:

- `internal/skill/assets/SKILL.md`: the body, **and** the frontmatter `description` when you add a
  noun.
- `internal/skill/assets/references/commands.md`.
- `make docs`, which regenerates `docs/guide/*` from the assets and `docs/reference/commands/`
  from the command tree (`fft gen-docs`). Commit the result.
- If the noun list in `CLAUDE.md` ("Tier 1 — curated") is now incomplete, update it.

## 10. Done means all of these pass

```sh
make generate   # no diff in internal/api/*.gen.go
make docs       # no diff in docs/
make test       # readonly_test, generated_test, access_test, skill_drift_test, opcount_drift_test included
make lint       # 0 issues
```

Never edit a guard spec to make it pass. Fix the command instead.
