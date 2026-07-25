package emulator

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Server.Eventing", func() {
	// targetsOf reads the target types out of the startup notice, in order.
	targetsOf := func(statuses []TargetStatus) []string {
		out := make([]string, 0, len(statuses))
		for _, s := range statuses {
			out = append(out, s.Target)
		}
		return out
	}

	It("reports all three API-defined targets, live or not", func() {
		srv, err := New(Config{transports: map[string]transport{targetWebhook: recordingWebhook()}})
		Expect(err).NotTo(HaveOccurred())

		targets := targetsOf(srv.Eventing())

		// Every one, including the brokers nothing is delivering here: a subscription
		// that silently never fires is the failure the notice exists to prevent, so an
		// unavailable target is listed, not omitted.
		Expect(targets).To(ContainElements(targetWebhook, targetGoogleCloudPubSub, targetAzureServiceBus))
	})

	It("reports a third-party target the built-in three do not include", func() {
		// The whole point of the transport protocol is a broker fft has never heard of.
		// A notice that only knew the three built-in types would hide exactly that
		// broker — the same silent failure, one the community can hit.
		srv, err := New(Config{transports: map[string]transport{
			targetWebhook: recordingWebhook(),
			"KAFKA":       recordingPubSub(),
		}})
		Expect(err).NotTo(HaveOccurred())

		status := srv.Eventing()
		Expect(targetsOf(status)).To(ContainElement("KAFKA"))

		for _, s := range status {
			if s.Target == "KAFKA" {
				Expect(s.Live).To(BeTrue())
			}
		}
	})
})
