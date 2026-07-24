package component_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/component"
)

// A stand-in GitHub: one release, whatever assets a spec put in it.
type fakeGitHub struct {
	tag    string
	assets map[string][]byte
	server *httptest.Server
}

func newFakeGitHub(tag string) *fakeGitHub {
	g := &fakeGitHub{tag: tag, assets: map[string][]byte{}}

	mux := http.NewServeMux()
	mux.HandleFunc("/assets/", func(w http.ResponseWriter, r *http.Request) {
		body, ok := g.assets[filepath.Base(r.URL.Path)]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body)
	})
	mux.HandleFunc("/repos/", func(w http.ResponseWriter, r *http.Request) {
		type asset struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		}

		payload := struct {
			TagName string  `json:"tag_name"`
			Assets  []asset `json:"assets"`
		}{TagName: g.tag}

		for name := range g.assets {
			payload.Assets = append(payload.Assets, asset{
				Name: name,
				URL:  g.server.URL + "/assets/" + name,
			})
		}
		Expect(json.NewEncoder(w).Encode(payload)).To(Succeed())
	})

	g.server = httptest.NewServer(mux)
	DeferCleanup(g.server.Close)

	return g
}

// publish adds an asset and lists it in checksums.txt with its real digest.
func (g *fakeGitHub) publish(name string, body []byte) {
	g.assets[name] = body

	sum := sha256.Sum256(body)
	g.checksum(name, hex.EncodeToString(sum[:]))
}

// checksum records a digest for an asset, right or wrong — a spec proving that a
// mismatch is caught needs to be able to record a wrong one.
func (g *fakeGitHub) checksum(name, digest string) {
	line := fmt.Appendf(nil, "%s  %s\n", digest, name)
	g.assets["checksums.txt"] = append(g.assets["checksums.txt"], line...)
}

// assetName is what this platform's archive of a component is called.
func assetName(name, version string) string {
	if runtime.GOOS == "windows" {
		return fmt.Sprintf("fft-component-%s_%s_%s_%s.zip", name, version, runtime.GOOS, runtime.GOARCH)
	}
	return fmt.Sprintf("fft-component-%s_%s_%s_%s.tar.gz", name, version, runtime.GOOS, runtime.GOARCH)
}

// archiveOf builds the archive this platform's release would carry, so that the
// bytes always match what [assetName] calls them.
//
// Both formats, rather than tar.gz everywhere, because the format is chosen by the
// asset's *name* and Windows releases are zips. A suite that only ever built
// tarballs would pass on two platforms and fail on the third for a reason that has
// nothing to do with what it is testing.
func archiveOf(files map[string]string) []byte {
	if runtime.GOOS == "windows" {
		return zipOf(files)
	}
	return tarGz(files)
}

// zipOf builds a zip from a name → contents map, marking bin/ entries executable.
func zipOf(files map[string]string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	for name, body := range files {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		if filepath.Dir(name) == "bin" {
			header.SetMode(0o755)
		} else {
			header.SetMode(0o644)
		}

		w, err := zw.CreateHeader(header)
		Expect(err).NotTo(HaveOccurred())
		_, err = w.Write([]byte(body))
		Expect(err).NotTo(HaveOccurred())
	}

	Expect(zw.Close()).To(Succeed())
	return buf.Bytes()
}

// tarGz builds a gzipped tarball from a name → contents map. Any file whose name
// starts with bin/ is marked executable.
func tarGz(files map[string]string) []byte {
	var buf bytes.Buffer

	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	for name, body := range files {
		mode := int64(0o644)
		if filepath.Dir(name) == "bin" {
			mode = 0o755
		}

		Expect(tw.WriteHeader(&tar.Header{
			Name: name, Mode: mode, Size: int64(len(body)), Typeflag: tar.TypeReg,
		})).To(Succeed())
		_, err := tw.Write([]byte(body))
		Expect(err).NotTo(HaveOccurred())
	}

	Expect(tw.Close()).To(Succeed())
	Expect(gz.Close()).To(Succeed())

	return buf.Bytes()
}

// tarGzWithPaxHeader is tarGz with a pax global-header record prepended, the way
// git-archive writes one.
func tarGzWithPaxHeader(files map[string]string) []byte {
	var buf bytes.Buffer

	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	Expect(tw.WriteHeader(&tar.Header{
		Name:     "pax_global_header",
		Typeflag: tar.TypeXGlobalHeader,
		PAXRecords: map[string]string{
			"comment": "0000000000000000000000000000000000000000",
		},
	})).To(Succeed())

	for name, body := range files {
		mode := int64(0o644)
		if filepath.Dir(name) == "bin" {
			mode = 0o755
		}
		Expect(tw.WriteHeader(&tar.Header{
			Name: name, Mode: mode, Size: int64(len(body)), Typeflag: tar.TypeReg,
		})).To(Succeed())
		_, err := tw.Write([]byte(body))
		Expect(err).NotTo(HaveOccurred())
	}

	Expect(tw.Close()).To(Succeed())
	Expect(gz.Close()).To(Succeed())

	return buf.Bytes()
}

const weatherManifest = `apiVersion: 1
name: weather
version: 1.0.0
kind: command
exec: bin/fft-weather
commands:
  - name: weather
    short: Show the forecast
    session: none
`

var _ = Describe("the installer", func() {
	var (
		root string
		hub  *fakeGitHub
		inst *component.Installer
	)

	BeforeEach(func() {
		root = filepath.Join(GinkgoT().TempDir(), "components")
		hub = newFakeGitHub("v1.0.0")
		inst = component.NewInstaller(root, component.WithAPI(hub.server.URL))
	})

	// archive is the well-formed component every spec starts from.
	archive := func() []byte {
		return archiveOf(map[string]string{
			"component.yaml":   weatherManifest,
			"bin/fft-weather":  "#!/bin/sh\n",
			"README.md":        "hello\n",
			"bin/.placeholder": "",
		})
	}

	Describe("a release it can verify", func() {
		BeforeEach(func() {
			hub.publish(assetName("weather", "1.0.0"), archive())
		})

		It("stages without touching the installed tree", func() {
			plan, err := inst.Prepare(context.Background(), component.Source{Repo: "acme/weather", Name: "weather"})
			Expect(err).NotTo(HaveOccurred())
			defer inst.Discard(plan)

			Expect(plan.Manifest.Name).To(Equal("weather"))
			Expect(plan.Digest).NotTo(BeEmpty())
			Expect(plan.Verification()).To(HavePrefix("sha256:"))

			// Staged, not installed: the confirmation has not been asked yet, and this is
			// the whole point of the two-step.
			Expect(filepath.Join(root, "weather")).NotTo(BeADirectory())
		})

		It("installs on commit, and records where it came from", func() {
			plan, err := inst.Prepare(context.Background(), component.Source{Repo: "acme/weather", Name: "weather"})
			Expect(err).NotTo(HaveOccurred())

			installed, err := inst.Commit(plan)
			Expect(err).NotTo(HaveOccurred())

			Expect(installed.Installed).To(BeTrue())
			Expect(installed.Source).To(Equal("github.com/acme/weather@v1.0.0"))

			// The staging directory is gone, not merely unused.
			entries, err := os.ReadDir(root)
			Expect(err).NotTo(HaveOccurred())
			Expect(entries).To(HaveLen(1))
			Expect(entries[0].Name()).To(Equal("weather"))
		})

		It("leaves nothing behind when the plan is discarded", func() {
			plan, err := inst.Prepare(context.Background(), component.Source{Repo: "acme/weather"})
			Expect(err).NotTo(HaveOccurred())

			inst.Discard(plan)

			entries, err := os.ReadDir(root)
			Expect(err).NotTo(HaveOccurred())
			Expect(entries).To(BeEmpty())
		})

		It("refuses an archive that is not the component asked for", func() {
			_, err := inst.Prepare(context.Background(), component.Source{Repo: "acme/weather", Name: "emulator"})
			Expect(err).To(MatchError(ContainSubstring(`asked for the "emulator" component`)))
		})
	})

	Describe("verification", func() {
		It("refuses an archive whose checksum does not match", func() {
			body := archive()
			hub.assets[assetName("weather", "1.0.0")] = body
			hub.checksum(assetName("weather", "1.0.0"), "00000000000000000000000000000000000000000000000000000000000000ff")

			_, err := inst.Prepare(context.Background(), component.Source{Repo: "acme/weather"})
			Expect(err).To(MatchError(ContainSubstring("does not match its checksum")))
			Expect(filepath.Join(root, "weather")).NotTo(BeADirectory())
		})

		// Not installed with a shrug: the checksum is the only thing between "the
		// release you asked for" and "whatever answered", and skipping it when it is
		// inconvenient provides no assurance at all.
		It("refuses a release that publishes no checksums at all", func() {
			hub.assets[assetName("weather", "1.0.0")] = archive()

			_, err := inst.Prepare(context.Background(), component.Source{Repo: "acme/weather"})
			Expect(err).To(MatchError(ContainSubstring("publishes no checksums.txt")))
		})

		It("refuses an archive the checksums file does not list", func() {
			hub.assets[assetName("weather", "1.0.0")] = archive()
			hub.checksum("something-else.tar.gz", "abc")

			_, err := inst.Prepare(context.Background(), component.Source{Repo: "acme/weather"})
			Expect(err).To(MatchError(ContainSubstring("does not list")))
		})

		// git-archive and some tar tools prepend a pax global-header record. It is not a
		// file and carries no path to unpack, so it must be skipped, not rejected as
		// "not a regular file" — which would fail an install of an ordinary tarball.
		//
		// A tar-only concern: a pax header cannot appear in a zip, and Windows release
		// archives are zips (assetName picks the suffix), so the scenario does not exist
		// there — publishing a tarball under a .zip name would only prove unpackZip
		// rejects non-zip bytes, which is a different test.
		It("tolerates a pax global header at the front of the tarball", func() {
			if runtime.GOOS == "windows" {
				Skip("pax global headers are a tar feature; Windows components ship as zip")
			}

			hub.publish(assetName("weather", "1.0.0"), tarGzWithPaxHeader(map[string]string{
				"component.yaml":  weatherManifest,
				"bin/fft-weather": "#!/bin/sh\n",
			}))

			plan, err := inst.Prepare(context.Background(), component.Source{Repo: "acme/weather"})
			Expect(err).NotTo(HaveOccurred())
			defer inst.Discard(plan)

			Expect(plan.Manifest.Name).To(Equal("weather"))
		})

		It("reports a release that is cosign-signed", func() {
			name := assetName("weather", "1.0.0")
			hub.publish(name, archive())
			hub.assets[name+".sig"] = []byte("signature")

			plan, err := inst.Prepare(context.Background(), component.Source{Repo: "acme/weather"})
			Expect(err).NotTo(HaveOccurred())
			defer inst.Discard(plan)

			Expect(plan.Signed).To(BeTrue())
			Expect(plan.Verification()).To(ContainSubstring("cosign verify-blob"))
		})
	})

	Describe("what it will not unpack", func() {
		// An archive is bytes off the internet. The checksum proves it is the one the
		// release names; it proves nothing at all about what is inside it.
		It("refuses an entry that would escape the component directory", func() {
			hub.publish(assetName("weather", "1.0.0"), archiveOf(map[string]string{
				"component.yaml":         weatherManifest,
				"bin/fft-weather":        "#!/bin/sh\n",
				"../../../../etc/passwd": "root::0:0\n",
			}))

			_, err := inst.Prepare(context.Background(), component.Source{Repo: "acme/weather"})
			Expect(err).To(MatchError(ContainSubstring("would be written outside the component")))
		})

		It("refuses an entry with an absolute path", func() {
			hub.publish(assetName("weather", "1.0.0"), archiveOf(map[string]string{
				"component.yaml":  weatherManifest,
				"bin/fft-weather": "#!/bin/sh\n",
				"/etc/passwd":     "root::0:0\n",
			}))

			_, err := inst.Prepare(context.Background(), component.Source{Repo: "acme/weather"})
			Expect(err).To(MatchError(ContainSubstring("not a relative path")))
		})

		It("refuses an archive with no manifest", func() {
			hub.publish(assetName("weather", "1.0.0"), archiveOf(map[string]string{"bin/fft-weather": "x"}))

			_, err := inst.Prepare(context.Background(), component.Source{Repo: "acme/weather"})
			Expect(err).To(MatchError(ContainSubstring("so it is not a component")))
		})

		It("refuses a manifest naming an executable the archive does not carry", func() {
			hub.publish(assetName("weather", "1.0.0"), archiveOf(map[string]string{"component.yaml": weatherManifest}))

			_, err := inst.Prepare(context.Background(), component.Source{Repo: "acme/weather"})
			Expect(err).To(MatchError(ContainSubstring("does not contain it")))
		})
	})

	Describe("upgrading", func() {
		It("replaces the installed version", func() {
			hub.publish(assetName("weather", "1.0.0"), archive())

			plan, err := inst.Prepare(context.Background(), component.Source{Repo: "acme/weather"})
			Expect(err).NotTo(HaveOccurred())
			_, err = inst.Commit(plan)
			Expect(err).NotTo(HaveOccurred())

			next := newFakeGitHub("v2.0.0")
			next.publish(assetName("weather", "2.0.0"), archiveOf(map[string]string{
				"component.yaml":  weatherManifest + "\n",
				"bin/fft-weather": "#!/bin/sh\n# v2\n",
			}))

			upgrade := component.NewInstaller(root, component.WithAPI(next.server.URL))
			plan, err = upgrade.Prepare(context.Background(), component.Source{Repo: "acme/weather"})
			Expect(err).NotTo(HaveOccurred())
			Expect(plan.Replaces).To(Equal("1.0.0"))

			_, err = upgrade.Commit(plan)
			Expect(err).NotTo(HaveOccurred())

			body, err := os.ReadFile(filepath.Join(root, "weather", "bin", "fft-weather"))
			Expect(err).NotTo(HaveOccurred())
			Expect(string(body)).To(ContainSubstring("# v2"))
		})
	})

	Describe("removing", func() {
		It("removes an installed component", func() {
			hub.publish(assetName("weather", "1.0.0"), archive())

			plan, err := inst.Prepare(context.Background(), component.Source{Repo: "acme/weather"})
			Expect(err).NotTo(HaveOccurred())
			_, err = inst.Commit(plan)
			Expect(err).NotTo(HaveOccurred())

			Expect(inst.Remove("weather")).To(Succeed())
			Expect(filepath.Join(root, "weather")).NotTo(BeADirectory())
		})

		// The name comes from a shell, where a typo is one keystroke. fft will not
		// recursively delete a directory it cannot prove it created.
		It("refuses a directory that is not a component", func() {
			Expect(os.MkdirAll(filepath.Join(root, "important"), 0o755)).To(Succeed())

			Expect(inst.Remove("important")).To(MatchError(ContainSubstring("is not an installed component")))
			Expect(filepath.Join(root, "important")).To(BeADirectory())
		})

		It("refuses a name that is not one", func() {
			Expect(inst.Remove("../../etc")).To(MatchError(ContainSubstring("is not a component name")))
		})
	})
})
