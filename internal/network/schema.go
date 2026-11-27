package network

import _ "embed"

// ConfigSchema is the JSON Schema of fft-network.yaml, for editors and agents.
// A spec holds it to the decoder: every example and every file `fft network
// infer` writes validates against both.
//
//go:embed config.schema.json
var ConfigSchema []byte
