package httplog_test

import (
	"os"
	"regexp"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"

	"github.com/Joessst-Dev/fft-cli/internal/httplog"
)

// specPath is the swagger the typed client and the --example bodies are generated
// from. It is regenerated without notice (see CLAUDE.md), so a new credential
// field can appear in it with no code change — this gate is what turns that into a
// build failure rather than a silent --debug leak.
const specPath = "../../api/openapi/fft.api.swagger.yaml"

// secretName is the high-signal half of a credential field name. It catches
// password/clientSecret/currentPassword/firebaseWebApiKey without the false
// positives a bare "key"/"token" would drag in (carrierKey, measurementUnitKey…),
// so the gate needs no allowlist to stay quiet on the current spec.
var secretName = regexp.MustCompile(`(?i)(password|secret|apikey)`)

var _ = Describe("the redactor against the API spec", func() {
	It("covers every secret-named field the swagger defines", func() {
		data, err := os.ReadFile(specPath)
		Expect(err).NotTo(HaveOccurred())

		var root yaml.Node
		Expect(yaml.Unmarshal(data, &root)).To(Succeed())

		covered := map[string]bool{}
		for _, f := range httplog.SecretJSONFields() {
			covered[strings.ToLower(f)] = true
		}

		var uncovered []string
		for name := range propertyNames(&root) {
			if secretName.MatchString(name) && !covered[strings.ToLower(name)] {
				uncovered = append(uncovered, name)
			}
		}

		// If this fails, the spec grew a credential field. Add it to
		// jsonSecretFields in httplog.go — or, if it is not actually a secret,
		// tighten secretName so the gate stops matching it.
		Expect(uncovered).To(BeEmpty(), "secret-named spec fields the --debug redactor does not cover: %v", uncovered)
	})
})

// propertyNames walks an OpenAPI document and returns the set of names declared
// under a `properties:` mapping — the JSON fields that ride in a body — as opposed
// to schema definition names, which are not values and must not be matched.
func propertyNames(n *yaml.Node) map[string]struct{} {
	names := map[string]struct{}{}
	var walk func(node *yaml.Node, underProperties bool)
	walk = func(node *yaml.Node, underProperties bool) {
		switch node.Kind {
		case yaml.DocumentNode:
			for _, c := range node.Content {
				walk(c, false)
			}
		case yaml.SequenceNode:
			for _, c := range node.Content {
				walk(c, false)
			}
		case yaml.MappingNode:
			// Content is key, value, key, value…
			for i := 0; i+1 < len(node.Content); i += 2 {
				key, val := node.Content[i], node.Content[i+1]
				if underProperties {
					names[key.Value] = struct{}{}
				}
				// A `properties:` value is itself a mapping of field name → schema;
				// its keys are the property names one level down.
				walk(val, key.Value == "properties" && val.Kind == yaml.MappingNode)
			}
		}
	}
	walk(n, false)
	return names
}
