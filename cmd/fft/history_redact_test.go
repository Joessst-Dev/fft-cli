package main

import (
	"regexp"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/Joessst-Dev/fft-cli/internal/history"
)

// pairFlagsKept are the repeatable name=value flags whose values history keeps,
// each with the reason. Every other such flag must have its values withheld.
var pairFlagsKept = map[string]string{
	// Filter values — a status, a date, an id — are what reopening the request
	// needs. A pair whose name is credential- or person-shaped is still withheld.
	"query": "filter values are kept, as the history help says",
}

// pairUsage finds a flag whose usage shows a name=value pair: "as name=value",
// "--set email=a@b.de".
var pairUsage = regexp.MustCompile(`[A-Za-z]=[A-Za-z0-9]`)

// The redaction census, in the idiom of access_test.go's POST census: a new
// repeatable name=value flag fails the build until history either withholds its
// values or says here why it keeps them.
var _ = Describe("the request history's redaction of name=value flags", func() {
	It("withholds the values of every such flag not kept on purpose", func() {
		root := newRootCmd(newCLI().deps)

		found := map[string]bool{}
		var walk func(*cobra.Command)
		walk = func(cmd *cobra.Command) {
			cmd.Flags().VisitAll(func(f *pflag.Flag) {
				switch f.Value.Type() {
				case "stringArray", "stringSlice", "stringToString":
					if pairUsage.MatchString(f.Usage) {
						found[f.Name] = true
					}
				}
			})
			for _, sub := range cmd.Commands() {
				walk(sub)
			}
		}
		walk(root)
		// The census must find the flags it is about, or it passes by finding none.
		Expect(found).To(HaveKey("header"))

		for name := range found {
			if _, kept := pairFlagsKept[name]; kept {
				continue
			}
			redacted := history.Redact([]string{"--" + name + "=plain=hunter2"})
			Expect(redacted).To(Equal([]string{"--" + name + "=plain=" + history.Redacted}),
				"--%s takes name=value pairs: add it to pairFlags in internal/history/redact.go, "+
					"or to pairFlagsKept here with the reason its values may be kept", name)
		}
	})
})
