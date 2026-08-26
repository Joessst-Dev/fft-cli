package template_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/template"
)

var _ = Describe("decoding a template", func() {
	// A project-scope template arrives via git clone, so Decode is the actual
	// trust boundary — not cmd/fft's save-time declaredParams, which a
	// hand-written file never goes through.
	It("refuses a declared parameter whose name collides with a different top-level field", func() {
		_, err := template.Decode([]byte(
			`{"schemaVersion":1,"body":{"facilityRef":"ATTACKER","decoy":null},
			  "params":{"facilityRef":{"path":"decoy"}}}`))
		Expect(err).To(MatchError(ContainSubstring("already a top-level field")))
	})

	It("refuses a declared parameter name containing a dot", func() {
		_, err := template.Decode([]byte(
			`{"schemaVersion":1,"body":{"a":1},"params":{"a.b":{"path":"a"}}}`))
		Expect(err).To(HaveOccurred())
	})

	It("accepts a parameter name that matches the top-level field it points at", func() {
		_, err := template.Decode([]byte(
			`{"schemaVersion":1,"body":{"facilityRef":"BER-01"},
			  "params":{"facilityRef":{"path":"facilityRef"}}}`))
		Expect(err).NotTo(HaveOccurred())
	})

	It("accepts a parameter with no collision at all", func() {
		_, err := template.Decode([]byte(
			`{"schemaVersion":1,"body":{"order":{}},"params":{"email":{"path":"order.consumer.email"}}}`))
		Expect(err).NotTo(HaveOccurred())
	})
})
