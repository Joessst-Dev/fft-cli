package output_test

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/output"
)

// printer builds a Printer over two buffers — which is what every command's
// streams are under test — and hands back the two it wrote to.
func printer(format output.Format, colour bool) (*output.Printer, *bytes.Buffer, *bytes.Buffer) {
	var out, errOut bytes.Buffer
	return output.New(&out, &errOut, format, colour), &out, &errOut
}

var sgr = regexp.MustCompile("\x1b\\[[0-9;]*m")

// visible is what a terminal actually draws: the cell without its colour codes.
func visible(s string) string { return sgr.ReplaceAllString(s, "") }

var _ = Describe("ParseFormat", func() {
	DescribeTable("accepting the formats a command may render in",
		func(in string, want output.Format) {
			f, err := output.ParseFormat(in)

			Expect(err).NotTo(HaveOccurred())
			Expect(f).To(Equal(want))
		},
		Entry("table", "table", output.Table),
		Entry("json", "json", output.JSON),
		Entry("yaml", "yaml", output.YAML),
		Entry("in the case a hurried user types", "  JSON ", output.JSON),
	)

	It("names the formats that would have worked", func() {
		_, err := output.ParseFormat("xml")

		Expect(err).To(MatchError(ContainSubstring("table, json, yaml")))
	})
})

var _ = Describe("Printer", func() {
	rows := output.Rows{
		Headers: []string{"NAME", "STATUS"},
		Rows:    [][]string{{"Berlin", "ONLINE"}},
	}

	Describe("the stdout/stderr contract", func() {
		It("keeps notices off stdout, so a pipe into jq is never contaminated", func() {
			p, out, errOut := printer(output.JSON, false)

			p.Notef("Total: %d", 2133)
			p.Warnf("there are more results")
			Expect(p.Render(rows, map[string]string{"name": "Berlin"})).To(Succeed())

			Expect(errOut.String()).To(ContainSubstring("Total: 2133"))
			Expect(errOut.String()).To(ContainSubstring("there are more results"))

			// Everything on stdout, and only that, parses as the data it claims to be.
			var data map[string]string
			Expect(json.Unmarshal(out.Bytes(), &data)).To(Succeed())
			Expect(data).To(HaveKeyWithValue("name", "Berlin"))
		})

		It("marks a warning as one, and leaves a notice unadorned", func() {
			p, _, errOut := printer(output.Table, false)

			p.Notef("Now using project %q.", "prod")
			p.Warnf("no credentials are stored")

			Expect(errOut.String()).To(Equal("Now using project \"prod\".\nWarning: no credentials are stored\n"))
		})
	})

	Describe("Render", func() {
		It("renders a table for a human", func() {
			p, out, _ := printer(output.Table, false)

			Expect(p.Render(rows, nil)).To(Succeed())

			Expect(out.String()).To(Equal("NAME     STATUS\nBerlin   ONLINE\n"))
		})

		It("renders the data, not the table, as JSON", func() {
			p, out, _ := printer(output.JSON, false)

			Expect(p.Render(rows, map[string]int{"version": 41})).To(Succeed())

			Expect(out.String()).To(MatchJSON(`{"version":41}`))
		})

		It("renders the data as YAML", func() {
			p, out, _ := printer(output.YAML, false)

			Expect(p.Render(rows, map[string]int{"version": 41})).To(Succeed())

			Expect(out.String()).To(Equal("version: 41\n"))
		})
	})

	Describe("RenderRaw", func() {
		// This is the half of the contract that a script depends on: -o json is the
		// API's own JSON, not fft's summary of it.
		raw := []byte(`{"id":"abc","version":9007199254740993,"unmodelled":{"deep":[1,2]}}`)

		It("prints every field the API sent, including the ones fft has no model for", func() {
			p, out, _ := printer(output.JSON, false)

			Expect(p.RenderRaw(rows, raw)).To(Succeed())

			var doc map[string]any
			Expect(json.Unmarshal(out.Bytes(), &doc)).To(Succeed())
			Expect(doc).To(HaveKey("unmodelled"))
		})

		It("does not round a number through float64 on its way to the pipe", func() {
			p, out, _ := printer(output.JSON, false)

			Expect(p.RenderRaw(rows, raw)).To(Succeed())

			// A decode into map[string]any would answer 9007199254740992. The bytes are
			// only re-indented, never re-encoded, so the id survives intact.
			Expect(out.String()).To(ContainSubstring("9007199254740993"))
		})

		It("emits a YAML number as a number, not as a quoted string", func() {
			p, out, _ := printer(output.YAML, false)

			Expect(p.RenderRaw(rows, []byte(`{"version":41}`))).To(Succeed())

			Expect(out.String()).To(Equal("version: 41\n"))
		})

		It("still renders the view model as the table", func() {
			p, out, _ := printer(output.Table, false)

			Expect(p.RenderRaw(rows, raw)).To(Succeed())

			Expect(out.String()).To(ContainSubstring("Berlin"))
			Expect(out.String()).NotTo(ContainSubstring("unmodelled"))
		})
	})

	Describe("Empty", func() {
		It("leaves stdout empty and says so on stderr, so a pipe gets nothing", func() {
			p, out, errOut := printer(output.Table, false)

			Expect(p.Empty("facilities")).To(Succeed())

			Expect(out.String()).To(BeEmpty())
			Expect(errOut.String()).To(Equal("No facilities found.\n"))
		})

		It("emits an empty array under -o json, so `jq length` answers 0", func() {
			p, out, errOut := printer(output.JSON, false)

			Expect(p.Empty("facilities")).To(Succeed())

			Expect(strings.TrimSpace(out.String())).To(Equal("[]"))
			Expect(errOut.String()).To(BeEmpty())
		})
	})

	Describe("the table", func() {
		It("prints nothing at all when there are no rows to print", func() {
			p, out, _ := printer(output.Table, false)

			Expect(p.Render(output.Rows{Headers: []string{"NAME"}}, nil)).To(Succeed())

			// A lone header row over an empty result is noise a script has to filter.
			Expect(out.String()).To(BeEmpty())
		})

		It("keeps columns aligned when a cell is coloured", func() {
			// The regression this package exists to prevent. text/tabwriter measures a
			// cell in bytes, counts the ten bytes of a colour escape as ten columns, and
			// shifts every column to the right of it — even with tabwriter.Escape. So the
			// widths here are measured on what a terminal actually draws.
			p, out, _ := printer(output.Table, true)
			style := p.Style()

			Expect(p.Render(output.Rows{
				Headers: []string{"NAME", "STATUS", "CITY"},
				Rows: [][]string{
					{"a", style.Green("ONLINE"), "Berlin"},
					{"bbbbbbbbbb", "OFFLINE", "Hamburg"},
				},
			}, nil)).To(Succeed())

			lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
			Expect(lines).To(HaveLen(3))

			// Every CITY cell starts in the same column once the escapes are stripped.
			at := func(line, cell string) int { return strings.Index(visible(line), cell) }
			Expect(at(lines[0], "CITY")).To(Equal(at(lines[1], "Berlin")))
			Expect(at(lines[1], "Berlin")).To(Equal(at(lines[2], "Hamburg")))
		})

		It("emits no escape codes at all when colour is off", func() {
			p, out, _ := printer(output.Table, false)
			style := p.Style()

			Expect(p.Render(output.Rows{
				Headers: []string{"STATUS"},
				Rows:    [][]string{{style.Green("ONLINE")}},
			}, nil)).To(Succeed())

			// A golden-file spec, and a redirected stdout, both depend on this.
			Expect(out.String()).To(Equal("STATUS\nONLINE\n"))
		})

		It("leaves no trailing whitespace on a line", func() {
			p, out, _ := printer(output.Table, false)

			Expect(p.Render(output.Rows{
				Headers: []string{"NAME", "CITY"},
				Rows:    [][]string{{"a", "Berlin"}},
			}, nil)).To(Succeed())

			for _, line := range strings.Split(strings.TrimRight(out.String(), "\n"), "\n") {
				Expect(line).To(Equal(strings.TrimRight(line, " ")))
			}
		})
	})
})

var _ = Describe("Style", func() {
	It("paints nothing when colour is disabled", func() {
		p, _, _ := printer(output.Table, false)
		style := p.Style()

		Expect(style.Green("ONLINE")).To(Equal("ONLINE"))
		Expect(style.Bold("NAME")).To(Equal("NAME"))
		Expect(style.Faint("-")).To(Equal("-"))
	})

	It("paints when colour is enabled, whatever fatih/color's global thinks", func() {
		// fatih/color decides for itself, package-wide, from os.Stdout. A Printer
		// writing to a buffer must not be at the mercy of that — nor race another
		// spec that flipped it.
		p, _, _ := printer(output.Table, true)
		style := p.Style()

		painted := style.Green("ONLINE")

		Expect(painted).NotTo(Equal("ONLINE"))
		Expect(visible(painted)).To(Equal("ONLINE"))
	})

	It("leaves an empty string alone rather than emitting a bare escape pair", func() {
		p, _, _ := printer(output.Table, true)

		Expect(p.Style().Green("")).To(BeEmpty())
	})
})
