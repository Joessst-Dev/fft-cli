---
title: Try it without a tenant
---

# Try it without a tenant

`fft emulator` runs a local server that mimics the API in memory. No project is
configured, no secret is stored, and no request reaches a tenant:

```sh
fft emulator                 # one shell; it prints the recipe below on stderr
```

```sh
export FFT_BASE_URL=http://localhost:8080     # another shell
export FFT_FIREBASE_API_KEY=emulator
export FFT_EMAIL=dev@localhost
export FFT_ID_TOKEN=emulator-token

fft ping
fft facility create --example > facility.json
fft facility create --file facility.json
fft facility list                             # the facility you just created
```

That is the same shape as driving a real tenant (see [Getting started](./getting-started.md)),
with the sign-in removed. State lives in memory and dies with the process. More — seeding
fixtures, the container image, eventing — in the
[emulator guide](https://joessst-dev.github.io/fft-cli/guide/emulator).

---
