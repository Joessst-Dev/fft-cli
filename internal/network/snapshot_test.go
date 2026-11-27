package network_test

import (
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/network"
)

var _ = Describe("a snapshot on disk", func() {
	var path string

	BeforeEach(func() { path = filepath.Join(GinkgoT().TempDir(), "s.json.gz") })

	gzipped := func(content string) {
		f, err := os.Create(path)
		Expect(err).NotTo(HaveOccurred())
		zw := gzip.NewWriter(f)
		_, err = zw.Write([]byte(content))
		Expect(err).NotTo(HaveOccurred())
		Expect(zw.Close()).To(Succeed())
		Expect(f.Close()).To(Succeed())
	}

	It("reads back what was saved", func() {
		s := snapshot(spec{target: wildcard(network.TypeManaged), context: []entry{in(network.ContextTag, "express")}})

		Expect(network.Save(path, s)).To(Succeed())
		loaded, err := network.Load(path)

		Expect(err).NotTo(HaveOccurred())
		Expect(loaded).To(Equal(s))
	})

	DescribeTable("is refused with exit 3 and a way forward",
		func(prepare func(), message, hint string) {
			prepare()

			_, err := network.Load(path)

			Expect(err).To(MatchError(ContainSubstring(message)))
			Expect(exitcode.FromError(err)).To(Equal(exitcode.Config))
			var snapErr *network.SnapshotError
			Expect(errors.As(err, &snapErr)).To(BeTrue())
			Expect(snapErr.Hint()).To(ContainSubstring(hint))
		},
		Entry("when there is none", func() {}, "there is no snapshot there", "fft network snapshot"),
		Entry("when it is not gzip", func() {
			Expect(os.WriteFile(path, []byte("{}"), 0o600)).To(Succeed())
		}, "read the snapshot", "not a snapshot fft wrote"),
		Entry("when it was cut short", func() { gzipped(`{"schemaVersion":1,"facilities":[`) },
			"unexpected EOF", "cut short"),
		Entry("when it has no schema version", func() { gzipped(`{}`) }, "no schema version", "not a snapshot fft wrote"),
		Entry("when a newer fft wrote it", func() { gzipped(`{"schemaVersion":99}`) },
			"format 99", "Upgrade fft"),
		Entry("when a context entry points past the value sets", func() {
			gzipped(`{"schemaVersion":1,"edges":[{"id":"c1","context":[{"type":"TAG_REFERENCE","values":4}]}]}`)
		}, "no value set", "not a snapshot fft wrote"),
	)
})
