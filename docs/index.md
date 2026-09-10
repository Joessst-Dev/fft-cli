---
layout: home

hero:
  name: fft
  text: One CLI for the fulfillmenttools API
  tagline: One binary, one auth path, one output contract — reaching every one of the API's 561 operations from day one.
  actions:
    - theme: brand
      text: Get started
      link: /guide/install
    - theme: alt
      text: Try it without a tenant
      link: /guide/try-offline
    - theme: alt
      text: Command guide
      link: /guide/commands
    - theme: alt
      text: View on GitHub
      link: https://github.com/Joessst-Dev/fft-cli

features:
  - title: Every operation, from day one
    details: Three tiers — curated commands, generated commands for the rest, and an escape hatch by operationId — share one binary. Not the endpoints someone got around to wrapping; all of them.
  - title: Auth that gets out of the way
    details: Sign in once per project, switch between tenants with a flag, and let the CLI obtain and refresh tokens invisibly — no re-authenticating every hour. Secrets live in your OS keychain.
  - title: A pipe is always safe
    details: stdout is data and nothing else — totals, notices and prompts go to stderr. Under -o json you get the API's own bytes, never a re-encoding. Loop it, script it, run it from CI.
  - title: Runs without a tenant
    details: fft emulator is a local, in-memory stand-in for the API — for a demo, a test, or trying a command out. Ships as a container image and as Go and Java Testcontainers modules.
  - title: Built for agents too
    details: Ships an agent skill an AI reads before driving fft, compiled into the binary so it can never describe commands you don't have.
---

## Why this exists

fulfillmenttools ships an [official Postman
collection](https://docs.fulfillmenttools.com/documentation/getting-started/access-to-fulfillmenttools-apis),
and it is good: import it, fill in an environment, and every request in the API is a click
away — Newman will even run a collection from CI. But it stays inside Postman. A response
lands in a result pane or a run report, never on stdout, so there is nothing to pipe into
`jq`, compose into a script, or hand to an agent. And its token is yours to refresh by
hand once an hour.

`fft` is that same API in your shell. Set your projects up once, switch between them with
a flag, and let the CLI obtain and refresh tokens invisibly. stdout is data and nothing
else, so a pipe is always safe.

**And you can drive it without a tenant.** `fft emulator` is a local, in-memory stand-in
for the API — no account, no credentials, no network.

[Get started →](/guide/install) · [Try it without a tenant →](/guide/try-offline)
