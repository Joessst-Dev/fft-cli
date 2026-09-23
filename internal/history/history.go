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
	"io"
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

	// Args are the command's positional arguments and the flags it was given, as
	// [Args] records them. Read them with [SplitArgs]: an argument that starts
	// with a dash follows the flags and a "--".
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
	// it, keeping the newest entries. Zero means [DefaultMaxBytes]. A read keeps
	// at most the last [readLimitFactor] times MaxBytes of the file.
	MaxBytes int64
}

// readLimitFactor bounds a read relative to MaxBytes. Append keeps the file under
// MaxBytes, give or take a compaction that stood down (see [Log.compact]), so only
// a file something else wrote comes near it; and the newest entries of such a file
// are the ones worth reading.
const readLimitFactor = 4

// errNotRegular is what every function here says about a history path that holds
// something other than a file: a FIFO, which would hold up every command until
// something read it, a device, which may never end, or a symlink, which would
// have fft write to a file it does not own.
var errNotRegular = errors.New("not a regular file; move it aside for fft to keep a history there")

// errBusy marks a failure that is nothing but another process's open handle.
// Windows opens files without FILE_SHARE_DELETE, so an open and a rename of the
// same path exclude each other: an appender's handle refuses the rename that ends
// a compaction, and that rename refuses every open. The retry in open_windows.go
// gives up with this rather than with the bare "access is denied", so that a file
// compaction could not have to itself is told apart from one it could not write.
// On unix, where a rename over an open file is ordinary, it never occurs.
//
// It says why, not what to do. Compaction skips its turn on it; an append must
// not. An entry that lost the race was never written, and [Log.Append] has to
// keep saying so.
var errBusy = errors.New("the history file is held open by another process")

// CompactError is what [Log.Append] returns when the entry itself was written but
// the trailing compaction it triggered failed. Unwrap it to inspect the cause; a
// caller that only cares whether the entry was recorded should treat it as
// success and report the wrapped error on its own debug channel instead. A
// compaction that only stood down is not a failure and is not one of these.
type CompactError struct {
	Err error
}

func (e *CompactError) Error() string { return "compact history: " + e.Err.Error() }

func (e *CompactError) Unwrap() error { return e.Err }

func (l Log) maxBytes() int64 {
	if l.MaxBytes > 0 {
		return l.MaxBytes
	}
	return DefaultMaxBytes
}

// Append adds e to the end of the file, creating it (0600, in a 0700 directory)
// if needed, and taking group and other access away from a file or directory that
// has it.
//
// The entry is one write to a file opened for appending, which is what lets any
// number of fft processes record at once without a lock: the operating system
// places each write at the end, whole. Only compaction, which rewrites the file,
// takes a lock — and skips its turn rather than wait, for that lock or for the
// handles that keep it from renaming anything into place on Windows.
func (l Log) Append(e Entry) error {
	line, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("encode history entry: %w", err)
	}
	line = append(line, '\n')

	dir := filepath.Dir(l.Path)
	if err := os.MkdirAll(dir, atomicfile.DirMode); err != nil {
		return fmt.Errorf("create the history directory: %w", err)
	}
	// Before the entry is written, not after: an entry that cannot be kept private
	// is not written at all.
	if err := tightenDir(dir); err != nil {
		return fmt.Errorf("make %s private: %w", dir, err)
	}

	f, info, err := l.open(os.O_APPEND | os.O_CREATE | os.O_WRONLY)
	if err != nil {
		return err
	}
	if err := tightenFile(f, info); err != nil {
		return errors.Join(fmt.Errorf("make %s private: %w", l.Path, err), f.Close())
	}
	n, writeErr := f.Write(line)
	if err := errors.Join(writeErr, f.Close()); err != nil {
		return fmt.Errorf("append to %s: %w", l.Path, err)
	}

	// The size before the write plus the write is enough to decide on: compaction
	// looks at the file again under its lock before it rewrites anything.
	if info.Size()+int64(n) > l.maxBytes() {
		if err := l.compact(); err != nil {
			// The entry above is already durable; only the trailing compaction
			// failed, and that must not read to a caller as "not recorded".
			return &CompactError{Err: err}
		}
	}
	return nil
}

// compact rewrites the file with its newest entries, about half of MaxBytes.
//
// It is allowed to do nothing. Another process holding the lock, or on Windows
// holding a handle no rename can get past, leaves the file over its limit until an
// append finds it quiet — a cost paid in bytes, where failing would be paid by a
// command that has already been recorded.
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

	data, err := l.read()
	if err != nil {
		// Losing the file to another process's handle is the same non-event as
		// losing the lock above: nothing is wrong with the history, and whichever
		// append next finds it over the limit and quiet compacts it.
		if errors.Is(err, errBusy) {
			return nil
		}
		return err
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
		// The newest entry is kept whatever its size: dropping it would leave the
		// file empty, and the run just recorded with nothing to show for it.
		if size+next > budget && keep < len(lines) {
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
	// Same as the read above: a rename no appender's handle would let through is a
	// turn skipped, not a failure.
	if err := replace(l.Path, out.Bytes()); err != nil && !errors.Is(err, errBusy) {
		return err
	}
	return nil
}

// Read returns every entry in the file, oldest first. A file that does not exist
// is an empty history. A line that is not an entry — a write cut short by a full
// disk, a hand edit — is skipped, so one bad line never costs the rest.
//
// A file larger than the read limit (see [Log.MaxBytes]) is read from the end: its
// newest entries are returned, and the older ones are left unread.
func (l Log) Read() ([]Entry, error) {
	data, err := l.read()
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
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

// open opens the history file with flag, and refuses anything but a regular file
// before a byte is read or written.
//
// The path is looked at first, without following a link, so that a FIFO or a
// symlink is named for what it is rather than by the error its open gives; the
// opened file is looked at again, in case the path changed in between.
func (l Log) open(flag int) (*os.File, fs.FileInfo, error) {
	if info, err := os.Lstat(l.Path); err == nil && !info.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("%s: %w", l.Path, errNotRegular)
	}
	f, err := openFile(l.Path, flag|openFlags)
	if err != nil {
		return nil, nil, err
	}
	info, err := f.Stat()
	if err == nil && !info.Mode().IsRegular() {
		err = fmt.Errorf("%s: %w", l.Path, errNotRegular)
	}
	if err != nil {
		return nil, nil, errors.Join(err, f.Close())
	}
	return f, info, nil
}

// read returns the file's contents, or, for a file over the read limit, its last
// whole lines within the limit.
func (l Log) read() ([]byte, error) {
	f, info, err := l.open(os.O_RDONLY)
	if err != nil {
		return nil, err
	}
	data, err := readTail(f, info.Size(), readLimitFactor*l.maxBytes())
	if err := errors.Join(err, f.Close()); err != nil {
		return nil, fmt.Errorf("read %s: %w", l.Path, err)
	}
	return data, nil
}

// readTail reads at most limit bytes of f, which was size bytes long when it was
// opened. A longer file is read from the start of the first line that begins
// within the last limit bytes; a file that grew since is still cut at limit, and
// the line that cuts short is skipped like any other that is not an entry.
func readTail(f *os.File, size, limit int64) ([]byte, error) {
	if size <= limit {
		return io.ReadAll(io.LimitReader(f, limit))
	}
	// One byte early, so that a cut that falls right after a newline keeps the line
	// that starts there.
	if _, err := f.Seek(size-limit-1, io.SeekStart); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	i := bytes.IndexByte(data, '\n')
	if i < 0 {
		return nil, nil
	}
	return data[i+1:], nil
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
