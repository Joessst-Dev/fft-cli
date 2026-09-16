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

// confirmers maps every function in this package that decides whether to ask
// before acting — by reading AssumeYes, or by calling a function that does — to the
// command it belongs to, or to "" when it is shared or is not a command's.
//
// The spec below finds the functions by reading the source, so a new confirmation
// fails the build until it is written down here, and the command written down must
// carry annotationConfirms. The TUI adds --yes to a write the user has confirmed
// only when the command carries it: without it, a command that asks would find no
// terminal to ask on and refuse, and with it on every command, the equivalent
// command the UI shows would carry a flag that does nothing.
var confirmers = map[string]string{
	"newComponentInstallCmd":      "fft component install",
	"newComponentRemoveCmd":       "fft component remove",
	"newComponentUpgradeCmd":      "fft component upgrade",
	"newConnectionDeleteCmd":      "fft connection delete",
	"newFacilityDeleteCmd":        "fft facility delete",
	"newHistoryClearCmd":          "fft history clear",
	"newListingDeleteCmd":         "fft listing delete",
	"newListingPurgeCmd":          "fft listing purge",
	"newOrderCancelCmd":           "fft order cancel",
	"newProjectAddCmd":            "fft project add",
	"newProjectReadOnlyCmd":       "fft project read-only",
	"newProjectRemoveCmd":         "fft project remove",
	"newRoutingCategoryDeleteCmd": "fft routing category delete",
	"newSkillInstallCmd":          "fft skill install",
	"newStockActionsCmd":          "fft stock actions",
	"newStockDeleteCmd":           "fft stock delete",
	"newTemplateRemoveCmd":        "fft template remove",
	"newTemplateSaveCmd":          "fft template save",

	// The helpers the commands above ask through, each found again in its caller.
	"confirmDestructive":    "",
	"confirmHistoryClear":   "",
	"confirmInstall":        "",
	"confirmRemoval":        "",
	"confirmSkillOverwrite": "",
	"confirmWritable":       "",
	"runComponentInstall":   "",
	"runComponentRemove":    "",
	"runProjectAdd":         "",
	"runProjectReadOnly":    "",
	"runProjectRemove":      "",
	"runSkillInstall":       "",

	// Where the flag is read into Deps.
	"complete": "",
}

// confirmingFunctions returns the name of every function in this package, outside
// the specs, that reads AssumeYes or calls a function that does.
func confirmingFunctions() []string {
	GinkgoHelper()

	entries, err := os.ReadDir(".")
	Expect(err).NotTo(HaveOccurred())

	fset := token.NewFileSet()
	var funcs []*ast.FuncDecl
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		Expect(err).NotTo(HaveOccurred())
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil {
				funcs = append(funcs, fn)
			}
		}
	}

	found := make(map[string]bool)
	for {
		before := len(found)
		for _, fn := range funcs {
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if readsAssumeYes(n) || callsHelperOf(n, found) {
					found[fn.Name.Name] = true
				}
				return true
			})
		}
		// Callers of callers: a new name may have made another function one.
		if len(found) == before {
			return slices.Sorted(maps.Keys(found))
		}
	}
}

func readsAssumeYes(n ast.Node) bool {
	sel, ok := n.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "AssumeYes"
}

// callsHelperOf reports whether n calls a package-level function named in names
// that asks on its caller's behalf.
//
// A command's constructor is not such a function, and neither is complete: every
// parent command calls its children's constructors, and every command runs
// complete, so following either would mark the whole tree.
func callsHelperOf(n ast.Node, names map[string]bool) bool {
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return false
	}
	ident, ok := call.Fun.(*ast.Ident)
	if !ok || !names[ident.Name] {
		return false
	}
	constructor := strings.HasPrefix(ident.Name, "new") && strings.HasSuffix(ident.Name, "Cmd")
	return !constructor && ident.Name != "complete"
}

var _ = Describe("commands that ask before they act", func() {
	var root *cobra.Command

	BeforeEach(func() {
		root = newRootCmd(newCLI().deps)
	})

	It("names the command behind every function that decides whether to ask", func() {
		for _, fn := range confirmingFunctions() {
			Expect(confirmers).To(HaveKey(fn),
				"%s decides whether to ask: add it to confirmers with its command, and annotate that command with annotationConfirms", fn)
		}
	})

	It("marks every one of them", func() {
		for _, path := range confirmers {
			if path == "" {
				continue
			}
			cmd, _, err := root.Find(strings.Fields(path)[1:])
			Expect(err).NotTo(HaveOccurred())
			Expect(cmd.CommandPath()).To(Equal(path))
			Expect(cmd.Annotations).To(HaveKey(annotationConfirms), "%s asks before it acts", path)
		}
	})

	It("marks no other command", func() {
		marked := slices.Collect(maps.Values(confirmers))
		var walk func(*cobra.Command)
		walk = func(cmd *cobra.Command) {
			if _, ok := cmd.Annotations[annotationConfirms]; ok {
				Expect(marked).To(ContainElement(cmd.CommandPath()),
					"%s is marked as asking, and nothing in it does", cmd.CommandPath())
			}
			for _, child := range cmd.Commands() {
				walk(child)
			}
		}
		walk(root)
	})
})
