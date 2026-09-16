package tui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	tea "charm.land/bubbletea/v2"
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

// writeBodyFile writes body to a new private file in dir ("" for the system's
// temporary directory), and returns its path.
func writeBodyFile(dir string, body []byte) (string, error) {
	// CreateTemp makes the file 0600 and refuses a name that exists, so nothing
	// else can have it open, or have put a link where it goes.
	f, err := os.CreateTemp(dir, "fft-body-*.json")
	if err != nil {
		return "", fmt.Errorf("create a file for the editor: %w", err)
	}
	path := f.Name()
	_, err = f.Write(body)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		// Best effort: the write failed, and the file is of no use to anyone.
		_ = os.Remove(path)
		return "", fmt.Errorf("write the body for the editor: %w", err)
	}
	return path, nil
}

// openEditor writes body to a private file and opens the user's editor on it. The
// file is always removed: when the editor exits, whether it could start or not,
// and by [session.cleanup] if the UI ends first.
func (s *session) openEditor(body []byte, gen uint64) (tea.Cmd, error) {
	argv, err := editorCommand(s.getenv)
	if err != nil {
		return nil, err
	}
	path, err := writeBodyFile(s.tempDir, body)
	if err != nil {
		return nil, err
	}
	s.tempFiles[path] = true

	c := exec.Command(argv[0], append(argv[1:], path)...)
	return s.execProcess(c, func(err error) tea.Msg {
		return editorDoneMsg{path: path, gen: gen, err: err}
	}), nil
}

// finishEditing reads back what the editor left in msg's file, and removes it.
func (s *session) finishEditing(msg editorDoneMsg) ([]byte, error) {
	defer func() {
		// Nothing more can be done about a file that will not go: the UI ends by
		// trying once more.
		if err := os.Remove(msg.path); err == nil || errors.Is(err, os.ErrNotExist) {
			delete(s.tempFiles, msg.path)
		}
	}()
	if msg.err != nil {
		return nil, fmt.Errorf("the editor did not finish: %w", msg.err)
	}
	body, err := os.ReadFile(msg.path)
	if err != nil {
		return nil, fmt.Errorf("read what the editor saved: %w", err)
	}
	return body, nil
}
