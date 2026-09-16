package history_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/history"
)

// entry is a recorded run of operation in project, at minute past a fixed hour.
func entry(project, operation string, minute int) history.Entry {
	return history.Entry{
		V:           history.Version,
		TS:          time.Date(2026, 9, 16, 12, minute, 0, 0, time.UTC),
		Source:      history.SourceCLI,
		Project:     project,
		OperationID: operation,
		Command:     "fft api",
		Args:        []string{operation},
		Status:      200,
		DurationMS:  12,
	}
}

var _ = Describe("a history log", func() {
	var log history.Log

	BeforeEach(func() {
		log = history.Log{Path: filepath.Join(GinkgoT().TempDir(), "state", "fft", "history.jsonl")}
	})

	It("is empty before anything was recorded", func() {
		Expect(log.Read()).To(BeEmpty())
	})

	It("reads back what was appended, oldest first", func() {
		Expect(log.Append(entry("prod", "getFacility", 1))).To(Succeed())
		Expect(log.Append(entry("prod", "addPickJob", 2))).To(Succeed())

		entries, err := log.Read()
		Expect(err).NotTo(HaveOccurred())
		Expect(entries).To(HaveExactElements(
			HaveField("OperationID", "getFacility"),
			HaveField("OperationID", "addPickJob"),
		))
		Expect(entries[0]).To(Equal(entry("prod", "getFacility", 1)))
	})

	It("keeps the file and its directory private to the user", func() {
		if runtime.GOOS == "windows" {
			Skip("POSIX mode bits do not apply on Windows")
		}
		Expect(log.Append(entry("prod", "getFacility", 1))).To(Succeed())

		file, err := os.Stat(log.Path)
		Expect(err).NotTo(HaveOccurred())
		Expect(file.Mode().Perm()).To(Equal(os.FileMode(0o600)))

		dir, err := os.Stat(filepath.Dir(log.Path))
		Expect(err).NotTo(HaveOccurred())
		Expect(dir.Mode().Perm()).To(Equal(os.FileMode(0o700)))
	})

	It("skips the lines that are not entries and keeps the rest", func() {
		Expect(log.Append(entry("prod", "getFacility", 1))).To(Succeed())

		f, err := os.OpenFile(log.Path, os.O_APPEND|os.O_WRONLY, 0o600)
		Expect(err).NotTo(HaveOccurred())
		_, err = f.WriteString("{\"v\":1,\"operationId\":\"cutSho\n\nnot json at all\n{}\n")
		Expect(err).NotTo(HaveOccurred())
		Expect(f.Close()).To(Succeed())

		Expect(log.Append(entry("prod", "addPickJob", 2))).To(Succeed())

		Expect(log.Read()).To(HaveExactElements(
			HaveField("OperationID", "getFacility"),
			HaveField("OperationID", "addPickJob"),
		))
	})

	It("writes only whole lines when many processes append at once", func() {
		const writers, each = 8, 50

		var wg sync.WaitGroup
		for w := range writers {
			wg.Go(func() {
				defer GinkgoRecover()
				for i := range each {
					e := entry(fmt.Sprintf("p%d", w), "getFacility", i%60)
					// Long enough that a torn write would show.
					e.Args = []string{strings.Repeat("x", 512)}
					Expect(log.Append(e)).To(Succeed())
				}
			})
		}
		wg.Wait()

		data, err := os.ReadFile(log.Path)
		Expect(err).NotTo(HaveOccurred())
		lines := bytes.Split(bytes.TrimSuffix(data, []byte("\n")), []byte("\n"))
		Expect(lines).To(HaveLen(writers * each))
		for _, line := range lines {
			Expect(json.Valid(line)).To(BeTrue(), "a torn line: %q", line)
		}
	})

	When("it grows past its limit", func() {
		BeforeEach(func() {
			log.MaxBytes = 4096
		})

		It("keeps the newest entries, in about half of the limit", func() {
			for i := range 100 {
				e := entry("prod", fmt.Sprintf("op%03d", i), i%60)
				Expect(log.Append(e)).To(Succeed())
			}

			info, err := os.Stat(log.Path)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.Size()).To(BeNumerically("<=", log.MaxBytes))

			entries, err := log.Read()
			Expect(err).NotTo(HaveOccurred())
			Expect(entries).NotTo(BeEmpty())
			Expect(entries[len(entries)-1].OperationID).To(Equal("op099"), "the newest entry was dropped")
			Expect(entries[0].OperationID).NotTo(Equal("op000"), "nothing was dropped")
			for i := 1; i < len(entries); i++ {
				Expect(entries[i].OperationID > entries[i-1].OperationID).To(BeTrue(), "entries are out of order")
			}
		})

		It("drops a corrupt line while it is at it", func() {
			Expect(os.MkdirAll(filepath.Dir(log.Path), 0o700)).To(Succeed())
			Expect(os.WriteFile(log.Path, []byte("garbage\n"), 0o600)).To(Succeed())
			for i := range 60 {
				Expect(log.Append(entry("prod", fmt.Sprintf("op%03d", i), i))).To(Succeed())
			}

			data, err := os.ReadFile(log.Path)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(data)).NotTo(ContainSubstring("garbage"))
		})

		It("compacts under many appenders without losing the file", func() {
			var wg sync.WaitGroup
			for w := range 4 {
				wg.Go(func() {
					defer GinkgoRecover()
					for i := range 100 {
						Expect(log.Append(entry(fmt.Sprintf("p%d", w), "getFacility", i%60))).To(Succeed())
					}
				})
			}
			wg.Wait()

			data, err := os.ReadFile(log.Path)
			Expect(err).NotTo(HaveOccurred())
			for line := range bytes.SplitSeq(bytes.TrimSuffix(data, []byte("\n")), []byte("\n")) {
				Expect(json.Valid(line)).To(BeTrue(), "a torn line: %q", line)
			}
			Expect(log.Read()).NotTo(BeEmpty())
		})
	})

	Describe("clearing it", func() {
		It("removes every entry and says how many there were", func() {
			Expect(log.Append(entry("prod", "getFacility", 1))).To(Succeed())
			Expect(log.Append(entry("prod", "getFacility", 2))).To(Succeed())

			Expect(log.Clear()).To(Equal(2))
			Expect(log.Read()).To(BeEmpty())
		})

		It("is not an error when there is nothing to clear", func() {
			Expect(log.Clear()).To(BeZero())
		})
	})
})

var _ = Describe("the default history path", func() {
	It("lives in the XDG state directory", func() {
		dir := GinkgoT().TempDir()
		GinkgoT().Setenv("XDG_STATE_HOME", dir)

		Expect(history.Path()).To(Equal(filepath.Join(dir, "fft", "history.jsonl")))
	})

	It("falls back to ~/.local/state", func() {
		home := GinkgoT().TempDir()
		GinkgoT().Setenv("XDG_STATE_HOME", "")
		GinkgoT().Setenv("HOME", home)
		GinkgoT().Setenv("USERPROFILE", home)

		Expect(history.Path()).To(Equal(filepath.Join(home, ".local", "state", "fft", "history.jsonl")))
	})
})

var _ = Describe("the most used operations", func() {
	entries := []history.Entry{
		entry("prod", "getFacility", 1),
		entry("prod", "addPickJob", 2),
		entry("prod", "getFacility", 3),
		entry("staging", "getFacility", 4),
		entry("staging", "getFacility", 5),
		entry("staging", "getFacility", 6),
		entry("prod", "getStock", 7),
	}

	It("counts one project's operations, most used first and the latest first among equals", func() {
		top := history.Top(entries, "prod", 0)

		Expect(top).To(HaveExactElements(
			And(HaveField("OperationID", "getFacility"), HaveField("Count", 2)),
			And(HaveField("OperationID", "getStock"), HaveField("Count", 1)),
			And(HaveField("OperationID", "addPickJob"), HaveField("Count", 1)),
		))
		Expect(top[0].LastUsed).To(Equal(entries[2].TS))
	})

	It("counts each project separately when asked for all of them", func() {
		top := history.Top(entries, "", 2)

		Expect(top).To(HaveExactElements(
			And(HaveField("Project", "staging"), HaveField("OperationID", "getFacility"), HaveField("Count", 3)),
			And(HaveField("Project", "prod"), HaveField("OperationID", "getFacility"), HaveField("Count", 2)),
		))
	})

	It("is empty for a project that has no history", func() {
		Expect(history.Top(entries, "dev", 5)).To(BeEmpty())
	})
})
