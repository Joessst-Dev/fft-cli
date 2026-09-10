# fft

A command-line client for the [fulfillmenttools](https://fulfillmenttools.com) API.
Sign in once, then reach **every one of the API's 559 operations** from your shell —
not the ones someone got around to wrapping, all of them. stdout is data and nothing
else, so a pipe is always safe.

> **Not an official fulfillmenttools product.** This is an independent, open-source
> community project — not affiliated with, endorsed by, or supported by fulfillmenttools
> (OC fulfillment tools GmbH). For official support, contact fulfillmenttools, not this
> repository's issue tracker. Full [disclaimer](#disclaimer) below.

📖 **[Documentation → joessst-dev.github.io/fft-cli](https://joessst-dev.github.io/fft-cli/)**
— guides, the full CLI reference, and everything this page deliberately leaves out.

## Install

**Homebrew** (macOS):

```sh
brew install Joessst-Dev/tap/fft
```

**Go** (any platform, needs Go 1.26+):

```sh
go install github.com/Joessst-Dev/fft-cli/cmd/fft@latest
```

**Binary download** — darwin/linux/windows × amd64/arm64, from the
[releases page](https://github.com/Joessst-Dev/fft-cli/releases). Archives are
checksummed, SBOM'd and signed with cosign; the
[install guide](https://joessst-dev.github.io/fft-cli/guide/install) has the verification
recipe.

Then:

```sh
fft version
```

## Get started

You need a base URL, a Firebase Web API key, a username and a password from your
fulfillmenttools onboarding — plus your project id and environment, which `fft` uses to
build the synthetic login email. [What each of those looks
like](https://joessst-dev.github.io/fft-cli/guide/prerequisites), if they are not obvious.

```sh
fft project add prod     # answer the prompts
fft ping                 # is the tenant reachable?
fft facility list        # you're in
```

That is the whole setup. `fft project add` **authenticates before it saves anything**, so
a typo in the password fails at setup rather than becoming a mystery an hour later. Add as
many projects as you like and switch with `fft project use`, or run one command elsewhere
with `--project`. Secrets go to your OS keychain, never to the config file.

## Try it without a tenant

`fft emulator` is a local, in-memory stand-in for the API — no account, no credentials, no
network:

```sh
fft emulator                                    # one shell; prints the recipe on stderr
```

```sh
export FFT_BASE_URL=http://localhost:8080       # another shell
export FFT_FIREBASE_API_KEY=emulator
export FFT_EMAIL=dev@localhost
export FFT_ID_TOKEN=emulator-token

fft ping
fft facility create --example > facility.json
fft facility create --file facility.json
fft facility list                               # the facility you just created
```

Same shape as driving a real tenant, with the sign-in removed. State dies with the
process. Seeding, the container image, eventing and the Testcontainers modules are in the
[emulator guide](https://joessst-dev.github.io/fft-cli/guide/emulator).

## What else it does

- **[Reaches every operation](https://joessst-dev.github.io/fft-cli/guide/discovery)** —
  curated commands for the core entities, a generated command for everything else, and
  `fft api <operationId>` when the spec moves faster than `fft` does.
- **[Answers "what am I supposed to
  POST here?"](https://joessst-dev.github.io/fft-cli/guide/recipes)** — `--example` prints
  a synthesized request body for any operation, `--file` sends it back.
- **[Saves the bodies you send
  often](https://joessst-dev.github.io/fft-cli/guide/templates)** — `fft template` fills
  in the parts that change.
- **[Refuses to write when you say
  so](https://joessst-dev.github.io/fft-cli/guide/read-only)** — a read-only project
  blocks every mutation before it signs in, and knows that a `/search` POST is a read.
- **[Runs in CI from the
  environment](https://joessst-dev.github.io/fft-cli/guide/ci)** — no config file, no
  keychain, and [exit codes](https://joessst-dev.github.io/fft-cli/guide/troubleshooting)
  a script can branch on.
- **[Teaches an AI agent to drive
  it](https://joessst-dev.github.io/fft-cli/guide/agents)** — `fft skill install` ships
  the documentation an assistant reads first, compiled into the binary so it can never
  describe commands you do not have.
- **[Extends from outside the
  binary](https://joessst-dev.github.io/fft-cli/guide/components)** — components add
  commands, and transports the emulator can deliver events to.

## Contributing

```sh
make build && make test && make lint
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for the generated code, the docs site, and how a
release is cut.

## License

[MIT](LICENSE) © Joessst-Dev.

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
