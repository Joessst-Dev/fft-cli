package main

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestPubSubTransport(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "cmd/fft-emulator-pubsub")
}

// Only Hello and Plan are specified here: a Send needs a Pub/Sub emulator to dial, and
// the protocol seam means the fan-out around it is covered by the emulator's own specs
// with a recording transport.
var _ = Describe("pubSubTransport", func() {
	transport := newPubSubTransport("localhost:8085")

	It("announces what it delivers, and where", func() {
		targets, status, err := transport.Hello()

		Expect(err).NotTo(HaveOccurred())
		Expect(targets).To(Equal([]string{targetGoogleCloudPubSub}))

		// The emulator prints this verbatim on startup, which is how the notice says
		// where a broker is without the emulator knowing anything about brokers.
		Expect(status).To(ContainSubstring("localhost:8085"))
	})

	It("labels a delivery with the project and the topic", func() {
		label, err := transport.Plan(map[string]any{
			"type": targetGoogleCloudPubSub, "projectId": "acme", "topicId": "orders",
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(label).To(Equal("acme/orders"))
	})

	It("refuses a target that names no project and topic", func() {
		_, err := transport.Plan(map[string]any{"type": targetGoogleCloudPubSub, "projectId": "acme"})
		Expect(err).To(MatchError(ContainSubstring("no projectId and topicId")))
	})
})
