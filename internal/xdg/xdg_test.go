package xdg_test

import (
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/xdg"
)

var _ = Describe("Base", func() {
	var home string

	BeforeEach(func() {
		home = GinkgoT().TempDir()
		GinkgoT().Setenv("HOME", home)
		GinkgoT().Setenv("USERPROFILE", home)
	})

	Describe("Path", func() {
		It("puts the file under the variable's directory when it is set", func() {
			GinkgoT().Setenv("XDG_STATE_HOME", "/state")

			Expect(xdg.State().Path("history.jsonl")).To(Equal(filepath.Join("/state", "fft", "history.jsonl")))
		})

		DescribeTable("falls back to the home directory when the variable is empty",
			func(base xdg.Base, env string, want ...string) {
				GinkgoT().Setenv(env, "")

				Expect(base.Path("f")).To(Equal(filepath.Join(append([]string{home}, want...)...)))
			},
			Entry("config", xdg.Config(), "XDG_CONFIG_HOME", ".config", "fft", "f"),
			Entry("state", xdg.State(), "XDG_STATE_HOME", ".local", "state", "fft", "f"),
			Entry("data", xdg.Data(), "XDG_DATA_HOME", ".local", "share", "fft", "f"),
			Entry("cache", xdg.Cache(), "XDG_CACHE_HOME", ".cache", "fft", "f"),
		)
	})

	Describe("LookupPath", func() {
		lookup := func(v string) func(string) (string, bool) {
			return func(key string) (string, bool) {
				if key == "XDG_DATA_HOME" {
					return v, true
				}
				return "", false
			}
		}

		It("trims the variable before using it", func() {
			Expect(xdg.Data().LookupPath(lookup("  /data  "), "templates")).
				To(Equal(filepath.Join("/data", "fft", "templates")))
		})

		It("treats a whitespace-only variable as unset", func() {
			Expect(xdg.Data().LookupPath(lookup("   "), "templates")).
				To(Equal(filepath.Join(home, ".local", "share", "fft", "templates")))
		})
	})
})
