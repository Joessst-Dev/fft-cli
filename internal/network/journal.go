package network

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// JournalSuffix is appended to a snapshot's path to name the journal of the
// crawl writing it.
const JournalSuffix = ".partial.jsonl"

// journalVersion is the journal's own format. A journal is never read by
// anything but the crawl that resumes it, so it does not follow SchemaVersion.
const journalVersion = 1

// journal records each facility's connections as the crawl finishes them, so
// that a crawl cut short — by a token that expired, a gateway that gave up, or
// a laptop lid — resumes where it stopped instead of starting the hour over.
//
// It is JSON lines: a header, then value sets and facilities in the order they
// were interned. A value set is always written before the first facility whose
// edges point at it, so replaying the lines in order rebuilds the same indices.
type journal struct {
	path string
	f    *os.File
	w    *bufio.Writer
}

type journalHeader struct {
	Journal int    `json:"journal"`
	Source  Source `json:"source"`
}

type journalLine struct {
	Set      *int     `json:"set,omitempty"`
	Values   []string `json:"values,omitempty"`
	Facility string   `json:"facility,omitempty"`
	Edges    []Edge   `json:"edges,omitempty"`
}

// openJournal starts a fresh journal at path, or with resume replays the one
// there into b and returns the facilities it already holds.
//
// A journal from another tenant is refused rather than merged: two networks
// stitched together would report every gap between them.
func openJournal(path string, src Source, b *builder, resume bool) (*journal, map[string]bool, error) {
	done := map[string]bool{}
	if !resume {
		return createJournal(path, src, done)
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return createJournal(path, src, done)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("read the crawl journal: %w", err)
	}

	good, err := replay(data, src, b, done)
	if err != nil {
		return nil, nil, fmt.Errorf("resume from %s: %w", path, err)
	}

	f, err := os.OpenFile(path, os.O_WRONLY, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("open the crawl journal: %w", err)
	}
	// A crawl killed mid-write leaves half a line. Cut it off, or the next line
	// appended would be glued to it and the journal would be unreadable.
	if err := f.Truncate(good); err != nil {
		return nil, nil, errors.Join(fmt.Errorf("repair the crawl journal: %w", err), f.Close())
	}
	if _, err := f.Seek(good, io.SeekStart); err != nil {
		return nil, nil, errors.Join(fmt.Errorf("repair the crawl journal: %w", err), f.Close())
	}
	return &journal{path: path, f: f, w: bufio.NewWriter(f)}, done, nil
}

func createJournal(path string, src Source, done map[string]bool) (*journal, map[string]bool, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, nil, fmt.Errorf("create the snapshot directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("create the crawl journal: %w", err)
	}
	j := &journal{path: path, f: f, w: bufio.NewWriter(f)}
	if err := j.write(journalHeader{Journal: journalVersion, Source: src}); err != nil {
		return nil, nil, errors.Join(err, j.close())
	}
	if err := j.w.Flush(); err != nil {
		return nil, nil, errors.Join(fmt.Errorf("write the crawl journal: %w", err), j.close())
	}
	return j, done, nil
}

// replay feeds data's lines into b and returns the length of the part that
// parsed: everything after it is the torn tail of an interrupted write.
func replay(data []byte, src Source, b *builder, done map[string]bool) (int64, error) {
	line, rest, ok := bytes.Cut(data, []byte("\n"))
	if !ok {
		return 0, errors.New("the journal has no header")
	}
	var h journalHeader
	if err := json.Unmarshal(line, &h); err != nil || h.Journal != journalVersion {
		return 0, errors.New("it is not a journal this fft wrote")
	}
	if h.Source != src {
		return 0, fmt.Errorf("it was written for project %q at %s, not %q at %s; "+
			"run without --resume to start over", h.Source.Project, h.Source.BaseURL, src.Project, src.BaseURL)
	}

	good := int64(len(line) + 1)
	for len(rest) > 0 {
		line, next, complete := bytes.Cut(rest, []byte("\n"))
		if !complete {
			break
		}
		var l journalLine
		if err := json.Unmarshal(line, &l); err != nil {
			break
		}
		switch {
		case l.Set != nil:
			if i, _ := b.intern(l.Values); i != *l.Set {
				return 0, fmt.Errorf("value set %d is out of order", *l.Set)
			}
		case l.Facility != "":
			for _, e := range l.Edges {
				for _, c := range e.Context {
					if c.Values >= len(b.snap.ValueSets) {
						return 0, fmt.Errorf("facility %s points at a value set the journal never wrote", l.Facility)
					}
				}
			}
			b.snap.Edges = append(b.snap.Edges, l.Edges...)
			done[l.Facility] = true
		}
		good += int64(len(line) + 1)
		rest = next
	}
	return good, nil
}

// record appends one facility's edges, after the value sets they introduced,
// and flushes: a facility is either in the journal whole or not at all.
func (j *journal) record(b *builder, facility string, edges []Edge, fresh []int) error {
	for _, i := range fresh {
		if err := j.write(journalLine{Set: &i, Values: b.snap.ValueSets[i]}); err != nil {
			return err
		}
	}
	if err := j.write(journalLine{Facility: facility, Edges: edges}); err != nil {
		return err
	}
	if err := j.w.Flush(); err != nil {
		return fmt.Errorf("write the crawl journal: %w", err)
	}
	return nil
}

func (j *journal) write(v any) error {
	line, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode the crawl journal: %w", err)
	}
	if _, err := j.w.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write the crawl journal: %w", err)
	}
	return nil
}

func (j *journal) close() error {
	if j == nil || j.f == nil {
		return nil
	}
	flushErr := j.w.Flush()
	closeErr := j.f.Close()
	j.f = nil
	return errors.Join(flushErr, closeErr)
}

// remove deletes the journal once the snapshot it was building is written.
func (j *journal) remove() error {
	if err := j.close(); err != nil {
		return err
	}
	if err := os.Remove(j.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove the crawl journal: %w", err)
	}
	return nil
}
