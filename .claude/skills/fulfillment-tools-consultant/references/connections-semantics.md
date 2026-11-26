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
- **Do not treat top-level `type` as an input.** The spec's discriminator is `target.type`
  (`CUSTOMER`, `MANAGED_FACILITY`, `SUPPLIER`); read that one. The top-level copy is not
  contractual, and `SUPPLIER` targets are not confirmed to carry it.
- The create and update bodies are discriminated by the target kind as well
  (`InterFacilityConnectionTo*ForCreation`); no top-level `type` is documented for them either.

Tracked in https://github.com/Joessst-Dev/fft-cli/issues/324 (label `spec-drift`). When the field
appears in the upstream spec, `make generate` will show it and this note can go.
