// Package history is fft's local record of the requests it has sent.
//
// Each entry is metadata about one command line that addressed an API operation:
// when, against which project, which operation, with which flags (redacted), and
// how it ended. Request and response bodies are never recorded. The record is a
// JSON Lines file owned by the user, which the CLI appends to and `fft tui` reads
// to offer the operations a user reaches for most.
//
// Recording is best-effort by design. A command's output and exit code must not
// depend on whether its history could be written, so every function here reports
// its failure and leaves the decision to ignore it to the caller.
package history

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/Joessst-Dev/fft-cli/internal/atomicfile"
)

// Version is the entry format this package writes.
const Version = 1

// DefaultMaxBytes is the size above which [Log.Append] compacts the file.
const DefaultMaxBytes = 1 << 20

// compactLockWait bounds how long an append waits to compact. Another process
// holding the lock is compacting already, so waiting longer would only delay the
// command whose entry has already been written.
const compactLockWait = 50 * time.Millisecond

// The sources an entry can come from.
const (
	SourceCLI = "cli"
	SourceTUI = "tui"
)

// Entry is one recorded command line.
type Entry struct {
	V      int       `json:"v"`
	TS     time.Time `json:"ts"`
	Source string    `json:"source"`

	// Project is the name of the project the command acted on, "" when it failed
	// before one was resolved.
	Project string `json:"project,omitempty"`

	OperationID string `json:"operationId"`

	// Command is the command's path, "fft facility list".
	Command string `json:"command"`

	// Args are the command's positional arguments followed by the flags it was
	// given, each as --name=value, after [Redact].
	Args []string `json:"args,omitempty"`

	// Status is the HTTP status of the last response, 0 when none arrived.
	Status int `json:"status,omitempty"`

	Exit       int   `json:"exit"`
	DurationMS int64 `json:"durationMs"`
}

// Path is the default history file: $XDG_STATE_HOME/fft/history.jsonl, or
// ~/.local/state/fft/history.jsonl when XDG_STATE_HOME is unset.
func Path() (string, error) {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "fft", "history.jsonl"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate the home directory: %w", err)
	}
	return filepath.Join(home, ".local", "state", "fft", "history.jsonl"), nil
}

// Log is a history file.
type Log struct {
	Path string

	// MaxBytes is the size above which Append compacts the file to about half of
	// it, keeping the newest entries. Zero means [DefaultMaxBytes].
	MaxBytes int64
}

func (l Log) maxBytes() int64 {
	if l.MaxBytes > 0 {
		return l.MaxBytes
	}
	return DefaultMaxBytes
}

// Append adds e to the end of the file, creating it (0600, in a 0700 directory)
// if needed.
//
// The entry is one write to a file opened for appending, which is what lets any
// number of fft processes record at once without a lock: the operating system
// places each write at the end, whole. Only compaction, which rewrites the file,
// takes a lock — and skips its turn rather than wait for one.
func (l Log) Append(e Entry) error {
	line, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("encode history entry: %w", err)
	}
	line = append(line, '\n')

	if err := os.MkdirAll(filepath.Dir(l.Path), atomicfile.DirMode); err != nil {
		return fmt.Errorf("create the history directory: %w", err)
	}

	f, err := os.OpenFile(l.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, atomicfile.FileMode)
	if err != nil {
		return fmt.Errorf("open %s: %w", l.Path, err)
	}
	_, writeErr := f.Write(line)
	info, statErr := f.Stat()
	if err := errors.Join(writeErr, statErr, f.Close()); err != nil {
		return fmt.Errorf("append to %s: %w", l.Path, err)
	}

	if info.Size() > l.maxBytes() {
		return l.compact()
	}
	return nil
}

// compact rewrites the file with its newest entries, about half of MaxBytes.
//
// An append that lands between the read and the rename below is written to the
// file being replaced and is lost with it. That is the price of never making an
// append wait, and it is paid at most once per megabyte of history.
func (l Log) compact() error {
	unlock, locked, err := tryLock(l.Path+".lock", compactLockWait)
	if err != nil {
		return fmt.Errorf("lock %s for compaction: %w", l.Path, err)
	}
	if !locked {
		return nil
	}
	defer unlock()

	data, err := os.ReadFile(l.Path)
	if err != nil {
		return fmt.Errorf("read %s: %w", l.Path, err)
	}
	// Another process may have compacted while this one waited for the lock.
	if int64(len(data)) <= l.maxBytes() {
		return nil
	}

	budget := l.maxBytes() / 2
	lines := bytes.Split(bytes.TrimRight(data, "\n"), []byte("\n"))
	keep := len(lines)
	var size int64
	for keep > 0 {
		next := int64(len(lines[keep-1])) + 1
		if size+next > budget {
			break
		}
		size += next
		keep--
	}

	var out bytes.Buffer
	out.Grow(int(size))
	for _, line := range lines[keep:] {
		// A corrupt line would only be skipped by every future Read; compaction is
		// the one chance to drop it.
		if _, ok := decode(line); ok {
			out.Write(line)
			out.WriteByte('\n')
		}
	}
	return atomicfile.Write(l.Path, out.Bytes())
}

// Read returns every entry in the file, oldest first. A file that does not exist
// is an empty history. A line that is not an entry — a write cut short by a full
// disk, a hand edit — is skipped, so one bad line never costs the rest.
func (l Log) Read() ([]Entry, error) {
	data, err := os.ReadFile(l.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", l.Path, err)
	}

	var entries []Entry
	for line := range bytes.SplitSeq(data, []byte("\n")) {
		if e, ok := decode(line); ok {
			entries = append(entries, e)
		}
	}
	return entries, nil
}

// Clear deletes the history, reporting how many entries it held. Clearing a
// history that does not exist is not an error.
func (l Log) Clear() (int, error) {
	entries, err := l.Read()
	if err != nil {
		return 0, err
	}
	if err := os.Remove(l.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return 0, fmt.Errorf("remove %s: %w", l.Path, err)
	}
	return len(entries), nil
}

func decode(line []byte) (Entry, bool) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return Entry{}, false
	}
	var e Entry
	if err := json.Unmarshal(line, &e); err != nil || e.V < 1 || e.OperationID == "" {
		return Entry{}, false
	}
	return e, true
}
