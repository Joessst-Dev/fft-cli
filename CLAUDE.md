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

This is the central design idea, and most changes touch it. The API has 576 operations; hand-writing 576 commands does not converge, so coverage comes in three tiers that share one binary:

- **Tier 1 — curated** (`fft facility`, `fft connection`, `fft listing`, `fft stock`, `fft sourcing`, `fft order`, `fft routing`, the last with its own sub-nouns `strategy`, `category` and `decision-logs`). The authoritative list is the `groupResource` block in `newRootCmd` (`cmd/fft/root.go`). Hand-written UX: typed flags, validation, readable tables. One file set per noun under `cmd/fft/` (`<noun>.go` + `<noun>_<verb>.go` + `<noun>_test.go`; a sub-noun nests, as in `routing_strategy_update.go`), modelled on the facility set.
- **Tier 2 — generated** (`cmd/fft/generated.go`). A command per remaining operation, auto-registered from the spec at startup, with real flags and a `--file`/`--example`.
- **Tier 3 — escape hatch** (`fft api <operationId>`, `cmd/fft/op.go`). Address any operation by id.

A curated command **shadows** its generated twin: the twin is the operationId the curated command declares in its `Annotations`, and `addGeneratedCommands` skips any claimed operation. So promoting an endpoint from Tier 2 to Tier 1 is a pure upgrade, never a breaking rename. **Every command that reaches the tenant MUST carry `Annotations: {annotationOperationID: "..."}`** — the read-only tree-walk spec (`readonly_test.go`) fails the build otherwise; a command that makes no request is excused in `commandsWithoutOperation`. The full procedure for adding or promoting a curated command (file set, reads/writes, pagination, ids, docs, gates) is the `fft-curated-command` skill.

## Load-bearing invariants

**The output contract.** stdout is data and nothing else; totals, notices, warnings, prompts and the update banner all go to stderr, so a pipe is always safe and an empty stdout means no results. Under `-o json`/`-o yaml`, `Printer.RenderRaw` prints the API's *own bytes* re-indented, never fft's re-encoding of them — the table is fft's summary view, the JSON is the API's document. `Printer.Empty` writes `[]` (right for a list command, wrong for a single-object response — see the sourcing not-routable case, which must still print its document).

**Entities are carried as `entityDoc` (`map[string]any`), not the generated models.** The spec is versionless and moves without notice, so a generated struct knows only the fields of the last `make generate`, and `encoding/json` silently drops the rest. A mutation that read an entity into a struct and PUT it back would *delete* every field the API grew since. (Before oapi-codegen v2.8.0 the models also collapsed this spec's `allOf`-with-siblings schemas outright; that is fixed and no longer the reason.) So reads decode into a hand-written view struct (for the table only), writes go through the generated `...WithBody` variants with raw bytes, and `decodeDoc` uses `json.Decoder.UseNumber()` so 64-bit ids/versions survive. Do not "simplify" a command onto the generated model.

**Optimistic locking lives in the body, not a header.** Mutations read the entity to learn its `version`, send it back, and retry once on conflict (`client.UpdateVersioned` + `versionFlag`). `--if-version` skips the read for a clean 409. Exit 7 is a version conflict.

**A POST is a write unless proven otherwise.** The read-only gate (`cmd/fft/readonly.go`) is keyed on `Operation.Mutates()` (`internal/api/access.go`), which treats every POST as a mutation *except* the hand-curated `readPOSTs` allow-list — the cursor searches, the promise calculators, and `createSourcingOptionsRequest` (`fft sourcing simulate`, which reserves nothing). The census test in `access_test.go` fails the build until a newly-added POST is classified. Fail closed: a wrong "read" is a write fft promised not to make.

**Two pagination models, kept separate on purpose.** `internal/client/search.go` pages the POST `/{entity}/search` endpoints by opaque cursor (default size 20). `internal/client/list.go` pages the plain GET lists by `startAfterId`+`size` (default 25, envelope `{items, total}`, no cursor/hasNextPage). They are not unified — the defaults belong to the API, and `list.go` must cross-check the envelope's `total` because the GET endpoints declare no max `size` and a server that caps it would otherwise look like end-of-list. Both reuse `TruncatedError`/`MaxItems` so `--all` is always bounded and a truncated result always says so on stderr.

**Path params vs query filters resolve ids differently.** A facility path parameter accepts the `urn:fft:facility:tenantFacilityId:<id>` form (`client.FacilityRef`, no lookup). A *query filter* does not — the API answers a URN it cannot resolve with a cheerful empty 200 — so a filter value must be resolved to a platform UUID first (`resolveFacilityID`, one GET). See `fft connection list`'s `--facility` (path) vs `--target` (filter).

**Exit codes are the API** (`internal/exitcode`): 0 ok, 2 usage, 3 config/no-project, 4 auth (401), 5 forbidden (403), 6 not-found (404), 7 conflict (409), 8 partial bulk write, 9 upstream unavailable, 10 read-only refusal (nothing was sent), 130 interrupted. Commands return a typed error; `exitcode.FromError` maps it.

**The API contract is looked up, not guessed.** Before building or changing a command against an endpoint, classifying a POST, or chasing an unexpected 4xx, load the `fulfillment-tools-consultant` skill (`.claude/skills/`): it navigates the ~100k-line spec surgically and carries the semantics already dug out of it.

## Security surfaces

A change that weakens one of these is a finding, whatever else it fixes:

- **No credential on stdout.** The output contract promises stdout is safe to pipe and log; a token there is a leak (a live `FFT_ID_TOKEN` once shipped to stdout in the `fft component init` scaffold). Request/response debug output goes through the redactor in `internal/httplog`.
- **The component credential boundary** (`internal/component/env.go`): the `FFT_` namespace strip (case-insensitive, for Windows), `addSession`, and `apiKeyPlaceholder`, which exists so the long-lived Firebase API key never reaches a component. Read/write sessions hand over a live `FFT_ID_TOKEN`. Credentials at rest go through the keychain in `internal/secrets`.
- **The read-only gate** — see the POST invariant above. Fail closed.
- **Component install trust** (`internal/component/install.go`, `archive.go`, `manifest.go`): pinned to a release, `checksums.txt` verified, unsigned or mismatched archives refused; unpacking goes through an `os.Root` so no entry escapes the install dir; `validExec` rejects absolute or `..` executable paths; `maxArchive`/`maxFile`/`maxFiles` bound resource use. A component runs as the user.
- **Scaffolded scripts** (`internal/component/scaffold_templates.go`): shell/`sed` injection, `FFT_ID_TOKEN`/`FFT_PASSWORD` masking, shebang trust.
- **`pkg/transportproto`** reads untrusted newline-delimited JSON frames under a `MaxFrame` cap — check bounds and id correlation.

The `security-reviewer` skill is generic, and these rules override it here. Scan with the Go tooling — `govulncheck ./...` (CI runs it), plus `gosec ./...` and `gitleaks detect --source=.` where installed — and say which tools were missing rather than skipping silently; the skill's semgrep/bandit/npm/trivy steps do not apply. A security review is static and report-only: no fixes applied, no active testing beyond a local read-only proof of concept, never against a live tenant. The skill's authorization and stakeholder-confirmation steps are satisfied by being asked for the review: do not stop to wait for them, which an unattended routine never could.

## Testing

Ginkgo v2 + Gomega, one suite per package. In `cmd/fft`, the `cli` harness (`main_suite_test.go`) gives each spec a `fakeTenant` that records every request (method, path, query, body) — assert on what went over the wire, on exact stdout tables, and on the exit code. Cross-cutting guard specs you must keep green when adding a resource: `readonly_test.go` (every command annotated), `generated_test.go` (shadowing), `access_test.go` (POST read/write census), `skill_drift_test.go`, and `opcount_drift_test.go` (API counts quoted in prose). The `fft-specs` skill is the harness reference: every helper, what a command spec must prove, and what each guard catches.

Specs are BDD, never bare `testing.T` tables for new code: `Describe` the unit, `Context`/`When` the precondition, `It` the observable behaviour as a sentence (`It("exits 7 when the version is stale")`, not `It("tests the error path")`). Use `DescribeTable`/`Entry` for genuine input permutations, `DeferCleanup` over teardown-by-convention, `Eventually`/`Consistently` instead of `time.Sleep`, and no ordering dependencies between specs. Every bug fix ships a regression spec that fails before the fix and passes after.

The **agent skill** (`internal/skill/assets/`) is documentation compiled into the binary that an AI reads before driving `fft`. `skill_drift_test.go` walks every `fft ...` invocation in the skill and fails the build if it does not resolve against the real command tree — but only skill→tree, so **new commands can go undocumented and stay green**. When adding a Tier-1 noun, update `SKILL.md` (including the frontmatter `description`, the one line an agent reads before opening the skill) and `references/commands.md`.

## Widening coverage

Tier 2 and Tier 3 already reach every operation. To give a tag a *typed generated client*, add it to `output-options.include-tags` in `api/openapi/oapi-codegen.yaml` and run `make generate` (the spec is filtered by tag; unreached schemas are pruned). Two generators run under `make generate`, both writing to `internal/api/`: oapi-codegen → `fft.gen.go` (typed client + models), `tools/specgen` → `opmeta.gen.go` (summaries, permissions, and the synthesized `--example` bodies). Note that specgen cannot synthesize a coherent body for a discriminated `oneOf`; such commands hand-write their `--example` (see `fft stock create`, `fft connection create`).

## Documentation site

`docs/` is a VitePress site published to GitHub Pages (`.github/workflows/docs.yml`), and `make docs` runs the two generators that feed it, both drift-gated by CI's `docs` job exactly like `make generate`. `tools/docsgen` renders the skill assets (`internal/skill/assets/`) into guide pages; the hidden `fft gen-docs` command (`cmd/fft/gendocs.go`) renders the curated command tree into `docs/reference/commands/`. Edit a skill asset, run `make docs`, commit — or the build goes red.

**A guide page's front matter says who owns it.** A `source:` key names the skill asset it was generated from: edit that asset, never the page, and `docsgen` deletes such a page when its source goes away. A page *without* a `source:` key — `install`, `prerequisites`, `getting-started`, `configuration`, `auth`, `ci`, `try-offline`, `read-only`, `agents`, `tui` — is hand-written, edited in place, and `docsgen` never touches it; add a new one to the sidebar in `docs/.vitepress/config.mts` by hand, since nothing generates that list. `docs/index.md`, `docs/reference/index.md` and `docs/.vitepress/` are hand-written too.

**The README is not a source for the site.** It was until the guide pages were inverted; it is now a ~130-line getting-started page that links out, and `CONTRIBUTING.md` holds the contributor material. Deep prose belongs in a guide page, not in the README — and note that nothing drift-gates README examples against the command tree the way `skill_drift_test.go` does for the skill.

## Release

Tag `vX.Y.Z` and push — `.github/workflows/release.yml` runs GoReleaser (cross-platform binaries, SBOMs, cosign signatures) and updates the Homebrew tap at `Joessst-Dev/homebrew-tap` (a cask, `Casks/fft.rb`) and the Scoop bucket at `Joessst-Dev/scoop-bucket` (`bucket/fft.json`, the bucket is shared with other tools). Requires the `HOMEBREW_TAP_TOKEN` and `SCOOP_BUCKET_TOKEN` secrets; prerelease tags skip both pushes. Version is injected via ldflags into `internal/buildinfo`.

## Conventions

Go 1.26. Follow the `golang-*` skills (`.claude/skills/`), loading the ones the change touches: `golang-code-style`, `golang-naming` and `golang-safety` for any Go; `golang-cli` for commands, flags, exit codes and I/O; `golang-security` for anything handling tokens, user input, the filesystem or the network; `golang-troubleshooting` for bugs and races; `golang-continuous-integration` for workflows; `golang-concurrency`/`golang-context` for goroutines, cancellation and the emulators; `golang-dependency-management` for go.mod changes; `golang-modernize` for Go version bumps. Review with the `code-reviewer` and `security-reviewer` skills. Comments in this codebase explain **why**, not what — match that bar (the existing files are the reference); a comment that restates the code is noise. Commit messages and PRs carry no Claude attribution.

**Nothing that identifies a live tenant leaves the machine.** Checking a change against
a real tenant is encouraged — it is the bar for a hand-written `--example` body — but
the tenant's identity is not part of the finding. Base URLs, project ids, usernames,
emails, and the ids of real facilities, connections and orders stay out of commit
messages, PR and issue text, code comments and docs. "A pre-production tenant" carries
exactly the same evidence, and examples use obvious placeholders (`BER-01`,
`8f14e45f-ceea-467a-9575-25a1b5c8b3a1`) for the same reason.

This repository is public, and the removal is never clean: GitHub keeps the pre-edit
body of every issue, PR and comment and serves it through the content-edit history, so
an identifier published by accident cannot be taken back without asking GitHub Support
to purge it. Getting it right the first time is the whole of the control.
