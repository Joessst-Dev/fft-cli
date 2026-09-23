package config_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/config"
)

var _ = Describe("the emulator's environment", func() {
	It("is the four variables of the headless id-token recipe, in the order it is shown", func() {
		env := config.EmulatorEnv(config.EmulatorBaseURL(8080))

		names := make([]string, 0, len(env))
		for _, v := range env {
			names = append(names, v.Name+"="+v.Value)
		}
		Expect(names).To(Equal([]string{
			"FFT_BASE_URL=http://localhost:8080",
			"FFT_FIREBASE_API_KEY=emulator",
			"FFT_EMAIL=dev@localhost",
			"FFT_ID_TOKEN=emulator-token",
		}))
	})

	It("synthesizes the ephemeral project, which is how a run reaches the emulator at all", func() {
		env := config.EmulatorEnv(config.EmulatorBaseURL(9999))
		lookup := func(name string) (string, bool) {
			for _, v := range env {
				if v.Name == name {
					return v.Value, true
				}
			}
			return "", false
		}

		p, ok, err := config.FromEnv(lookup)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeTrue(), "the recipe is not a complete headless set")
		Expect(p.Name).To(Equal(config.EphemeralName))
		Expect(p.Ephemeral).To(BeTrue())
		// Plain http survives NormalizeBaseURL only because the host is loopback; a
		// bearer token in the clear to anywhere else is refused.
		Expect(p.BaseURL).To(Equal("http://localhost:9999"))
	})
})
