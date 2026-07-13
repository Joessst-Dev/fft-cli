// Package output renders command results.
//
// # stdout is data, stderr is everything else
//
// One rule governs everything here. Warnings, notices, prompts, progress,
// totals, truncation notices and update banners go to stderr; stdout carries the
// data a command was asked for and nothing else. So `fft facility list -o json |
// jq` receives JSON even when fft also had something to say to the human at the
// keyboard, and a script's stdout never depends on whether a warning fired.
//
// Commands reach these streams through cmd.OutOrStdout() and cmd.ErrOrStderr(),
// never through os.Stdout — which is what lets a spec assert on both.
//
// # Two renderings of the same thing, on purpose
//
// A table is a hand-written view model: the generated models are anyOf/oneOf
// unions with two dozen optional pointers, and printing one is unreadable. The
// view model flattens it into the few columns a human wants.
//
// `-o json` prints the API's own JSON, unaltered ([Printer.RenderRaw]). A script
// that pipes fft into jq needs every field the API sent, including the ones fft's
// view model dropped and the ones the swagger forgot to declare — and there are
// some. Reshaping it through a Go struct would silently discard exactly the
// fields that made the script necessary.
//
// Forcing one shape to serve both is what produces tables full of struct dumps
// and JSON full of padding.
package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"
)

// Format is how a command renders its result.
type Format string

// The supported formats.
const (
	Table Format = "table"
	JSON  Format = "json"
	YAML  Format = "yaml"
)

// Formats lists every valid -o value, for help text and shell completion.
func Formats() []string {
	return []string{string(Table), string(JSON), string(YAML)}
}

// ParseFormat validates a -o value.
func ParseFormat(s string) (Format, error) {
	switch f := Format(strings.ToLower(strings.TrimSpace(s))); f {
	case Table, JSON, YAML:
		return f, nil
	default:
		return "", fmt.Errorf("unknown output format %q: want one of %s", s, strings.Join(Formats(), ", "))
	}
}

// Printer renders to a command's output and error streams.
type Printer struct {
	out    io.Writer
	err    io.Writer
	format Format
	color  bool
}

// New returns a Printer writing data to out and diagnostics to err.
func New(out, err io.Writer, format Format, color bool) *Printer {
	return &Printer{out: out, err: err, format: format, color: color}
}

// Format is the format the printer renders in.
func (p *Printer) Format() Format { return p.format }

// Style paints text, or does not, according to --no-color, NO_COLOR and whether
// anything is watching.
func (p *Printer) Style() Style { return Style{enabled: p.color} }

// Out is the data stream. Anything written here is what a pipe receives.
//
// It exists for the one command whose output is not a rendered entity —
// `fft auth token --raw`, which prints a bare token for a shell to capture.
// Everything else should render, not write.
func (p *Printer) Out() io.Writer { return p.out }

// Notef tells the human something. It goes to stderr, so it is safe to call from
// a command whose stdout is being piped into jq.
func (p *Printer) Notef(format string, args ...any) {
	fmt.Fprintf(p.err, format+"\n", args...)
}

// Warnf reports something that may be wrong but is not fatal — a truncated
// result, a missing credential. Also stderr, and marked so it is not mistaken
// for output.
func (p *Printer) Warnf(format string, args ...any) {
	fmt.Fprintf(p.err, "%s %s\n", p.Style().Yellow("Warning:"), fmt.Sprintf(format, args...))
}

// Render writes data in the printer's format: table from the view model, JSON and
// YAML from data.
//
// Use it when fft owns the shape of the answer — `fft project list`, `fft ping`.
// For an entity that came off the API, use [Printer.RenderRaw]: a script wants
// the API's fields, not fft's summary of them.
func (p *Printer) Render(table Rows, data any) error {
	switch p.format {
	case JSON:
		return p.writeJSON(data)
	case YAML:
		return p.writeYAML(data)
	case Table:
		return p.table(table)
	default:
		return fmt.Errorf("unknown output format %q", p.format)
	}
}

// RenderRaw writes the API's own JSON, byte for byte, under -o json.
//
// raw must be the bytes the API sent (or an array assembled from them). It is
// re-indented and not re-encoded: every field survives, including the ones fft
// has no model for. -o yaml is the same document in YAML, and the table is the
// view model, because a raw entity has no readable table form.
func (p *Printer) RenderRaw(table Rows, raw []byte) error {
	switch p.format {
	case JSON:
		return p.writeRawJSON(raw)
	case YAML:
		return p.writeRawYAML(raw)
	case Table:
		return p.table(table)
	default:
		return fmt.Errorf("unknown output format %q", p.format)
	}
}

// RenderDocument writes an API answer that fft has no view model for — what the
// Tier-2 and Tier-3 commands get back from the 451 operations nobody has curated.
//
// There is no table here, and there deliberately is not going to be one: a table
// needs a hand-written view model, and an operation fft knows nothing about but
// its schema has none. So `-o table` prints JSON, which is the readable form of an
// arbitrary document. `-o yaml` is the same document in YAML.
//
// This is not [Printer.RenderRaw] with a nil view model, because that would render
// an empty table and leave the user thinking the API said nothing.
func (p *Printer) RenderDocument(raw []byte) error {
	if p.format == YAML {
		return p.writeRawYAML(raw)
	}
	return p.writeRawJSON(raw)
}

// Empty reports a result set with nothing in it.
//
// An empty result is not an error, and a lone header row for it is noise a script
// has to filter out. So a table says so on *stderr* and leaves stdout genuinely
// empty, while -o json prints `[]` — which is what `| jq length` needs in order
// to answer 0 rather than to fail.
//
// noun names what there was none of, plural: "facilities", "listings".
func (p *Printer) Empty(noun string) error {
	switch p.format {
	case JSON, YAML:
		return p.writeRawJSON([]byte("[]"))
	case Table:
		p.Notef("No %s found.", noun)
		return nil
	default:
		return fmt.Errorf("unknown output format %q", p.format)
	}
}

func (p *Printer) writeJSON(data any) error {
	enc := json.NewEncoder(p.out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(data); err != nil {
		return fmt.Errorf("render JSON: %w", err)
	}
	return nil
}

func (p *Printer) writeYAML(data any) error {
	enc := yaml.NewEncoder(p.out)
	enc.SetIndent(2)
	if err := enc.Encode(data); err != nil {
		return fmt.Errorf("render YAML: %w", err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("render YAML: %w", err)
	}
	return nil
}

// writeRawJSON pretty-prints without decoding. json.Indent rewrites the
// whitespace and touches nothing else, so a 64-bit id or a high-precision decimal
// reaches jq exactly as the API wrote it — which a round trip through
// map[string]any would not guarantee.
func (p *Printer) writeRawJSON(raw []byte) error {
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return fmt.Errorf("render JSON: %w", err)
	}
	buf.WriteByte('\n')

	if _, err := p.out.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("render JSON: %w", err)
	}
	return nil
}

// writeRawYAML has to decode, because YAML is a different document.
func (p *Printer) writeRawYAML(raw []byte) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()

	var doc any
	if err := dec.Decode(&doc); err != nil {
		return fmt.Errorf("render YAML: %w", err)
	}
	return p.writeYAML(numbers(doc))
}

// numbers turns every json.Number in a decoded document into the Go number it
// stands for.
//
// The decode asks for json.Number because a plain one makes every value a
// float64, and float64 cannot hold a version or an id past 2^53 without quietly
// rounding it. But json.Number is a string type, and yaml.v3 would faithfully
// emit `version: "42"` — a quoted string where the API sent a number. So the
// exactness is preserved through the decode and spent here.
func numbers(v any) any {
	switch v := v.(type) {
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return i
		}
		if f, err := v.Float64(); err == nil {
			return f
		}
		return v.String()

	case map[string]any:
		for key, val := range v {
			v[key] = numbers(val)
		}
		return v

	case []any:
		for i, val := range v {
			v[i] = numbers(val)
		}
		return v

	default:
		return v
	}
}
