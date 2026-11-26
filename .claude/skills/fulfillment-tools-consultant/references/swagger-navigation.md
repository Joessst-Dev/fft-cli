# Navigating `api/openapi/fft.api.swagger.yaml`

The spec is vendored from upstream under a renamed file, regenerated without notice, and over
100,000 lines long. `info.version` is `VERSIONLESS`, so nothing tells you which revision you hold
except the bytes. Line numbers move on every re-vendor: grep, then `Read` with `offset`/`limit`.

## Recipes

| To find | Run |
|---|---|
| Every path | `grep -n "^  /" api/openapi/fft.api.swagger.yaml` |
| One area's paths | `grep -n "^  /api/pickjobs" api/openapi/fft.api.swagger.yaml` |
| An operation by id | `grep -n "operationId: getPickJob$" api/openapi/fft.api.swagger.yaml` |
| A schema | `grep -n "^    PickJob:" api/openapi/fft.api.swagger.yaml` (4-space indent under `components.schemas`) |
| Deprecations in a block | `sed -n '<start>,<end>p' … \| grep -n "deprecated"` |

Schemas are alphabetised, so related types cluster: all the `PickJob*ActionParameter` schemas sit
together, as do `Stock*` and `Listing*`. The tag list near the end of the file maps the bounded
contexts (DOMS / Operations / Inventory / Core) to endpoint groups — orient there before grepping.

## Global facts

- **Server:** `https://{tenant}.api.fulfillmenttools.io`.
- **Auth:** a single security scheme, `BearerToken` — HTTP bearer, JWT.
- **`ApiError` is an array** of `ErrorInner`, not an object. Error rendering must unmarshal a JSON
  array.
- **`x-fft-permissions`** on most operations names the token permissions it needs (for example
  `PICKJOB_READ`, `PICKJOB_WRITE`). Useful for documenting scopes, and strong evidence when
  classifying a POST as read or write.
- **Facility path parameters** accept `urn:fft:facility:tenantFacilityId:<id>`. Query *filters* do
  not resolve that form — see CLAUDE.md, "Path params vs query filters".
- **The generated client covers only some tags.** `api/openapi/oapi-codegen.yaml` filters generation by
  `output-options.include-tags`; every other operation exists only as metadata in
  `internal/api/opmeta.gen.go`.
