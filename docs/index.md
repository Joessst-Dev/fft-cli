---
layout: home

hero:
  name: fft
  text: One CLI for the fulfillmenttools API
  tagline: One binary, one auth path, one output contract — reaching every one of the API's 559 operations from day one.
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
