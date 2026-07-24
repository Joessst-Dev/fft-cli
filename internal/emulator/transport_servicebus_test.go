package emulator

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Only plan is specified here: a send needs an AMQP broker, and the transport seam means
// the fan-out around it is covered by the emitter specs with a recording transport.
var _ = Describe("serviceBusTransport", func() {
	transport := newServiceBusTransport("localhost:5672")

	It("labels a delivery with the namespace and the entity", func() {
		d, err := transport.plan(map[string]any{
			"type": targetAzureServiceBus, "namespace": "acme", "queueOrTopicName": "orders",
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(d.label).To(Equal("acme/orders"))
	})

	It("labels a delivery with the entity alone when the target names no namespace", func() {
		d, err := transport.plan(map[string]any{
			"type": targetAzureServiceBus, "queueOrTopicName": "orders",
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(d.label).To(Equal("orders"))
	})

	It("refuses a target that names no queue or topic", func() {
		_, err := transport.plan(map[string]any{"type": targetAzureServiceBus, "namespace": "acme"})
		Expect(err).To(MatchError(ContainSubstring("no queueOrTopicName")))
	})
})
