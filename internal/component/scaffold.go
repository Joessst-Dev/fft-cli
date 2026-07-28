package component

import (
	"fmt"
	"os"
	"strings"
)

// Scaffold describes a component to stamp out. [Scaffold.Build] renders the files —
// it does not write them; the caller decides where they land.
//
// It is the single place the emitted manifest is kept correct: everything Build
// produces is built from a [Manifest], marshalled, and round-tripped through
// [ParseManifest] before it is returned, so a scaffold that would not install cannot
// be produced in the first place.
type Scaffold struct {
	// Name is the component's name, which is also its directory and — for a command
	// component — the command it adds. It is checked as part of the manifest round-trip.
	Name string

	// Kind is what the component extends: [KindCommand] or [KindTransport].
	Kind Kind

	// Lang is the language the executable is written in: "shell", "go", "python" or
	// "node". A "go" skeleton compiles to bin/<exec>; the others are interpreter
	// scripts that run the moment they are installed.
	Lang string

	// Session is how much of the caller's session the command receives. Command kind
	// only; ignored for a transport, which declares no commands.
	Session Session

	// FFTRequire pins github.com/Joessst-Dev/fft-cli in a Go transport's go.mod. Empty
	// leaves it out, so the README's `go mod tidy` step resolves it — which is what a
	// dev build of fft, whose version is no release tag, has to fall back to.
	FFTRequire string
}

// Supported languages a scaffold can be written in.
const (
	LangShell  = "shell"
	LangGo     = "go"
	LangPython = "python"
	LangNode   = "node"
)

// supportedLangs is the canonical set of languages skeleton() renders. It backs the
// "unknown language" error and the scaffold drift test, so a new language cannot be
// added to one without the other noticing.
var supportedLangs = []string{LangShell, LangGo, LangPython, LangNode}

// placeholderTarget is the target type a transport scaffold claims until its author
// replaces it. It is deliberately not a real one: a scaffold that delivered to a
// broker somebody actually runs would be a surprising thing to install unfinished.
const placeholderTarget = "EXAMPLE_TARGET"

// DefaultLang is the language a kind is scaffolded in when none is named: a shell
// script for a command (runs immediately, no toolchain), and Go for a transport
// (which is what most transport authors want, and the only language with the protocol
// as a library rather than a wire format to reimplement).
func DefaultLang(kind Kind) string {
	if kind == KindTransport {
		return LangGo
	}
	return LangShell
}

// File is one file a scaffold emits, with the mode it should be written under.
type File struct {
	// Name is the path relative to the component's own directory, slash-separated.
	Name string

	// Data is the file's contents.
	Data []byte

	// Mode is the permission bits to create it with — [execMode] for a bin/ script,
	// [fileMode] otherwise.
	Mode os.FileMode
}

// Build renders the component's files, or reports why the manifest it would carry is
// not one fft would accept.
func (s Scaffold) Build() ([]File, error) {
	m := s.manifest()

	data, err := marshalManifest(m)
	if err != nil {
		return nil, err
	}

	// The guarantee the whole feature rests on: what init writes, install accepts. A
	// round-trip through the same parser install uses is worth more than trusting the
	// templates below to stay in step with the manifest rules.
	if _, err := ParseManifest(data, ManifestName); err != nil {
		return nil, fmt.Errorf("the scaffolded %s would not parse: %w", ManifestName, err)
	}

	files := []File{{Name: ManifestName, Data: data, Mode: fileMode}}

	skeleton, err := s.skeleton()
	if err != nil {
		return nil, err
	}
	files = append(files, skeleton...)

	files = append(files, File{Name: "README.md", Data: []byte(s.readme()), Mode: fileMode})
	return files, nil
}

// manifest is the component.yaml the scaffold carries.
func (s Scaffold) manifest() Manifest {
	m := Manifest{
		APIVersion:  APIVersion,
		Name:        s.Name,
		Version:     "0.1.0",
		Description: fmt.Sprintf("A %s component named %s", s.Kind, s.Name),
		Kind:        s.Kind,
		Exec:        "bin/" + s.execName(),
	}

	switch s.Kind {
	case KindCommand:
		// The safest session, and the one that needs no project to run: default an
		// unset session here rather than lean on the caller, so a bare Scaffold{} for a
		// command is still a valid one.
		session := s.Session
		if session == "" {
			session = SessionNone
		}
		m.Commands = []Command{{
			Name:    s.Name,
			Short:   fmt.Sprintf("%s — describe what this command does", s.Name),
			Session: session,
			// A write session with no mutates would be refused by the manifest rules; a
			// scaffold that could not install is a contradiction, so derive it.
			Mutates: session == SessionWrite,
		}}
	case KindTransport:
		m.Targets = []string{placeholderTarget}
	}
	return m
}

// execName is the executable's base name, which the exec path and every skeleton
// agree on: fft-<name>, the convention the first-party components already follow.
func (s Scaffold) execName() string { return "fft-" + s.Name }

// skeleton renders the executable and, for Go, its module files.
func (s Scaffold) skeleton() ([]File, error) {
	bin := "bin/" + s.execName()

	switch s.Lang {
	case LangShell:
		return []File{{Name: bin, Data: []byte(s.render(s.shellTemplate())), Mode: execMode}}, nil
	case LangPython:
		return []File{{Name: bin, Data: []byte(s.render(s.pythonTemplate())), Mode: execMode}}, nil
	case LangNode:
		return []File{{Name: bin, Data: []byte(s.render(s.nodeTemplate())), Mode: execMode}}, nil
	case LangGo:
		return []File{
			{Name: "main.go", Data: []byte(s.render(s.goTemplate())), Mode: fileMode},
			{Name: "go.mod", Data: []byte(s.goMod()), Mode: fileMode},
		}, nil
	default:
		return nil, fmt.Errorf("unknown language %q: want one of %s",
			s.Lang, strings.Join(supportedLangs, ", "))
	}
}

// render substitutes the tokens a skeleton carries. The set is tiny on purpose — a
// scaffold is source somebody is about to edit, and a template that did more would
// leave less that reads like code they wrote.
func (s Scaffold) render(tmpl string) string {
	r := strings.NewReplacer(
		"{{name}}", s.Name,
		"{{exec}}", s.execName(),
		"{{target}}", placeholderTarget,
	)
	return r.Replace(tmpl)
}

// goMod is a Go skeleton's module file. A command needs no dependency; a transport
// requires fft for the protocol package, pinned when the fft that generated it was a
// release and left to `go mod tidy` when it was not.
func (s Scaffold) goMod() string {
	var b strings.Builder
	fmt.Fprintf(&b, "module %s\n\ngo 1.25\n", s.Name)
	if s.Kind == KindTransport && s.FFTRequire != "" {
		fmt.Fprintf(&b, "\nrequire %s %s\n", modulePath, s.FFTRequire)
	}
	return b.String()
}

// modulePath is fft's own module, which a Go transport imports transportproto from.
const modulePath = "github.com/Joessst-Dev/fft-cli"
