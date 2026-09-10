# Contributing to fft

```sh
make build      # build ./fft
make test       # go test -race -shuffle=on ./...
make lint       # go vet + golangci-lint — must be 0 issues
make fmt        # gofmt -s -w .
make generate   # regenerate everything derived from the swagger
make docs       # regenerate the documentation-site sources
```

## Generated code must be reproducible

**After a swagger update, run `make generate` and commit the result.** The upstream spec
is versionless and is regenerated without notice, so CI enforces that generated code is
reproducible: if `make generate` produces a diff, the build goes red. That red build is
the signal that the spec moved under you — it is a feature, not a nuisance.

`make generate` runs two generators, both writing into `internal/api/`:

- `oapi-codegen` → `fft.gen.go` — the typed client and models.
- `tools/specgen` → `opmeta.gen.go` — operation metadata: summaries, permissions, and
  the synthesized sample bodies behind `--example`.

**Widening endpoint coverage is a one-line change.** The generated commands and
`fft api <operationId>` already reach every operation; what
`api/openapi/oapi-codegen.yaml` controls is which tags get a *typed generated client*.
Add a tag to its `output-options.include-tags` list, run `make generate`, and commit.

## The agent skill is held to the same bar as the code

**Rename a flag and the suite will tell you which snippet of the agent skill you just
broke**, with its file and line. Fix the snippet in `internal/skill/assets/`, not the
spec: the skill is documentation an agent acts on.

Note that the drift spec only walks skill→command tree. A *new* command can go
undocumented and stay green, so when you add a curated noun, update `SKILL.md` (including
the frontmatter `description`) and `references/commands.md` yourself.

## The documentation site

`docs/` is a VitePress site published to GitHub Pages. Its guide pages come from two
places, and the front matter says which:

- **Generated.** A page whose front matter carries a `source:` key is rendered by
  `tools/docsgen` from that skill asset. Edit the asset and run `make docs`; editing the
  page directly is undone on the next run. `docsgen` also deletes a generated page whose
  source has gone away.
- **Hand-written.** A page with no `source:` key — install, the setup and auth guides,
  read-only projects, AI agents — is edited in place and `docsgen` never touches it. Add
  a new one to the sidebar in `docs/.vitepress/config.mts`; nothing generates that list.

`docs/reference/commands/` is rendered from the real command tree by the hidden
`fft gen-docs` command, so a new curated command gets its reference page from `make docs`.

CI's `docs no drift` job re-runs `make docs` and fails on any diff or uncommitted page,
exactly as it does for `make generate`. The README is **not** a source for the site: keep
it short and let it link out.

## Conventions

Go 1.26. Comments explain **why**, not what — a comment that restates the code is noise;
the existing files are the reference. Commit messages and PRs carry no AI attribution.

**Nothing that identifies a live tenant leaves the machine.** Checking a change against a
real tenant is encouraged — it is the bar for a hand-written `--example` body — but the
tenant's identity is not part of the finding. Base URLs, project ids, usernames, emails,
and the ids of real facilities, connections and orders stay out of commit messages, PR and
issue text, code comments and docs. "A pre-production tenant" carries exactly the same
evidence, and examples use obvious placeholders (`BER-01`,
`8f14e45f-ceea-467a-9575-25a1b5c8b3a1`) for the same reason.

This repository is public, and the removal is never clean: GitHub keeps the pre-edit body
of every issue, PR and comment and serves it through the content-edit history, so an
identifier published by accident cannot be taken back without asking GitHub Support to
purge it. Getting it right the first time is the whole of the control.

## Releasing

Tag it. `release.yml` does the rest.

```sh
git tag v0.1.0 && git push origin v0.1.0
```

fft is published to three package managers, and each one is a repository the workflow
does not own. The workflow's built-in `GITHUB_TOKEN` is scoped to this repository only,
which is the whole reason the secrets below exist.

Five things must exist before a tag:

- The tap repository **`Joessst-Dev/homebrew-tap`** — a separate repo, created by hand.
- A repository secret **`HOMEBREW_TAP_TOKEN`**: a fine-grained PAT with `contents: write`
  on that tap repo.
- The Scoop bucket **`Joessst-Dev/scoop-bucket`** — public, and **with an initial commit
  on `main`**. That last part matters: GoReleaser writes the manifest through the GitHub
  contents API, and a repository with zero commits has no default branch for that API to
  write against. A README is the cheapest initial commit.
- A repository secret **`SCOOP_BUCKET_TOKEN`**: a fine-grained PAT with `contents: write`
  on the bucket repo, and on nothing else. Deliberately not a widened
  `HOMEBREW_TAP_TOKEN` — one repo per token, so a leak costs one publishing surface.
- A fork of **`microsoft/winget-pkgs`** owned by a **dedicated bot account**, plus a
  repository secret **`WINGET_PKGS_TOKEN`** holding a *classic* PAT with `public_repo`
  minted on that account.

  The broad scope is unavoidable and worth understanding before you rotate it. Opening a
  pull request whose base is `microsoft/winget-pkgs` requires `pull_requests: write` on a
  repository we do not own; a fine-grained PAT is scoped by explicit repository selection
  within your own account, so it cannot be granted that, and cross-fork PR creation with
  one fails. `public_repo` is write access to every public repo the token's account can
  push to — which is why it belongs to an account that is a collaborator nowhere else and
  holds nothing. Give it a short expiry and calendar the rotation.

  If that tradeoff ever stops being acceptable, set `pull_request.enabled: false` in the
  `winget` block: GoReleaser then only pushes the branch to the fork, which a narrow
  fine-grained PAT can do, and you open the PR by hand once per release.

`skip_upload: auto` means a prerelease tag exercises the build but **skips** all three
publishers, so a `-rc` rehearsal proves the config and proves that nothing was sent
upstream — but it does not prove the tokens. Verify those by hand before the first real
tag.

The WinGet half of a release is not done when the workflow is: the first submission to
`microsoft/winget-pkgs` is reviewed by a human and can take days, and a manifest the
upstream validation rejects is fixed by editing that PR — it never requires re-tagging
fft. Homebrew and Scoop are live the moment the job goes green.
