// Command specgen turns the fulfillmenttools OpenAPI spec into opmeta.gen.go: the
// metadata table that lets fft reach every operation the typed client does not have.
//
// oapi-codegen generates a typed client for five tags — 106 methods of the API's
// 557 operations. The other 451 have no Go method to call, so a command that wants
// to reach them has to build the request itself: method, path template, parameters
// and body. That is what this table carries.
//
// It also carries something the spec does not: a **sample request body** for each
// of the 271 operations that take one. The spec has 1,556 field-level `example:`
// values and not a single request-body example, so there was nothing to copy; the
// bodies are synthesized from the schemas (see sample.go).
//
// Run it through `make generate`. CI re-runs it and fails on a diff, so the output
// must be a deterministic function of the spec.
//
//	go run ./tools/specgen -spec api/openapi/fft.api.swagger.yaml -out internal/api/opmeta.gen.go
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "specgen: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("specgen", flag.ContinueOnError)

	var (
		spec = fs.String("spec", "", "path to the OpenAPI spec (required)")
		out  = fs.String("out", "", "path to the Go file to write (required)")
		pkg  = fs.String("package", "api", "package name of the generated file")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *spec == "" || *out == "" {
		fs.Usage()
		return fmt.Errorf("-spec and -out are both required")
	}

	ops, err := load(*spec)
	if err != nil {
		return fmt.Errorf("read %s: %w", *spec, err)
	}

	src, err := render(*pkg, *spec, ops)
	if err != nil {
		return fmt.Errorf("render %s: %w", *out, err)
	}

	// 0644: this is source code, checked in and read by everyone who builds.
	if err := os.WriteFile(*out, src, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", *out, err)
	}

	fmt.Fprintf(os.Stderr, "specgen: wrote %d operations to %s\n", len(ops), *out)
	return nil
}
