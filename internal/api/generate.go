// Package api contains the generated fulfillmenttools HTTP client and models, and
// the operation metadata that reaches the operations the client does not have.
//
// Everything in this package with a .gen.go suffix is generated — do not edit it by
// hand. Two generators write here, and they cover different things:
//
//   - fft.gen.go is oapi-codegen's typed client. It is filtered to five tags (see
//     api/openapi/oapi-codegen.yaml), which is 106 methods of the API's 557
//     operations.
//   - opmeta.gen.go is tools/specgen's metadata table, and it covers all 557 —
//     method, path, parameters (with the per-parameter explode that decides what a
//     filter means), permissions, and a synthesized sample request body. It is what
//     `fft api` and the generated commands build their requests from.
//
// The hand-written wrapper the commands actually use lives in internal/client; the
// hand-written half of the metadata (the types, the lookups) is in opmeta.go.
package api

//go:generate go tool oapi-codegen --config=../../api/openapi/oapi-codegen.yaml ../../api/openapi/fft.api.swagger.yaml
//go:generate go run ../../tools/specgen -spec ../../api/openapi/fft.api.swagger.yaml -out opmeta.gen.go
