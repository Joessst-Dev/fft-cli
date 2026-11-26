# Interfacility connections — semantics

## The list response carries an undeclared top-level `type`

`GET /api/facilities/{facilityId}/connections` returns each connection with a top-level `type`
field (observed values `MANAGED_FACILITY` and `CUSTOMER`) that is a copy of `target.type`.
The vendored spec does not declare it: the `InterFacilityConnection` schema lists its properties
without a `type`, and sets `additionalProperties: false`. Re-verify with
`grep -n "^    InterFacilityConnection:" api/openapi/fft.api.swagger.yaml` and read the block.

Consequences:

- **fft is unaffected.** Connections are carried as `entityDoc` and written back as raw bytes, so the
  extra field survives a read-modify-write. Do not "fix" this by moving a command onto the
  generated model, which would drop it.
- **Anything that validates responses against the spec fails every connection** on that field.
  Treat it as upstream drift, not as a malformed response.
- **On reads, `target.type` is the discriminator** (`CUSTOMER`, `MANAGED_FACILITY`,
  `SUPPLIER`). The top-level copy in list responses is not contractual, and `SUPPLIER`
  targets are not confirmed to carry it.
- **On writes, top-level `type` is declared and required.** Every
  `InterFacilityConnectionTo*ForCreation` / `...ForUpdate` schema, and the
  `InterFacilityConnectionForCreation` / `ForUpdate` umbrella schemas, lists it in `required`.
  Do not strip it from a create or update body: that is a 400. fft's `normaliseConnectionType`
  (`cmd/fft/connection.go`) fills it in from `target.type` when a body taken from a read lacks
  it, and refuses a body where the two disagree; do not "simplify" it away on the strength of
  this note.
- **Reconciling with the code comments.** `connection_update.go`'s help text and the comment in
  `cmd/fft/connection.go` say the read model carries `type` only inside `target`. The list
  response above partly contradicts that, but the fill-in is still needed whenever a read
  lacks the field (a `SUPPLIER` target, or a single-connection GET where it has not been
  observed).

Tracked in https://github.com/Joessst-Dev/fft-cli/issues/324 (label `spec-drift`). When the field
appears in the upstream spec, `make generate` will show it and this note can go.
