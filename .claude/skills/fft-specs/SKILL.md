---
name: fft-specs
description: How to write and fix specs in fft — Ginkgo v2 + Gomega BDD style, the cmd/fft `cli` harness (newCLI, fakeTenant, run, out/errOut) that asserts on what went over the wire, the exact stdout table, and the exit code, the guard specs that police the command tree, and the rule that every bug fix ships a failing-first regression spec. Use whenever adding or changing a spec, writing a regression test for a bug, debugging a red or flaky suite, or deciding what a new command's specs must prove.
---

# Writing fft specs

## Layout and running

- Ginkgo v2 + Gomega, **one suite per package**: a `<pkg>_suite_test.go` with the single
  `Test<Pkg>(t)` → `RunSpecs`. Never add a second `testing.T` entry point. New code gets no bare
  `testing.T` tables either.
- `make test` runs `go test -race -shuffle=on ./...`. Specs run in random order under the race
  detector, so any hidden dependency between specs fails there.
- To run one package, or one spec:

  ```sh
  go test ./cmd/fft/ -run TestFFT
  go test ./cmd/fft/ -run TestFFT -args -ginkgo.focus="fft facility update"
  ```

## The style rules

- `Describe` names the unit, usually the command line (`Describe("fft facility update")`).
- `Context`/`When` states the precondition (`When("the tenant has facilities of both kinds")`).
- `It` states the observable behaviour as a sentence a user would recognise:
  `It("exits 7 on a stale --if-version, and says which version to send")`, never
  `It("tests the error path")`.
- `DescribeTable`/`Entry` only for genuine input permutations (see "refusing a value the API would
  reject" in `facility_test.go`).
- Clean up with `DeferCleanup`, not teardown by convention. Wait with `Eventually`/`Consistently`,
  never `time.Sleep`. Use `GinkgoT().TempDir()` / `GinkgoT().Setenv` for files and environment.
- Helpers call `GinkgoHelper()` so a failure points at the spec, not at the helper.
- A comment in a spec explains why the assertion matters ("One request, not two: that is the whole
  point of the flag."), not what it does.

## The `cli` harness (`cmd/fft/main_suite_test.go`)

It drives the **real command tree** through the same `execute` funnel as `main`, against fakes:
a temp config file, an in-memory keychain, buffers for stdin/stdout/stderr, an instant retry sleep,
and a static token source. No spec ever talks to Google or a real tenant.

| Helper | Use |
|---|---|
| `newCLI()` | a fresh harness per spec (in `BeforeEach`). It also calls `hermeticEnv()`, which unsets every `FFT_*` variable and points HOME/XDG_* into temp dirs, so the developer's or CI's environment cannot leak in |
| `c.fakeTenant(func(w, r, body))` | an `httptest` tenant that records each **call** (`Method`, `Path`, `Query`, `RawQuery`, `Body`) and points the project at it (headless). This is the default choice |
| `c.fakeAPI(handler)` | the older variant that records `*http.Request` without bodies. Use `fakeTenant` for new specs |
| `api.calls` / `api.recorded()` / `api.only()` | the requests made. `only()` asserts exactly one. Use `recorded()` while requests may still be arriving (TUI runner) |
| `call.json()` | the request body decoded into a `map[string]any` |
| `c.run(args...) int` | runs one command line on a fresh tree and returns the exit code. Config and components are re-read between runs, as in a second process |
| `c.out()` / `c.errOut()` | stdout (the data a pipe sees) and stderr (notices, prompts, warnings) |
| `c.setenv`, `c.headless()` | set a variable for the spec, or synthesize the CI project from `FFT_*` |
| `c.answer(lines...)` | pretend to be a terminal and queue what the user types (confirmations) |
| `fixture(name)`, `writeJSON(w, status, body)`, `tempFile(content)` | `testdata/*.json` bodies, a tenant-style JSON answer, a `--file` to read (in `facility_test.go`) |

`internal/testsupport` holds the cross-suite, per-platform assertions (`ExpectOwnerOnlyFile/Dir`,
`ExpectReadableFile/Dir`, `MakeUnwritableDir`). Use them, and don't hand-roll mode checks that are
meaningless on Windows.

## What a command spec must prove

Assert on what a user or a pipe would see, not on internal structs:

1. **The wire:** method, path (including the URN form of a tenantFacilityId), query (`RawQuery` when
   `a,b` vs `a&a` matters), and body: the version stamped in, the fields preserved, nothing extra.
2. **stdout, exactly:** the table, or the API's own document under `-o json`/`-o yaml`. An empty
   result means empty stdout (or `[]` under `-o json`), with the "No … found." notice on stderr.
3. **stderr:** notices, totals and truncation warnings land here, never in stdout.
4. **The exit code**, as an `exitcode.*` constant (`exitcode.OK`, `exitcode.Conflict`, …), never a
   bare integer.

A worked example from `facility_test.go`:

```go
It("reads the facility for its version, then replaces it with the file", func() {
    api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
        writeJSON(w, http.StatusOK, fixture("facility_managed.json"))
    })
    file := tempFile(`{"type":"MANAGED_FACILITY","name":"Berlin Neu","address":{"city":"Berlin"}}`)

    Expect(c.run("facility", "update", "BER-01", "--file", file)).To(Equal(exitcode.OK))

    Expect(api.calls).To(HaveLen(2))
    put := api.calls[1]
    Expect(put.Method).To(Equal(http.MethodPut))
    Expect(put.Path).To(Equal("/api/facilities/urn:fft:facility:tenantFacilityId:BER-01"))
    // The version came from the read; the user's file never carried one.
    Expect(put.json()).To(HaveKeyWithValue("version", BeNumerically("==", 41)))
})
```

For a new curated command, the minimum is: the happy path on the wire and in the table, `-o json`
printing the API's bytes, the empty case, every validation that exits 2 **without sending
anything**, and for mutations the `--if-version` path and the exit-7 conflict. A read-only project
must refuse a write with exit 10 and no request (see `readonly_test.go`'s `readOnlyProject`).

## Bug fixes ship a regression spec

Write the spec first and watch it fail for the reason the bug report gives. Then fix, then watch it
pass. Name it after the behaviour that was broken, and say in a comment what went wrong if that is
not obvious.

## The guard specs — keep them green, never weaken them

| Spec | Fails when |
|---|---|
| `cmd/fft/readonly_test.go` | a command that can reach the tenant has no `annotationOperationID`, or an entry in `commandsWithoutOperation` outlives its command |
| `cmd/fft/generated_test.go` | a curated command doesn't shadow its generated twin, or an operation in the spec has no command |
| `internal/api/access_test.go` | a POST in the spec is not classified as read or write (`readPOSTs`) |
| `cmd/fft/skill_drift_test.go` | an `fft ...` line in the agent skill (`internal/skill/assets/`) doesn't resolve against the tree |
| `cmd/fft/opcount_drift_test.go` | a number the repo states about the API (operation counts in the README, docs, skill) no longer matches the spec |

When one goes red, the code or the docs are wrong, not the guard. If a guard truly needs to change,
that is a design decision to raise, not something to slip into a fix.

## Flaky or red under `-race -shuffle=on`

Load **`golang-troubleshooting`** (and **`golang-concurrency`** for races). The usual causes here are
leaked environment (`hermeticEnv` covers `FFT_*`/XDG, so check for new variables fft reads), shared
package-level state mutated by a spec, a spec relying on another's side effect, and a `time.Sleep`
standing in for `Eventually`. Rerun with `-ginkgo.seed=<seed from the failing run>` to reproduce
the order.
