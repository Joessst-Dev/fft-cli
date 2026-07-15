# Routine: spec-drift-detect

Source of truth for the **spec-drift detector** cloud routine (registered at
https://claude.ai/code/routines). Schedule: daily, `0 6 * * *` UTC. Environment: Default.
Edit this file and re-register the routine when the prompt changes.

Companion routine: [`spec-drift-implement`](./spec-drift-implement.md) picks up the issue this one files.

---

## Prompt

You watch the fulfillmenttools OpenAPI spec for drift against this repo's vendored copy and, on drift, file a
detailed GitHub issue an implementer can act on. You start with zero context; everything you need is below.

Repo: `Joessst-Dev/fft-cli` — `fft`, a Go CLI (Go 1.25, Cobra, Ginkgo/Gomega, golangci-lint) generated from a
**vendored, versionless** copy of the fulfillmenttools OpenAPI spec at `api/openapi/fft.api.swagger.yaml`.
Upstream publishes the canonical spec at
`https://raw.githubusercontent.com/fulfillmenttools/fulfillmenttools-api-reference/master/api.swagger.yaml`
(the repo vendors it renamed). Use the `gh` CLI for GitHub operations.

**Run entirely in your throwaway checkout. Never commit, push, or open a PR. Your only side effect is
creating or updating one GitHub issue.**

This repo ships project agents (`.claude/agents/`) and skills (`.claude/skills/`) in your checkout — use them
via the Task and Skill tools. In particular, when you suggest a read-vs-write classification for a new POST
(step 7), consult the **`fulfillment-tools-consultant`** agent to check the endpoint's real semantics against
the fulfillmenttools API reference instead of guessing from its name or shape.

1. Ensure you're on the latest `main`.
2. Download upstream:
   `curl -fsSL https://raw.githubusercontent.com/fulfillmenttools/fulfillmenttools-api-reference/master/api.swagger.yaml -o /tmp/upstream.yaml`.
3. Compare it to `api/openapi/fft.api.swagger.yaml`. If they are byte-identical (compare hashes, e.g.
   `sha256sum`), there is **no drift** — do nothing and stop. Do NOT open or comment on any issue.
4. On any difference, produce an accurate, low-noise change report by dry-running the real pipeline in your
   scratch tree: copy `/tmp/upstream.yaml` over `api/openapi/fft.api.swagger.yaml`, run `make generate`, then
   run `make test` and `make lint`. Capture, for the report:
   - operation-level diff: added / removed / changed `operationId`s (parse the `paths` map in both specs);
   - **new and removed POSTs** specifically — these are the census-critical ones;
   - whether any of the six typed `include-tags` in `api/openapi/oapi-codegen.yaml` changed (those touch the
     typed client `internal/api/fft.gen.go`; other ops are metadata-only in `internal/api/opmeta.gen.go`);
   - `git diff --stat` of the regenerated `internal/api/*.gen.go`;
   - the exact guard-test failures — they name precisely what an implementer must edit (the
     `readPOSTs`/`knownMutatingPOSTs` classification in `internal/api/access.go` + `access_test.go`, and the
     pinned POST total in `access_test.go`).
5. Ensure the label exists:
   `gh label create spec-drift --color BFD4F2 --description "Upstream OpenAPI spec drift" || true`.
6. Dedup: if an **open** issue labelled `spec-drift` already exists, update its body and/or add a comment with
   the current report instead of opening a duplicate. Otherwise open a new issue labelled `spec-drift`.
7. Issue title: `spec-drift: upstream OpenAPI changed (<N> ops added, <M> removed, <K> changed)`.
   The body must be a checklist an implementer can follow:
   - one-paragraph summary of what moved;
   - the added / removed / changed operation lists;
   - a **"New POSTs to classify"** section listing each new POST's operationId, with a *suggested*
     read-vs-write call (informed by the `fulfillment-tools-consultant` agent, per above) and the rule:
     "a POST is a write unless it's a pure search / promise-calculator / simulate — classify, don't guess";
   - the census-count delta for the pinned total in `access_test.go`;
   - which `.gen.go` files changed and whether a typed tag was affected;
   - a **"Needs human (Tier-1)"** note if any new noun looks like it deserves hand-written curated UX.
8. Leave your working tree clean; discard the scratch changes.

**Guardrails:** hash-compare first so the common no-drift case exits without running the toolchain. One open
`spec-drift` issue at a time. No writes to the repo of any kind.
