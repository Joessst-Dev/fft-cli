package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"
)

// configSavers maps every function in this package that saves the config file to
// the command it belongs to, or to "" with the reason it needs no command marked.
//
// The spec below finds the functions by reading the source, so a new call to
// SaveConfig fails the build until it is written down here — and the command
// written down must then carry annotationExclusive, or the TUI would run it beside
// another writer and lose one of the two updates.
var configSavers = map[string]string{
	"runProjectUse":      "fft project use",
	"persistProject":     "fft project add",
	"runProjectRemove":   "fft project remove",
	"runProjectReadOnly": "fft project read-only",

	// The choke point itself: every other save goes through it.
	"SaveConfig": "",
	// Runs in every command's complete, so no command can be marked for it. Under
	// the TUI the session's own complete has already migrated every key it could
	// before the runner exists; a run that retries a failed one saves the same
	// transformation of the same file as any other run doing so, while the read lock
	// keeps every exclusive writer out.
	"migrateAPIKeys": "",
}

// otherWriters are the commands that rewrite the template directory or the request
// history. They are listed by hand: the stores' Write, Remove and Clear are names
// too common to find reliably in the source, and each has only these writers.
var otherWriters = []string{"fft template save", "fft template remove", "fft history clear"}

// configSaveCallers returns the name of every function in this package, outside
// the specs, that saves the config file.
func configSaveCallers() []string {
	GinkgoHelper()

	entries, err := os.ReadDir(".")
	Expect(err).NotTo(HaveOccurred())

	fset := token.NewFileSet()
	found := make(map[string]bool)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		Expect(err).NotTo(HaveOccurred())

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if savesConfig(n) {
					found[fn.Name.Name] = true
				}
				return true
			})
		}
	}
	return slices.Sorted(maps.Keys(found))
}

// savesConfig reports whether n is a call to SaveConfig, or to the store's own
// Save through a Config field.
func savesConfig(n ast.Node) bool {
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if sel.Sel.Name == "SaveConfig" {
		return true
	}
	inner, ok := sel.X.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "Save" && inner.Sel.Name == "Config"
}

var _ = Describe("commands that rewrite a shared file", func() {
	var root *cobra.Command

	BeforeEach(func() {
		root = newRootCmd(newCLI().deps)
	})

	It("names the command behind every function that saves the config file", func() {
		for _, fn := range configSaveCallers() {
			Expect(configSavers).To(HaveKey(fn),
				"%s saves the config file: add it to configSavers with its command, and annotate that command with annotationExclusive", fn)
		}
	})

	find := func(path string) *cobra.Command {
		GinkgoHelper()
		cmd, _, err := root.Find(strings.Fields(path)[1:])
		Expect(err).NotTo(HaveOccurred())
		Expect(cmd.CommandPath()).To(Equal(path))
		return cmd
	}

	// The value, not only the key: the TUI forgets its session's tokens after a run
	// only when the command names the config file, so a config saver marked with
	// another file's value would run alone and still sign the next run in as
	// whoever the project used to be.
	It("marks every config saver to run alone, as rewriting the config file", func() {
		for fn, path := range configSavers {
			if path == "" {
				continue
			}
			Expect(find(path).Annotations).To(HaveKeyWithValue(annotationExclusive, exclusiveConfig),
				"%s saves the config file", fn)
		}
	})

	It("marks every other writer to run alone", func() {
		for _, path := range otherWriters {
			Expect(find(path).Annotations).To(HaveKey(annotationExclusive), "%s rewrites a shared file", path)
		}
	})
})
