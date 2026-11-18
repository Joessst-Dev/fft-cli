package component

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("unpack", func() {
	// localPath judges an entry by its name alone, and "bin/fft-weather" is a
	// perfectly good name. What it cannot see is that bin/ already exists as a link
	// out of the directory — so this is the case only os.Root stops, and it fails if
	// the extractor ever goes back to writing through plain os calls.
	DescribeTable("will not write through a symlink already inside the directory",
		func(asset string, archive []byte) {
			dir := GinkgoT().TempDir()
			outside := GinkgoT().TempDir()
			if err := os.Symlink(outside, filepath.Join(dir, "bin")); err != nil {
				Skip("symlinks unsupported here: " + err.Error())
			}

			Expect(unpack(asset, archive, dir)).To(MatchError(ContainSubstring("create")))

			escaped, err := os.ReadDir(outside)
			Expect(err).NotTo(HaveOccurred())
			Expect(escaped).To(BeEmpty())
		},
		Entry("a tarball", "weather.tar.gz", oneFileTarGz("bin/fft-weather")),
		Entry("a zip", "weather.zip", oneFileZip("bin/fft-weather")),
	)
})

var _ = Describe("localPath", func() {
	DescribeTable("accepts a name that stays inside the component",
		func(name, want string) {
			Expect(localPath(name)).To(Equal(filepath.FromSlash(want)))
		},
		Entry("a file at the root", "component.yaml", "component.yaml"),
		Entry("a nested file", "bin/fft-weather", "bin/fft-weather"),
		Entry("a directory entry", "bin/", "bin"),
		Entry("the root itself, as tar -C dir . writes it", "./", "."),
		Entry("a detour that comes back", "bin/../component.yaml", "component.yaml"),
	)

	DescribeTable("refuses a name that does not",
		func(name, reason string) {
			_, err := localPath(name)
			Expect(err).To(MatchError(ContainSubstring(reason)))
		},
		Entry("a parent", "..", "outside the component"),
		Entry("a climb", "../../etc/passwd", "outside the component"),
		Entry("a climb after a detour", "bin/../../x", "outside the component"),
		Entry("an absolute path", "/etc/passwd", "not a relative path"),
		Entry("a backslash", `..\..\x`, "not a relative path"),
		Entry("an empty name", "", "not a relative path"),
	)
})

func oneFileTarGz(name string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	body := []byte("#!/bin/sh\n")
	Expect(tw.WriteHeader(&tar.Header{
		Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg,
	})).To(Succeed())
	_, err := tw.Write(body)
	Expect(err).NotTo(HaveOccurred())

	Expect(tw.Close()).To(Succeed())
	Expect(gz.Close()).To(Succeed())
	return buf.Bytes()
}

func oneFileZip(name string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	header := &zip.FileHeader{Name: name, Method: zip.Deflate}
	header.SetMode(0o755)
	w, err := zw.CreateHeader(header)
	Expect(err).NotTo(HaveOccurred())
	_, err = w.Write([]byte("#!/bin/sh\n"))
	Expect(err).NotTo(HaveOccurred())

	Expect(zw.Close()).To(Succeed())
	return buf.Bytes()
}
