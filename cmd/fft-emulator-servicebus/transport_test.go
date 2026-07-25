package main

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestServiceBusTransport(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "cmd/fft-emulator-servicebus")
}

// Only Hello and Plan are specified here: a Send needs an AMQP broker, and the
// protocol seam means the fan-out around it is covered by the emulator's own specs
// with a recording transport.
var _ = Describe("serviceBusTransport", func() {
	transport := newServiceBusTransport("localhost:5672")

	It("announces what it delivers, and where", func() {
		targets, status, err := transport.Hello()

		Expect(err).NotTo(HaveOccurred())
		Expect(targets).To(Equal([]string{targetAzureServiceBus}))

		// The emulator prints this verbatim on startup, which is how the notice can say
		// where a broker is without the emulator knowing anything about brokers.
		Expect(status).To(ContainSubstring("localhost:5672"))
	})

	It("labels a delivery with the namespace and the entity", func() {
		label, err := transport.Plan(map[string]any{
			"type": targetAzureServiceBus, "namespace": "acme", "queueOrTopicName": "orders",
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(label).To(Equal("acme/orders"))
	})

	It("labels a delivery with the entity alone when the target names no namespace", func() {
		label, err := transport.Plan(map[string]any{
			"type": targetAzureServiceBus, "queueOrTopicName": "orders",
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(label).To(Equal("orders"))
	})

	It("refuses a target that names no queue or topic", func() {
		_, err := transport.Plan(map[string]any{"type": targetAzureServiceBus, "namespace": "acme"})
		Expect(err).To(MatchError(ContainSubstring("no queueOrTopicName")))
	})
})
