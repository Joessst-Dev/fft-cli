package exitcode_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strconv"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

// declaredCodes is every exit code constant in exitcode.go, by name, read from
// the source so that a code added there cannot be missed here.
func declaredCodes() map[string]int {
	GinkgoHelper()
	file, err := parser.ParseFile(token.NewFileSet(), "exitcode.go", nil, parser.SkipObjectResolution)
	Expect(err).NotTo(HaveOccurred())

	codes := map[string]int{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs := spec.(*ast.ValueSpec)
			Expect(vs.Names).To(HaveLen(1), "one exit code per line")
			Expect(vs.Values).To(HaveLen(1), "exit code %s has no literal value", vs.Names[0])
			lit, ok := vs.Values[0].(*ast.BasicLit)
			Expect(ok && lit.Kind == token.INT).To(BeTrue(),
				"exit code %s is not an integer literal; this spec reads the values from the source", vs.Names[0])
			code, err := strconv.Atoi(lit.Value)
			Expect(err).NotTo(HaveOccurred())
			codes[vs.Names[0].Name] = code
		}
	}
	return codes
}

var _ = Describe("Meaning", func() {
	DescribeTable("describes a code in a few words",
		func(code int, meaning string) {
			Expect(exitcode.Meaning(code)).To(Equal(meaning))
		},
		Entry("ok", exitcode.OK, "success"),
		Entry("auth", exitcode.Auth, "authentication failed"),
		Entry("read-only", exitcode.ReadOnly, "read-only: fft refused a write, nothing was sent"),
		Entry("interrupted", exitcode.Interrupted, "interrupted"),
	)

	It("finds the declared codes", func() {
		codes := declaredCodes()
		Expect(codes).To(HaveKeyWithValue("OK", exitcode.OK))
		Expect(codes).To(HaveKeyWithValue("Interrupted", exitcode.Interrupted))
		Expect(len(codes)).To(BeNumerically(">=", 12))
	})

	It("has a description for every code declared", func() {
		for name, code := range declaredCodes() {
			Expect(exitcode.Meaning(code)).NotTo(Equal("unknown exit code"), "exitcode.%s (%d)", name, code)
		}
	})

	It("has every declared code in the troubleshooting reference's table", func() {
		table, err := os.ReadFile("../skill/assets/references/troubleshooting.md")
		Expect(err).NotTo(HaveOccurred())
		for name, code := range declaredCodes() {
			row := regexp.MustCompile(`(?m)^\| ` + strconv.Itoa(code) + ` \|`)
			Expect(row.Match(table)).To(BeTrue(), "exitcode.%s (%d) is not documented", name, code)
		}
	})

	It("says so for a code fft never exits with", func() {
		Expect(exitcode.Meaning(42)).To(Equal("unknown exit code"))
	})
})
