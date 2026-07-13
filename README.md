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
| **Firebase Web API key** | `AIzaSy…` | The value in `?key=` on the Google sign-in URL. **This is not a credential** — see [Authentication](#authentication-honestly). |
| **Username** | `jane.doe` | Your login name, *not* an email address. |
| **Password** | | |

Plus the **project id** (`acme`) and **environment** (`pre`, `prod`, …) — fft needs them
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
Firebase Web API key: AIzaSy…
fulfillmenttools project id: acme
Environment (e.g. staging, prod): pre
Username or full email address: jane.doe
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
falls back to a `0600` file. In CI, skip projects entirely — see
[CI and headless use](#ci-and-headless-use).

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
