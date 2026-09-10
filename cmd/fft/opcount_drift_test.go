package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"

	"github.com/Joessst-Dev/fft-cli/internal/api"
)

// The size of the API is a selling point, so it is quoted in prose everywhere: the
// README, the docs site, the agent skill, the repo description, and a dozen Go
// comments explaining why a tier exists. The swagger those numbers describe is
// versionless and regenerated without notice, so every one of them goes stale
// silently — which is how the repo came to claim 557 in one place and 559 in
// twenty-five others while the spec had moved on again.
//
// These gates turn that into a build failure. They do not check prose for sense;
// they check that a number the repo states about the API still matches the API.

const repoRoot = "../.."

// specPath is the swagger every count here is derived from.
const specPath = repoRoot + "/api/openapi/fft.api.swagger.yaml"

// codegenPath declares which tags get a typed client, and so decides the split
// between "reachable through a Go method" and "reachable only through opmeta".
const codegenPath = repoRoot + "/api/openapi/oapi-codegen.yaml"

// The repo states three different quantities in the same words — "561
// operations" (the whole API), "466 operations" (the ones with no typed method),
// "278 operations" (the ones taking a body) — so the gate cannot tell them apart
// by phrasing, and trying to would break on the next reworded comment. It checks
// the weaker, sturdier property instead: every operation count the repo states
// must be one the spec still makes true. A number that was true of an older
// swagger stops being in that set, which is exactly the drift to catch.
//
// The same reasoning applies to the spec's path and schema counts — also quoted
// in prose (oapi-codegen.yaml, specgen_test.go) and just as prone to going stale
// on a silent regen, as the 2,725-vs-2,255 schema count transposition did. They
// get their own regexes rather than folding into countRE/ofTheRE: the phrasing
// differs ("NNN paths", "N,NNN schemas" with a thousands comma), so a shared
// pattern would either miss the comma or over-match operation counts.
//
// \d{3,4} rather than \d{3}: at 561 today a 3-digit pattern is enough, but this
// gate exists to survive the count changing, including past 1,000 — \b\d{3}\b
// cannot match inside a longer digit run, so a 4-digit figure would silently
// stop being checked at all.
var (
	countRE   = regexp.MustCompile(`\b(\d{3,4}) operations\b`)
	ofTheRE   = regexp.MustCompile(`\b(\d{3,4}) of the \d{3,4} operations\b`)
	pathsRE   = regexp.MustCompile(`\b(\d{3,4}) paths\b`)
	schemasRE = regexp.MustCompile(`\b(\d{1,3}(?:,\d{3})*) schemas\b`)
)

var _ = Describe("the operation counts the repo documents", func() {
	It("states only numbers the spec still makes true", func() {
		c := countSpec()
		valid := map[string]string{
			strconv.Itoa(c.total):           "the whole API",
			strconv.Itoa(c.total - c.typed): "operations with no typed method",
			strconv.Itoa(c.withBody):        "operations taking a request body",
			strconv.Itoa(c.permissions):     "operations declaring a permission",
			strconv.Itoa(c.paths):           "paths declared by the spec",
			strconv.Itoa(c.schemas):         "component schemas the spec declares",
		}

		var wrong []string
		for _, f := range prose() {
			body, err := os.ReadFile(f) // #nosec G304 -- a repo file this gate walks
			Expect(err).NotTo(HaveOccurred())
			for _, re := range []*regexp.Regexp{countRE, ofTheRE, pathsRE, schemasRE} {
				for _, m := range re.FindAllStringSubmatch(string(body), -1) {
					// Strip the schemas count's thousands comma before the lookup;
					// the other three patterns never match one, so this is a no-op there.
					n := strings.ReplaceAll(m[1], ",", "")
					if _, ok := valid[n]; !ok {
						wrong = append(wrong, strings.TrimPrefix(f, repoRoot+"/")+": "+m[0])
					}
				}
			}
		}

		// If this fails the swagger moved, and these sentences describe an API
		// that no longer exists. Replace each with whichever quantity it meant.
		// The GitHub repo description states the total too, and no test can
		// reach it — update that by hand in the same pass.
		Expect(wrong).To(BeEmpty(),
			"counts the spec no longer makes true: %v\nvalid now: %v", wrong, valid)
	})

	It("agrees with the generated operation table", func() {
		// countSpec parses the swagger; api.Operations() is generated from it by
		// specgen. If these disagree, one of the two is reading it wrongly.
		Expect(countSpec().total).To(Equal(len(api.Operations())))
	})
})

// prose is every file in the repo that can state a count: documentation, the Go
// comments that justify the three-tier design, and the YAML comments in the
// codegen config that state the spec's path and schema counts. Only trees that
// are not ours are skipped. Generated pages are deliberately scanned along with
// their sources — docs/guide holds hand-written pages next to skill-derived
// ones, and a gate that skipped the directory would miss every hand-written page
// in it. The vendored spec itself (fft.api.swagger.yaml) is scanned too — it is
// large, but a plain regex pass over it costs nothing worth special-casing for.
func prose() []string {
	skip := []string{"node_modules", ".vitepress/dist", ".git", ".claude"}

	var out []string
	err := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := filepath.ToSlash(strings.TrimPrefix(path, repoRoot+"/"))
		for _, s := range skip {
			if strings.HasPrefix(rel, s) || strings.Contains(rel, "/"+s+"/") {
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
		}
		if d.IsDir() {
			return nil
		}
		switch filepath.Ext(path) {
		case ".md", ".go", ".mts", ".yaml", ".yml":
			// This gate states counts itself, in its own doc comment.
			if filepath.Base(path) != "opcount_drift_test.go" {
				out = append(out, path)
			}
		}
		return nil
	})
	Expect(err).NotTo(HaveOccurred())
	return out
}

// specCounts are the figures the design comments quote, all read from the one
// spec so they can never disagree with each other.
type specCounts struct{ total, typed, permissions, withBody, paths, schemas int }

func countSpec() specCounts {
	data, err := os.ReadFile(specPath)
	Expect(err).NotTo(HaveOccurred())
	var spec struct {
		Paths map[string]map[string]struct {
			Tags        []string       `yaml:"tags"`
			RequestBody map[string]any `yaml:"requestBody"`
		} `yaml:"paths"`
		Components struct {
			Schemas map[string]any `yaml:"schemas"`
		} `yaml:"components"`
	}
	Expect(yaml.Unmarshal(data, &spec)).To(Succeed())

	cfg, err := os.ReadFile(codegenPath)
	Expect(err).NotTo(HaveOccurred())
	var codegen struct {
		OutputOptions struct {
			IncludeTags []string `yaml:"include-tags"`
		} `yaml:"output-options"`
	}
	Expect(yaml.Unmarshal(cfg, &codegen)).To(Succeed())
	included := map[string]bool{}
	for _, t := range codegen.OutputOptions.IncludeTags {
		included[t] = true
	}

	methods := map[string]bool{"get": true, "put": true, "post": true, "delete": true, "patch": true, "head": true, "options": true, "trace": true}

	var c specCounts
	c.paths = len(spec.Paths)
	c.schemas = len(spec.Components.Schemas)
	for _, item := range spec.Paths {
		for method, op := range item {
			if !methods[strings.ToLower(method)] {
				continue
			}
			c.total++
			for _, t := range op.Tags {
				if included[t] {
					c.typed++
					break
				}
			}
			if op.RequestBody != nil {
				c.withBody++
			}
		}
	}
	for _, op := range api.Operations() {
		if len(op.Permissions) > 0 {
			c.permissions++
		}
	}
	return c
}
