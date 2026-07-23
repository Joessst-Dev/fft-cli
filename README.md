# fft

A command-line client for the [fulfillmenttools](https://fulfillmenttools.com) API.

> **Not an official fulfillmenttools product.** This is an independent, open-source
> community project. It is not affiliated with, endorsed by, or supported by
> fulfillmenttools or OC fulfillment tools GmbH. "fulfillmenttools" is the trademark of
> its respective owner and is used here only to describe what this tool talks to.
> For official support, contact fulfillmenttools — not this repository's issue tracker.

Working with the API today means hand-rolling curl requests or maintaining a Postman
collection: mint a bearer token by hand, remember the right host for the right tenant,
and dig a correctly-shaped JSON body out of an 86,000-line swagger file. Switching from
staging to prod to a customer's tenant means doing all of it again.

`fft` replaces that. Set your projects up once, switch between them freely, and let the
CLI obtain and refresh tokens invisibly.

**Every one of the 557 API operations is reachable from day one.** Not "the ones someone
got around to wrapping" — all of them.

📖 **Documentation:** [joessst-dev.github.io/fft-cli](https://joessst-dev.github.io/fft-cli/) —
the browsable, searchable rendering of this README, the command guide, and the full CLI
reference.

---

## Install

**Homebrew** (macOS):

```sh
brew install Joessst-Dev/tap/fft
```

**Go** (any platform, needs Go 1.25+):

```sh
go install github.com/Joessst-Dev/fft-cli/cmd/fft@latest
```

**Binary download** — darwin/linux/windows × amd64/arm64, from the
[releases page](https://github.com/Joessst-Dev/fft-cli/releases):

```sh
curl -sSL https://github.com/Joessst-Dev/fft-cli/releases/latest/download/fft_Linux_x86_64.tar.gz | tar xz
sudo mv fft /usr/local/bin/
```

Archives are checksummed, SBOM'd, and signed with [cosign](https://docs.sigstore.dev/)
keylessly — see [Verifying a download](#verifying-a-download).

Confirm it worked:

```sh
fft version
```

fft tells you when a newer release exists (at most once a day, on stderr, never in your
way). Set `FFT_NO_UPDATE_CHECK=1` to turn that off.

---

## Before you begin

You need four things from your fulfillmenttools onboarding email or your admin. They are
not all obvious, so:

| What | Looks like | Notes |
|---|---|---|
| **Base URL** | `https://ocff-acme-pre.api.fulfillmenttools.com` | The **full host**. Don't try to build it from the project id — the official docs disagree with themselves about the prefix. Copy whatever a working curl call or the onboarding email uses. |
| **fulfillmenttools API key** | `AIzaSy…` | A Firebase Web API key — the value in `?key=` on the Google sign-in URL. **This is not a credential** — see [Authentication](#authentication-honestly). |
| **Username** | `jane.doe` | Your login name, *not* an email address. |
| **Password** | | |

Plus the **project id** (`acme`) and **environment** (`pre` or `prd`) — fft needs them
to derive your sign-in email, which is synthetic:
`{username}@ocff-{projectId}-{env}.com`. If you already know the full email, pass that
instead and the other two become unnecessary.

## Getting started

```sh
fft project add prod     # answer the prompts
fft ping                 # is the tenant reachable?
fft facility list        # you're in
```

That's the whole setup. `fft project add` **authenticates before it saves anything**, so a
typo in the password fails at setup rather than becoming a mystery an hour later — and it
stores the email that actually worked, rather than the one it guessed.

## Setting up a project

A *project* is one tenant plus the credentials to reach it. You can configure as many as
you like and switch between them; commands act on the **active** one unless you pass
`--project`.

### Interactively

```sh
$ fft project add prod
Project name: prod
Base URL (e.g. https://acme.api.fulfillmenttools.com): https://ocff-acme-pre.api.fulfillmenttools.com
fulfillmenttools API key: AIzaSy…
fulfillmenttools project id: acme
Environment (pre or prd): pre
Username (login name): jane.doe
Password:
Project "prod" added and is now active.
```

The password is masked as you type, never passed as a flag, and never written to your
shell history. A value you already gave as a flag is not asked for again, so you can mix
the two freely.

### Non-interactively (scripts, provisioning)

There is deliberately **no `--password` flag** — it would land in your shell history and
in `ps` output. Use `--password-stdin`:

```sh
echo "$PASSWORD" | fft project add prod \
  --base-url   https://ocff-acme-pre.api.fulfillmenttools.com \
  --api-key    "$FIREBASE_WEB_API_KEY" \
  --project-id acme \
  --env        pre \
  --username   jane.doe \
  --password-stdin
```

Pass `--email jane.doe@ocff-acme-pre.com` instead of `--username`/`--project-id`/`--env`
if you already know the full address. `--force` overwrites an existing project of the
same name.

### Working with several projects

```sh
fft project list                    # * marks the active one
fft project use staging             # switch
fft project current                 # which am I on?
fft facility list --project prod    # one command against another project
fft project remove staging          # forgets it, and wipes its keychain entries
```

```
$ fft project list
NAME        BASE URL                                        EMAIL                        CREDENTIAL
* prod      https://ocff-acme-pre.api.fulfillmenttools.com  jane.doe@ocff-acme-pre.com   keyring
  staging   https://ocff-acme-stg.api.fulfillmenttools.com  jane.doe@ocff-acme-stg.com   keyring
```

### Where things are kept

- **Secrets** (password, refresh token, ID token) → your **OS keychain**, one entry each.
- **Everything else** (name, base URL, email, active project) → `~/.config/fft/config.yaml`,
  mode `0600`. Plain YAML; safe to read, edit, and commit to a dotfiles repo — it contains
  no secrets.

No keychain available (headless Linux, a container)? `--no-keyring` / `FFT_NO_KEYRING=1`
falls back to a `0600` file — on Windows that mode buys you less than it looks like, see
[On Windows, `--no-keyring` protects less than `0600` suggests](#on-windows---no-keyring-protects-less-than-0600-suggests).
In CI, skip projects entirely — see [CI and headless use](#ci-and-headless-use).

### Shell completion

```sh
fft completion zsh  > "${fpath[1]}/_fft"          # zsh
fft completion bash > /etc/bash_completion.d/fft  # bash
fft completion fish > ~/.config/fish/completions/fft.fish
```

---

## The three-tier command surface

Hand-writing 557 commands does not converge, so coverage comes in three tiers. They
share one binary, one auth path, and one output contract.

**Tier 1 — curated.** Hand-written UX for the core entities: typed flags, validation,
readable tables.

```sh
fft facility list --status ACTIVE
fft stock get 7f3c…
fft listing list --facility my-warehouse-berlin
fft connection list --facility my-warehouse-berlin
fft sourcing simulate --file order.json
```

**Tier 2 — generated.** Every remaining operation, auto-registered from the spec.
Real flags with the correct array encoding, a `--file` for the body, and a `--help`
carrying the endpoint's summary, its required permission, and a sample body.

```sh
fft picking get-pick-job --pick-job-id abc123
fft handover get-all-handoverjobs
```

**Tier 3 — the escape hatch.** When the spec moves faster than `fft` does, address any
operation by its `operationId`.

```sh
fft api list --tag picking          # what is there?
fft api describe addPickJob         # what does it take?
fft api getPickJob --param pickJobId=abc123
fft api queryPickJobs --query status=OPEN --query size=10
```

A curated command **shadows** its generated twin, so an endpoint being promoted from
Tier 2 to Tier 1 is a pure upgrade — never a breaking rename.

### The universal workflow for any mutating endpoint

The swagger has 1,556 field-level examples and *zero* request-body examples, so `fft`
synthesizes one for every operation that takes a body. `--example` prints it, `--file`
sends it back:

```sh
fft stock create --example > stock.json
$EDITOR stock.json
fft stock create --file stock.json
```

This works identically across all three tiers — `fft api addPickJob --example` too.
It is the answer to "what am I supposed to POST here?"

---

## Letting an AI agent drive

`fft` ships an **agent skill**: the documentation an AI coding assistant reads before it
runs `fft` on your behalf. It covers the command surface, how to discover the API's 557
operations, and the things no `--help` can teach it — that stdout is data and stderr is
everything else, that a POST is not necessarily a write, that exit 8 means *some* of a bulk
write landed, and that it must ask you before it changes anything.

```sh
fft skill install              # ~/.claude/skills/fft — Claude Code, every project
fft skill install --local      # ./.claude/skills/fft — this project, committable
fft skill install --dir DIR    # anywhere else
```

For an assistant that reads a single context file rather than a directory of skills:

```sh
fft skill show >> AGENTS.md
```

The skill needs no project, no credentials and no network, so it is a reasonable first
thing to run on a new machine. Installing twice changes nothing; a file you have edited is
never replaced without `--force`, or without asking.

You can read it without installing anything:
[`internal/skill/assets/SKILL.md`](internal/skill/assets/SKILL.md).

**It cannot quietly go stale.** The skill is compiled into the binary, so it always
describes the commands you actually have — and every `fft` invocation in it is resolved
against the real command tree by a spec. Rename a flag and `fft`'s own build fails, naming
the file and line of the snippet that has started lying.

---

## Authentication, honestly

fulfillmenttools does not authenticate you. **Google Identity Platform (Firebase)
does**, and fulfillmenttools accepts the resulting token. The swagger has no login
endpoint at all, which is why this is worth spelling out:

1. `fft` signs in against `identitytoolkit.googleapis.com` with your username and
   password, and receives an ID token and a refresh token.
2. It sends `Authorization: Bearer <idToken>` to `https://<tenant>.api.fulfillmenttools.com`.
3. When the token nears expiry it refreshes against `securetoken.googleapis.com`,
   transparently. You will not notice.

**The API key is not a credential.** It is the Firebase *Web API key*: it identifies the
Firebase project and confers no authorization whatsoever. It is sent only as `?key=` on
those two Google URLs and is **never sent to fulfillmenttools** — the token source owns a
separate HTTP client with a hardcoded allowlist of the two Google hosts, so the key is
structurally incapable of reaching your tenant.

**Your username is not your email.** fulfillmenttools derives a synthetic one:
`{username}@ocff-{projectId}-{env}.com`. `fft` builds it for you; `project add` asks for
the parts.

Secrets (password, refresh token, ID token) live in the **OS keychain** — Keychain on
macOS, Credential Manager on Windows, Secret Service on Linux. Each gets its own entry.
Non-secret project data lives in `~/.config/fft/config.yaml`, mode `0600`.

On a Linux box with no Secret Service (a headless server, a bare container), pass
`--no-keyring` or set `FFT_NO_KEYRING=1` to fall back to a `0600` file.

### On Windows, `--no-keyring` protects less than `0600` suggests

The default on Windows is the **Credential Manager**, and it is the right choice: the OS holds
each secret per-user, and `config.yaml` never contains a secret. None of what follows applies
to the default path.

`--no-keyring` is different. It writes `%USERPROFILE%\.local\state\fft\credentials.json`, and
that file holds your **password and refresh token in cleartext**, exactly as on Linux. What is
*not* the same is the protection around it:

- Windows has no POSIX mode bits. `fft` asks for mode `0600`, but Go's `os.Chmod` on Windows
  only toggles the read-only attribute and discards the rest. File security on Windows is an
  **ACL**, and `fft` sets no ACL of its own.
- The file is therefore protected by exactly one thing: **the ACL it inherits from its parent
  directory.** Under the default `%USERPROFILE%` that inheritance is sound — a stock Windows
  install grants your profile directory to you, `SYSTEM` and `Administrators`, and to no other
  standard user. There, the file is about as private as `0600` on Linux, where `root` can read
  it anyway.
- The weakness is that the protection is *inherited rather than asserted*. Point
  `XDG_STATE_HOME` (which `fft` honours on Windows too) at a shared directory, a second volume
  with default permissions, a network share, or a redirected/roaming profile, and the file
  inherits **that** ACL — which may let every user on the machine read it. On Linux, `0600`
  would still protect you in all of those places. On Windows, nothing does.

**So:** on Windows, prefer the Credential Manager. If you genuinely need `--no-keyring` — a
Windows CI container, say — leave `XDG_STATE_HOME` unset so the file stays inside your user
profile, and assume anyone with local Administrator can read your tenant password.

The specs that assert `0600` are **skipped** on Windows, with that reason printed in the CI
output, rather than deleted — so this gap stays visible instead of rotting quietly.

## CI and headless use

Set these and `fft` runs entirely from the environment — it touches **neither the config
file nor the keychain**:

| Variable | |
|---|---|
| `FFT_BASE_URL` | `https://<tenant>.api.fulfillmenttools.com` |
| `FFT_FIREBASE_API_KEY` | the Firebase Web API key |
| `FFT_USERNAME` *or* `FFT_EMAIL` | the username, or the full synthetic email |
| `FFT_PASSWORD` | |
| `FFT_PROJECT_ID` | needed to derive the email from `FFT_USERNAME` |
| `FFT_ENV` | likewise |
| `FFT_READ_ONLY` | optional; refuse every request that would change the tenant — see [Read-only projects](#read-only-projects) |

```yaml
- run: fft facility list -o json | jq '.[].name'
  env:
    FFT_BASE_URL: ${{ secrets.FFT_BASE_URL }}
    FFT_FIREBASE_API_KEY: ${{ secrets.FFT_FIREBASE_API_KEY }}
    FFT_USERNAME: ${{ secrets.FFT_USERNAME }}
    FFT_PASSWORD: ${{ secrets.FFT_PASSWORD }}
    FFT_PROJECT_ID: ${{ vars.FFT_PROJECT_ID }}
    FFT_ENV: ${{ vars.FFT_ENV }}
```

Every global flag has an environment variable too: `--output` is `FFT_OUTPUT`,
`--project` is `FFT_PROJECT`, and so on.

## Read-only projects

A project can be marked read-only. `fft` then refuses every request that would change
the tenant — creates, updates, patches, deletes, action endpoints — **before it signs
in**, so a refused write costs no round trip and mints no token. It exits `10`.

```sh
fft project add prod --base-url … --read-only   # protected from the start
fft project read-only prod                      # or protect one you already have
fft project read-only prod --off                # asks before it re-arms writes
```

Three ways to say it, and they only ever tighten:

| | |
|---|---|
| `readOnly: true` in the config | the durable one, per project; shown in `fft project list` |
| `FFT_READ_ONLY=1` | protects whichever project `fft` is about to use — the CI knob |
| `--read-only` on any command | protects that one invocation |

`--read-only=false` cannot loosen a project that is configured read-only, or a set
`FFT_READ_ONLY`. It is refused as a usage error rather than quietly honoured: a
guardrail that a flag can switch off is one a copied-and-pasted command line switches
off.

The one place `--read-only=false` *does* mean something is `fft project add --force`,
where it is how you take the mark off a project you are reconfiguring — and there, like
`fft project read-only --off`, it asks before it re-arms writes. Re-adding a protected
project **without** saying `--read-only=false` leaves the mark on; rotating a password
does not silently disarm prod.

**Reads keep working, and that includes the searches.** The fulfillmenttools API runs
its cursor searches over `POST` — `POST /api/facilities/search` is a read — so being
read-only is *not* the same as refusing to `POST`. Which POST is which cannot be
guessed from the path (only 31 of the 43 read-POSTs end in `/search`) nor from the
status code (41 mutating POSTs answer `200`, not `201`), so `fft` carries an explicit,
hand-curated allowlist of the POSTs that read, and treats every other one as a write.
`postDeliveryPromise` sits in the same family as the pure delivery calculators and
reserves stock, so it is blocked; `evaluateRoutingStrategy` is a dry run, so it is not.
A POST the API grows tomorrow is a write until a human says otherwise, and the build
fails until one does.

## Working offline

`fft emulator` runs a local server that mimics the API in memory — for a demo, a test,
or trying a command out without touching a tenant. It prints an `FFT_*` recipe to export
in another shell, remembers the top-level collections (facilities, listings, stocks,
orders) so creates, versions and pagination work, and synthesizes everything else from
the spec. It holds all state in memory and forgets it on exit.

```sh
fft emulator --port 8080
```

It can also publish domain events to a **local** Google Pub/Sub emulator you run
yourself — never to real Google Cloud. See the
[emulator guide](https://joessst-dev.github.io/fft-cli/guide/emulator) for the stateful
model, seeding fixtures, and driving eventing end to end.

## Output contract

**stdout is data. Nothing else.** Update notices, warnings, prompts and progress all go
to stderr. So this is always safe, and always will be:

```sh
fft facility list -o json | jq '.[] | select(.status == "ACTIVE") | .name'
```

`-o table` (the default) renders hand-written view models. `-o json` prints the raw API
response, unmodified, for full fidelity. `-o yaml` converts it.

## Exit codes

Scripts can branch on these. They are part of the CLI's contract.

| Code | Meaning |
|---|---|
| `0` | success |
| `2` | usage — bad flags or arguments |
| `3` | configuration — no active project, or the config is unusable |
| `4` | authentication failed (401) |
| `5` | forbidden — authenticated, but not permitted (403) |
| `6` | not found (404) |
| `7` | version conflict (409) — you sent a stale `version` |
| `8` | partial success — a bulk operation failed for some items |
| `9` | upstream unavailable — 5xx or timeout |
| `10` | read-only — the write was refused before it was sent |
| `130` | interrupted (SIGINT) |

Exit `8` is the one worth designing for: `listing upsert` and `stock upsert` are bulk
operations whose response is a per-item envelope. Some items can succeed while others
fail, and a script that treats that as success will quietly lose data.

Exit `7` carries a precise message — *"you sent version 41, the current version is 42"* —
because the API returns both on a conflict.

## Verifying a download

Releases are signed keylessly: there is no private key, and the signature is bound to
this repository and the exact workflow that produced it, in Sigstore's public
transparency log.

```sh
cosign verify-blob \
  --certificate checksums.txt.pem \
  --signature   checksums.txt.sig \
  --certificate-identity-regexp 'https://github.com/Joessst-Dev/fft-cli/.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt

sha256sum --check checksums.txt --ignore-missing
```

---

## Contributing

```sh
make build      # build ./fft
make test       # go test -race -shuffle=on ./...
make lint       # go vet + golangci-lint
make generate   # regenerate everything derived from the swagger
```

**Rename a flag and the suite will tell you which snippet of the agent skill you just
broke**, with its file and line. Fix the snippet in `internal/skill/assets/`, not the spec:
the skill is documentation an agent acts on, so it is held to the same standard as the code.

**After a swagger update, run `make generate` and commit the result.** The upstream spec
is versionless and is regenerated without notice, so CI enforces that generated code is
reproducible: if `make generate` produces a diff, the build goes red. That red build is
the signal that the spec moved under you — it is a feature, not a nuisance.

`make generate` runs two generators, both writing into `internal/api/`:

- `oapi-codegen` → `fft.gen.go` — the typed client and models.
- `tools/specgen` → `opmeta.gen.go` — operation metadata: summaries, permissions, and
  the synthesized sample bodies behind `--example`.

**Widening endpoint coverage is a one-line change.** Tier 2 and Tier 3 already reach
every operation; what `api/openapi/oapi-codegen.yaml` controls is which tags get a
*typed generated client*. Add a tag to its `output-options.include-tags` list, run
`make generate`, and commit.

### Releasing

Tag it. `release.yml` does the rest.

```sh
git tag v0.1.0 && git push origin v0.1.0
```

This requires two things to exist:

- The tap repository **`Joessst-Dev/homebrew-tap`** — a separate repo, created by hand.
- A repository secret **`HOMEBREW_TAP_TOKEN`**: a fine-grained PAT with `contents: write`
  on that tap repo. The workflow's built-in `GITHUB_TOKEN` cannot push to another
  repository, which is the whole reason this secret exists.

---

## License

[MIT](LICENSE) © Jost Weyers.

## Disclaimer

This is an independent open-source project, built by the community, for the community.
It is **not affiliated with, endorsed by, or supported by fulfillmenttools** (OC
fulfillment tools GmbH). It is not an official client, and it comes with no warranty —
see the [licence](LICENSE).

"fulfillmenttools" and any related marks belong to their respective owner and are used
here descriptively, to say what this tool connects to. The API surface is derived from
the publicly published OpenAPI specification.

Bugs and feature requests for **fft** belong in this repository's issue tracker. Bugs in
the **fulfillmenttools API or platform** belong with fulfillmenttools.
