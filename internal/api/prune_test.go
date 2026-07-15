package api

// oapi-codegen's prune removes a component schema only when its $ref appears
// nowhere in the whole document — and it walks every component, not just the
// ones a kept operation reaches. A schema that references itself (e.g.
// AuditSearchQuery, via its and/or arrays) therefore keeps its own $ref alive
// forever and survives prune even when no included tag reaches it, dragging its
// filter graph into fft.gen.go. api/openapi/oapi-codegen.yaml lists those roots
// under exclude-schemas to force them out. Nothing else notices when that list
// stops working: `make generate` stays a no-op, lint stays clean, and the leaked
// types compile fine. This suite is that missing signal.

import (
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

const (
	codegenConfigPath = "../../api/openapi/oapi-codegen.yaml"
	specPath          = "../../api/openapi/fft.api.swagger.yaml"
	generatedPath     = "fft.gen.go"
)

func excludedSchemas() []string {
	var cfg struct {
		OutputOptions struct {
			ExcludeSchemas []string `yaml:"exclude-schemas"`
		} `yaml:"output-options"`
	}
	data, err := os.ReadFile(codegenConfigPath)
	Expect(err).NotTo(HaveOccurred())
	Expect(yaml.Unmarshal(data, &cfg)).To(Succeed())
	return cfg.OutputOptions.ExcludeSchemas
}

var _ = Describe("exclude-schemas", func() {
	// Nothing in exclude-schemas should exist as a generated identifier. The
	// names are distinct enough that a substring match also catches the inline
	// enum children a root drags along (AuditedEntityTypeEnumFilter → ...Eq/In/NotEq).
	It("keeps the excluded schemas out of the generated client", func() {
		schemas := excludedSchemas()
		Expect(schemas).NotTo(BeEmpty(), "exclude-schemas is empty; this guard would pass vacuously")

		gen, err := os.ReadFile(generatedPath)
		Expect(err).NotTo(HaveOccurred())
		src := string(gen)

		for _, name := range schemas {
			Expect(src).NotTo(ContainSubstring(name),
				"%q is in exclude-schemas but still appears in %s — the prune workaround has "+
					"stopped working (oapi-codegen upgrade, or the schema was renamed upstream)", name, generatedPath)
		}
	})

	// A stale entry is not harmless: if the upstream spec renames a listed schema,
	// this entry silently no-ops while the *renamed* schema leaks back in unnamed.
	// Turn that into a failure so the rename is noticed and the list re-checked.
	It("names only schemas the spec still defines", func() {
		var spec struct {
			Components struct {
				Schemas map[string]yaml.Node `yaml:"schemas"`
			} `yaml:"components"`
		}
		data, err := os.ReadFile(specPath)
		Expect(err).NotTo(HaveOccurred())
		Expect(yaml.Unmarshal(data, &spec)).To(Succeed())

		for _, name := range excludedSchemas() {
			_, ok := spec.Components.Schemas[name]
			Expect(ok).To(BeTrue(),
				"exclude-schemas names %q, which the spec no longer defines. It was likely renamed "+
					"upstream — find the new self-referential schema now leaking and update the list", name)
		}
	})
})
