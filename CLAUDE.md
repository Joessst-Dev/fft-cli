# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

`fft` is a CLI for the fulfillmenttools fulfillment API. One Go binary, one auth path, one output contract, reaching every operation the API has.

## Commands

```sh
make build         # go build -trimpath -o fft ./cmd/fft
make test          # go test -race -shuffle=on ./...   (Ginkgo/Gomega)
make lint          # go vet + golangci-lint  — must be 0 issues
make generate      # regenerate the API client + op metadata from the spec
make fmt           # gofmt -s -w .
make snapshot      # build all release targets locally, as the tag build does
```

Run one spec or one suite (standard `go test`, since suites are Ginkgo):

```sh
go test ./cmd/fft/ -run TestFFT                 # one package's suite
go test ./cmd/fft/ -run TestFFT -args -ginkgo.focus="fft connection list"
```

`make generate` **must be a no-op on re-run** — CI fails the build on any diff in `internal/api/*.gen.go`. The upstream swagger is versionless and regenerated without notice, so after any spec change run `make generate` and commit the result; a red "no drift" job is the signal the spec moved under you.

## The three-tier command surface

This is the central design idea, and most changes touch it. The API has 557 operations; hand-writing 557 commands does not converge, so coverage comes in three tiers that share one binary:

- **Tier 1 — curated** (`fft facility`, `fft connection`, `fft listing`, `fft stock`, `fft sourcing`). Hand-written UX: typed flags, validation, readable tables. One file set per noun under `cmd/fft/` (`<noun>.go` + `<noun>_<verb>.go` + `<noun>_test.go`), modelled on the facility set.
- **Tier 2 — generated** (`cmd/fft/generated.go`). A command per remaining operation, auto-registered from the spec at startup, with real flags and a `--file`/`--example`.
- **Tier 3 — escape hatch** (`fft api <operationId>`, `cmd/fft/op.go`). Address any operation by id.

A curated command **shadows** its generated twin: the twin is the operationId the curated command declares in its `Annotations`, and `addGeneratedCommands` skips any claimed operation. So promoting an endpoint from Tier 2 to Tier 1 is a pure upgrade, never a breaking rename. **Every command that reaches the tenant MUST carry `Annotations: {annotationOperationID: "..."}`** — the read-only tree-walk spec (`readonly_test.go`) fails the build otherwise; a command that makes no request is excused in `commandsWithoutOperation`.

## Load-bearing invariants

**The output contract.** stdout is data and nothing else; totals, notices, warnings, prompts and the update banner all go to stderr, so a pipe is always safe and an empty stdout means no results. Under `-o json`/`-o yaml`, `Printer.RenderRaw` prints the API's *own bytes* re-indented, never fft's re-encoding of them — the table is fft's summary view, the JSON is the API's document. `Printer.Empty` writes `[]` (right for a list command, wrong for a single-object response — see the sourcing not-routable case, which must still print its document).

**Entities are carried as `entityDoc` (`map[string]any`), not the generated models.** oapi-codegen collapses this spec's `allOf`-with-siblings schemas — e.g. `InterFacilityConnection` becomes a bare `VersionedResource`, and target types lose `facilityRef`. Decoding a mutation through a lossy struct would silently *delete* fields on a PUT. So reads decode into a hand-written view struct (for the table only), writes go through the generated `...WithBody` variants with raw bytes, and `decodeDoc` uses `json.Decoder.UseNumber()` so 64-bit ids/versions survive. Do not "simplify" a command onto the generated model.

**Optimistic locking lives in the body, not a header.** Mutations read the entity to learn its `version`, send it back, and retry once on conflict (`client.UpdateVersioned` + `versionFlag`). `--if-version` skips the read for a clean 409. Exit 7 is a version conflict.

**A POST is a write unless proven otherwise.** The read-only gate (`cmd/fft/readonly.go`) is keyed on `Operation.Mutates()` (`internal/api/access.go`), which treats every POST as a mutation *except* the hand-curated `readPOSTs` allow-list — the cursor searches, the promise calculators, and `createSourcingOptionsRequest` (`fft sourcing simulate`, which reserves nothing). The census test in `access_test.go` fails the build until a newly-added POST is classified. Fail closed: a wrong "read" is a write fft promised not to make.

**Two pagination models, kept separate on purpose.** `internal/client/search.go` pages the POST `/{entity}/search` endpoints by opaque cursor (default size 20). `internal/client/list.go` pages the plain GET lists by `startAfterId`+`size` (default 25, envelope `{items, total}`, no cursor/hasNextPage). They are not unified — the defaults belong to the API, and `list.go` must cross-check the envelope's `total` because the GET endpoints declare no max `size` and a server that caps it would otherwise look like end-of-list. Both reuse `TruncatedError`/`MaxItems` so `--all` is always bounded and a truncated result always says so on stderr.

**Path params vs query filters resolve ids differently.** A facility path parameter accepts the `urn:fft:facility:tenantFacilityId:<id>` form (`client.FacilityRef`, no lookup). A *query filter* does not — the API answers a URN it cannot resolve with a cheerful empty 200 — so a filter value must be resolved to a platform UUID first (`resolveFacilityID`, one GET). See `fft connection list`'s `--facility` (path) vs `--target` (filter).

**Exit codes are the API** (`internal/exitcode`): 0 ok, 2 usage, 3 config/no-project, 4 auth (401), 5 forbidden (403), 6 not-found (404), 7 conflict (409), 8 partial bulk write, 9 upstream unavailable, 10 read-only refusal (nothing was sent), 130 interrupted. Commands return a typed error; `exitcode.FromError` maps it.

## Testing

Ginkgo v2 + Gomega, one suite per package. In `cmd/fft`, the `cli` harness (`main_suite_test.go`) gives each spec a `fakeTenant` that records every request (method, path, query, body) — assert on what went over the wire, on exact stdout tables, and on the exit code. Cross-cutting guard specs you must keep green when adding a resource: `readonly_test.go` (every command annotated), `generated_test.go` (shadowing), `access_test.go` (POST read/write census), and `skill_drift_test.go`.

The **agent skill** (`internal/skill/assets/`) is documentation compiled into the binary that an AI reads before driving `fft`. `skill_drift_test.go` walks every `fft ...` invocation in the skill and fails the build if it does not resolve against the real command tree — but only skill→tree, so **new commands can go undocumented and stay green**. When adding a Tier-1 noun, update `SKILL.md` (including the frontmatter `description`, the one line an agent reads before opening the skill) and `references/commands.md`.

## Widening coverage

Tier 2 and Tier 3 already reach every operation. To give a tag a *typed generated client*, add it to `output-options.include-tags` in `api/openapi/oapi-codegen.yaml` and run `make generate` (the spec is filtered by tag; unreached schemas are pruned). Two generators run under `make generate`, both writing to `internal/api/`: oapi-codegen → `fft.gen.go` (typed client + models), `tools/specgen` → `opmeta.gen.go` (summaries, permissions, and the synthesized `--example` bodies). Note that specgen cannot synthesize a coherent body for a discriminated `oneOf`; such commands hand-write their `--example` (see `fft stock create`, `fft connection create`).

## Release

Tag `vX.Y.Z` and push — `.github/workflows/release.yml` runs GoReleaser (cross-platform binaries, SBOMs, cosign signatures) and updates the Homebrew tap at `Joessst-Dev/homebrew-tap` (a cask, `Casks/fft.rb`). Requires the `HOMEBREW_TAP_TOKEN` secret. Version is injected via ldflags into `internal/buildinfo`.

## Conventions

Go 1.25. Follow the `golang-*` skills. Comments in this codebase explain **why**, not what — match that bar (the existing files are the reference); a comment that restates the code is noise. Commit messages and PRs carry no Claude attribution.
