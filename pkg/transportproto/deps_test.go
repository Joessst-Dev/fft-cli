package transportproto_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// This package is the one thing in the module an outside program imports, so its
// dependencies are a promise. Go's own internal/ rule does not keep that promise: a
// stray import of github.com/Joessst-Dev/fft-cli/internal/... compiles and passes this
// repo's CI, because the import is inside the same module — and then breaks every
// external consumer, who cannot resolve internal/ at all. Nothing else in the tree
// would catch it, so this does: the non-test build must import the standard library
// and nothing else, which rules out internal/ (and any other module path) transitively.
var _ = Describe("dependencies", func() {
	It("imports only the standard library", func() {
		_, thisFile, _, ok := runtime.Caller(0)
		Expect(ok).To(BeTrue())
		dir := filepath.Dir(thisFile)

		entries, err := os.ReadDir(dir)
		Expect(err).NotTo(HaveOccurred())

		fset := token.NewFileSet()
		checked := 0
		for _, e := range entries {
			name := e.Name()
			// Test files are excused: they legitimately reach for Ginkgo and this file
			// parses the standard library. Only what an external import actually compiles
			// — the package's non-test build — is the promise.
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}

			f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.ImportsOnly)
			Expect(err).NotTo(HaveOccurred())
			checked++

			for _, imp := range f.Imports {
				path, err := strconv.Unquote(imp.Path.Value)
				Expect(err).NotTo(HaveOccurred())

				// A standard-library import path's first segment has no dot; anything with a
				// domain there (github.com/…, golang.org/…) is a module dependency.
				first, _, _ := strings.Cut(path, "/")
				Expect(first).NotTo(ContainSubstring("."),
					"%s imports %q — the public package must depend on the standard library only", name, path)
			}
		}

		// A guard that checked nothing would pass while proving nothing — the very
		// silent green it exists to prevent — so require that it actually read the package.
		Expect(checked).To(BeNumerically(">", 0), "found no non-test .go files to check in %s", dir)
	})
})
