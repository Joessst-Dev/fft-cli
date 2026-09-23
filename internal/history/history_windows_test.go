//go:build windows

package history_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/atomicfile"
	"github.com/Joessst-Dev/fft-cli/internal/history"
)

// What CI hits now and then — appenders that never leave the file free for the
// length of one rename — made to happen every time. os.Open takes the same kind
// of handle an appender holds, without FILE_SHARE_DELETE, so for as long as it is
// open no rename can replace the file, and compaction runs out its whole budget.
var _ = Describe("a history file another process holds open", func() {
	It("is left uncompacted by an append, which still records its entry", func() {
		dir := GinkgoT().TempDir()
		var skipped []error
		log := history.Log{
			Path:     filepath.Join(dir, "history.jsonl"),
			MaxBytes: 4096,
			OnSkip:   func(err error) { skipped = append(skipped, err) },
		}

		var data []byte
		for i := range 60 {
			line, err := json.Marshal(entry("prod", fmt.Sprintf("op%03d", i), i))
			Expect(err).NotTo(HaveOccurred())
			data = append(append(data, line...), '\n')
		}
		// Over the limit, so compaction is certainly attempted; under the read limit,
		// so the assertions below see the whole file rather than its tail.
		Expect(int64(len(data))).To(BeNumerically(">", log.MaxBytes))
		Expect(int64(len(data))).To(BeNumerically("<", 4*log.MaxBytes))
		Expect(os.WriteFile(log.Path, data, 0o600)).To(Succeed())

		held, err := os.Open(log.Path)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(held.Close)

		Expect(log.Append(entry("prod", "opLast", 1))).To(Succeed())

		info, err := os.Stat(log.Path)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Size()).To(BeNumerically(">", log.MaxBytes), "the file was compacted after all")

		// Silence here would be a history that stops shrinking with nothing to say why.
		Expect(skipped).To(HaveLen(1))
		Expect(skipped[0]).To(MatchError(ContainSubstring("held open by another process")))

		read, err := log.Read()
		Expect(err).NotTo(HaveOccurred())
		Expect(read[0].OperationID).To(Equal("op000"), "entries were dropped")
		Expect(read[len(read)-1].OperationID).To(Equal("opLast"))
		// atomicfile removes its temporary file on every failed attempt, and a
		// compaction that gave up made one per millisecond of its budget.
		Expect(filepath.Glob(filepath.Join(dir, atomicfile.TempPrefix+"*"))).To(BeEmpty())
	})
})
