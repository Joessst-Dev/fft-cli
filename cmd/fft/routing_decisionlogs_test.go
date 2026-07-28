package main

import (
	"fmt"
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

func decisionLog(id, tenantOrderID, orderRef string) string {
	return fmt.Sprintf(
		`{"id":%q,"created":"2026-02-03T08:45:51.525Z","relatedRefs":{"tenantOrderId":%q,"orderRef":%q,"processRef":"p1"}}`,
		id, tenantOrderID, orderRef)
}

func decisionLogPage(items []string, total int) string {
	return fmt.Sprintf(`{"decisionLogs":[%s],"total":%d}`, strings.Join(items, ","), total)
}

var _ = Describe("fft routing decision-logs", func() {
	var c *cli

	BeforeEach(func() { c = newCLI() })

	It("renders the logs, keyed by the refs a run belonged to", func() {
		c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, decisionLogPage([]string{
				decisionLog("l1", "R456", "o-uuid"),
			}, 1))
		})

		Expect(c.run("routing", "decision-logs")).To(Equal(exitcode.OK))
		Expect(c.out()).To(ContainSubstring("R456"))
		Expect(c.out()).To(ContainSubstring("o-uuid"))
		Expect(c.out()).To(ContainSubstring("p1"))
	})

	It("reads the logs out of the decisionLogs envelope", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, decisionLogPage(nil, 0))
		})

		Expect(c.run("routing", "decision-logs")).To(Equal(exitcode.OK))
		Expect(api.only().Path).To(Equal("/api/routing/decisionlogs"))
	})

	It("passes the filters through as exact-match query params", func() {
		api := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, decisionLogPage(nil, 0))
		})

		Expect(c.run("routing", "decision-logs",
			"--order", "o-1", "--routing-plan", "rp-1", "--process", "pr-1",
			"--tenant-order-id", "R9", "--sourcing-option", "so-1", "--sourcing-options", "sos-1",
		)).To(Equal(exitcode.OK))

		q := api.only().Query
		Expect(q.Get("orderRef")).To(Equal("o-1"))
		Expect(q.Get("routingPlanRef")).To(Equal("rp-1"))
		Expect(q.Get("processRef")).To(Equal("pr-1"))
		Expect(q.Get("tenantOrderId")).To(Equal("R9"))
		Expect(q.Get("sourcingOptionRef")).To(Equal("so-1"))
		Expect(q.Get("sourcingOptionsRef")).To(Equal("sos-1"))
	})

	It("leaves stdout empty and says so on stderr when there are no logs", func() {
		c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(w, http.StatusOK, decisionLogPage(nil, 0))
		})

		Expect(c.run("routing", "decision-logs")).To(Equal(exitcode.OK))
		Expect(c.out()).To(BeEmpty())
		Expect(c.errOut()).To(ContainSubstring("No decision logs"))
	})
})
