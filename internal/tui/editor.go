package tui

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Joessst-Dev/fft-cli/internal/component"
)

// editorDoneMsg says the editor opened on path has exited, with err when it
// could not start or did not exit cleanly.
type editorDoneMsg struct {
	path string
	gen  uint64
	err  error
}

// editorCommand is the editor to open a file in, as a program and its first
// arguments: $VISUAL, else $EDITOR, else one every system of its kind has. $VISUAL
// comes first by the same convention git and crontab follow: it names the editor
// for a full-screen terminal, which is what the UI hands over.
//
// The variable is split into words here, and never handed to a shell. A shell
// would also expand whatever else the variable holds — $(…), ;, a redirection —
// and an editor setting is not where a command line should be able to hide.
func editorCommand(getenv func(string) string) ([]string, error) {
	for _, name := range []string{"VISUAL", "EDITOR"} {
		value := strings.TrimSpace(getenv(name))
		if value == "" {
			continue
		}
		argv, err := splitCommand(value, runtime.GOOS != "windows")
		if err != nil {
			return nil, fmt.Errorf("$%s: %w", name, err)
		}
		if len(argv) > 0 {
			return argv, nil
		}
	}
	if runtime.GOOS == "windows" {
		return []string{"notepad"}, nil
	}
	return []string{"vi"}, nil
}

// errUnclosed is what splitCommand says about a quote that never ends,
// or a backslash with nothing after it.
var errUnclosed = errors.New("a quote or a backslash is not closed")

// splitCommand splits s into words the way a POSIX shell splits a simple command,
// and does nothing else a shell does: no variables, no globs, no operators.
// Single quotes keep everything literally; double quotes keep everything but a
// backslash before " or \. Outside quotes, a backslash keeps the next character —
// unless backslashes is false, as on Windows, where they separate a path.
func splitCommand(s string, backslashes bool) ([]string, error) {
	var (
		words  []string
		word   strings.Builder
		inWord bool
		quote  rune
	)
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		escapes := backslashes && r == '\\'
		switch {
		case quote == '\'':
			if r == '\'' {
				quote = 0
			} else {
				word.WriteRune(r)
			}
		case quote == '"':
			switch {
			case r == '"':
				quote = 0
			case escapes && i+1 < len(runes) && (runes[i+1] == '"' || runes[i+1] == '\\'):
				i++
				word.WriteRune(runes[i])
			default:
				word.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
			inWord = true
		case escapes:
			if i+1 == len(runes) {
				return nil, errUnclosed
			}
			i++
			word.WriteRune(runes[i])
			inWord = true
		case r == ' ' || r == '\t' || r == '\n':
			if inWord {
				words = append(words, word.String())
				word.Reset()
				inWord = false
			}
		default:
			word.WriteRune(r)
			inWord = true
		}
	}
	if quote != 0 {
		return nil, errUnclosed
	}
	if inWord {
		words = append(words, word.String())
	}
	return words, nil
}

// bodyFileName is the name the body has in its directory. The directory is the
// session's alone, so the name need not be unpredictable; the extension is what
// tells an editor to treat it as JSON.
const bodyFileName = "body.json"

// writeBodyFile writes body to a file in a new private directory under parent (""
// for the system's temporary directory), and returns the file's path.
//
// A directory rather than a file: an editor writes a swap file, a backup or an
// undo file beside what it edits, holding the same body, and those go when the
// directory does.
func writeBodyFile(parent string, body []byte) (string, error) {
	// MkdirTemp makes the directory 0700 under a name nobody else has taken, so
	// nothing else can list it, or have put a link where the file goes.
	dir, err := os.MkdirTemp(parent, "fft-body-*")
	if err != nil {
		return "", fmt.Errorf("create a directory for the editor: %w", err)
	}
	path := filepath.Join(dir, bodyFileName)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		// Best effort: the write failed, and the directory is of no use to anyone.
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("write the body for the editor: %w", err)
	}
	return path, nil
}

// openEditor writes body to a private file and opens the user's editor on it. The
// file's directory is always removed: when the editor exits, whether it could
// start or not, and by [session.cleanup] if the UI ends first.
func (s *session) openEditor(body []byte, gen uint64) (tea.Cmd, error) {
	argv, err := editorCommand(s.getenv)
	if err != nil {
		return nil, err
	}
	path, err := writeBodyFile(s.tempDir, body)
	if err != nil {
		return nil, err
	}
	s.tempDirs[filepath.Dir(path)] = true

	c := exec.Command(argv[0], append(argv[1:], path)...)
	// The editor is the user's program, not fft's, and a plugin or a shell escape in
	// it has no business with the credentials a headless session exported.
	c.Env = component.WithoutFFT(s.environ())
	return s.execProcess(c, func(err error) tea.Msg {
		return editorDoneMsg{path: path, gen: gen, err: err}
	}), nil
}

// finishEditing reads back what the editor left in msg's file, and removes the
// file's directory with everything in it.
func (s *session) finishEditing(msg editorDoneMsg) ([]byte, error) {
	defer func() {
		// Nothing more can be done about a directory that will not go: the UI ends by
		// trying once more.
		dir := filepath.Dir(msg.path)
		if err := os.RemoveAll(dir); err == nil {
			delete(s.tempDirs, dir)
		}
	}()
	if msg.err != nil {
		return nil, fmt.Errorf("the editor did not finish: %w", msg.err)
	}
	body, err := readEditedBody(msg.path)
	if err != nil {
		return nil, fmt.Errorf("read what the editor saved: %w", err)
	}
	return body, nil
}

// maxEditedBody is the largest body read back from the editor. The file is the
// user's to fill, and a paste gone wrong must not have the UI hold whatever it
// holds; a body for one request is nowhere near it.
const maxEditedBody = 8 << 20

func readEditedBody(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	body, err := io.ReadAll(io.LimitReader(f, maxEditedBody+1))
	if err := errors.Join(err, f.Close()); err != nil {
		return nil, err
	}
	if len(body) > maxEditedBody {
		return nil, fmt.Errorf("the body is too large to load back: the editor left more than %d MiB", maxEditedBody>>20)
	}
	return body, nil
}
