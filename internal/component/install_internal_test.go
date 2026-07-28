package component

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("allowedURL", func() {
	i := &Installer{api: "https://api.github.com"}

	DescribeTable("accepts a GitHub host over https",
		func(u string) { Expect(i.allowedURL(u)).To(Succeed()) },
		Entry("the API host", "https://api.github.com/repos/o/r/releases/latest"),
		Entry("a release download", "https://github.com/o/r/releases/download/v1/asset.tar.gz"),
		Entry("codeload", "https://codeload.github.com/o/r/tar.gz/v1"),
		Entry("objects.githubusercontent.com", "https://objects.githubusercontent.com/x"),
		Entry("release-assets.githubusercontent.com", "https://release-assets.githubusercontent.com/x"),
	)

	DescribeTable("refuses anything else",
		func(u string) { Expect(i.allowedURL(u)).NotTo(Succeed()) },
		Entry("http to a GitHub host", "http://github.com/o/r/asset.tar.gz"),
		Entry("a non-GitHub host", "https://evil.example/asset.tar.gz"),
		Entry("a suffix spoof", "https://githubusercontent.com.evil.com/x"),
		Entry("a lookalike host", "https://notgithub.com/o/r/asset"),
		// The configured endpoint is api.github.com over *https*; http to that same
		// host must not be waved through by the endpoint exemption.
		Entry("http to the configured https endpoint", "http://api.github.com/repos/o/r/releases/latest"),
		Entry("http to the configured endpoint, differently cased", "http://API.GitHub.com/x"),
	)

	It("allows the WithAPI endpoint on its own scheme and host, and nothing else", func() {
		// A spec's loopback httptest server serves both the release JSON and the
		// assets over http; the allowlist must let that host through without pinning
		// it to GitHub — but only that host.
		loop := &Installer{api: "http://127.0.0.1:54321"}
		Expect(loop.allowedURL("http://127.0.0.1:54321/assets/weather.tar.gz")).To(Succeed())
		Expect(loop.allowedURL("http://127.0.0.1:9999/assets/x")).NotTo(Succeed())
	})

	It("refuses a followed redirect off the allowed host", func() {
		// The redirect target is a *second* loopback server — resolvable, so a DNS
		// failure cannot masquerade as the refusal — that is not the configured
		// endpoint, so allowedURL denies it. Reverting CheckRedirect would let the
		// client follow it: the target would be reached and get() would succeed.
		var reached atomic.Int64
		blocked := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			reached.Add(1)
		}))
		DeferCleanup(blocked.Close)

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, blocked.URL+"/payload", http.StatusFound)
		}))
		DeferCleanup(srv.Close)

		inst := NewInstaller(GinkgoT().TempDir(), WithAPI(srv.URL))
		_, err := inst.get(context.Background(), srv.URL+"/asset", maxArchive, "")
		Expect(err).To(HaveOccurred())
		Expect(reached.Load()).To(BeZero(), "the redirect target must never be reached")
	})
})

var _ = Describe("isRegularFile", func() {
	It("accepts a real file but refuses a symlink standing in for the executable", func() {
		dir := GinkgoT().TempDir()

		real := filepath.Join(dir, "real")
		Expect(os.WriteFile(real, []byte("#!/bin/sh\n"), 0o755)).To(Succeed())
		Expect(isRegularFile(real)).To(BeTrue())

		link := filepath.Join(dir, "link")
		if err := os.Symlink(real, link); err != nil {
			Skip("symlinks unsupported here: " + err.Error())
		}
		Expect(isRegularFile(link)).To(BeFalse())
	})
})
