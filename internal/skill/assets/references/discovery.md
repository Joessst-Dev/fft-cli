# Finding the operation you need

557 operations. You are not expected to know them, and you must not guess them. Everything
here reads the spec compiled into the binary: it works offline, needs no project, sends no
request, and is free.

## From a word the user said, to a command line

```sh
fft api list --search pickjob
```

`--search` matches the operationId, the path and the summary. `--tag` matches the API's own
grouping, case-insensitively and by substring, so `--tag picking` finds
`Picking (Operations)`:

```sh
fft api list --tag picking
```

The JSON form carries a `command` field: the fft command that runs that operation. This is
the fastest path from a noun to something you can execute.

```sh
fft api list --search handover -o json
```

## Then ask what it wants

```sh
fft api describe getPickJob
```

That prints the parameters, the permission the caller needs, and — for anything with a
body — a complete sample body, synthesized from the schema. Every required field is there.

## The generated commands

Every operation that no curated command claims gets its own command, named from its tag and
its operationId:

- tag `Picking (Operations)` → group `picking`
- operationId `getPickJob` → `fft picking get-pick-job`

```sh
fft picking get-pick-job --pick-job-id abc123
fft picking get-pick-job --help
```

Parameters become real, typed flags. The body comes from `--file`, or inline with `--data`,
and `--example` prints a sample of it:

```sh
fft picking add-pick-job --example > pickjob.json
fft picking add-pick-job --file pickjob.json
```

## The escape hatch

When you have an operationId and want no ceremony:

```sh
fft api getPickJob --param pickJobId=abc123
fft api queryPickJobs --query status=OPEN --query size=10
fft api addPickJob --data @pickjob.json
```

- `--param` fills a path parameter, `--query` a query parameter (repeat it, or use
  `name=a,b` for a list), `--header` a header.
- `--data` takes inline JSON, `@file`, or `-` for stdin. `--file` takes a path, or `-`.
- `--example` works here too.

Use it when the curated or generated command does not exist, or when you already know the
operationId. Prefer the curated command when there is one: it validates, and it renders a
table a human can read.

## A warning about POST

Most of the API's *searches* are POSTs — `fft facility list` sends
`POST /api/facilities/search`. So "it is a POST" tells you nothing about whether it writes.
fft knows the difference (that is what makes `--read-only` work on a list command), but if
you are reasoning about the API yourself, do not infer intent from the verb. `fft api
describe` will tell you what the operation does.

The clearest case is `fft sourcing simulate`. It POSTs a whole order and gets back the ways
that order could be fulfilled — and it reserves nothing, creates nothing and moves nothing.
It is a read, fft classifies it as one, and it runs happily on a read-only project. Refusing
to run it because "POST means write" would be declining to answer a question that was never
dangerous.
