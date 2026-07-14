package client_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/client"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

// connections is the ListOp under test. The facility is a path parameter and the
// target a query filter, which is the shape every GET list has: everything but the
// paging pair is closed over.
func connections(facility, target string) client.ListOp[json.RawMessage] {
	return client.FacilityConnections(facility, target)
}

// page renders the envelope these endpoints answer with: the named array and a
// total, and nothing that says whether there is more.
func page(ids []string, total int) string {
	items := make([]string, 0, len(ids))
	for _, id := range ids {
		items = append(items, fmt.Sprintf(`{"id":%q,"sourceFacilityRef":"BER-01"}`, id))
	}
	return fmt.Sprintf(`{"interFacilityConnections":[%s],"total":%d}`,
		strings.Join(items, ","), total)
}

// ids is what the specs assert on: the entities are opaque here, and only their
// order and their identity matter to the paginator.
func ids(items []json.RawMessage) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		id, err := client.RawID(item)
		Expect(err).NotTo(HaveOccurred())
		out = append(out, id)
	}
	return out
}

// collect drains ListAll, separating the entities from the error that ended it.
//
// It reads each id leniently — an entity without one is exactly what the "no id to
// page from" spec serves, and a helper that insisted on one would fail that spec
// itself, before the paginator ever got the chance to.
func collect(seq func(func(json.RawMessage, error) bool)) ([]string, error) {
	var (
		got []string
		bad error
	)
	for item, err := range seq {
		if err != nil {
			bad = err
			break
		}
		id, _ := client.RawID(item)
		got = append(got, id)
	}
	return got, bad
}

var _ = Describe("listing by startAfterId", func() {
	var ctx context.Context

	BeforeEach(func() { ctx = context.Background() })

	Describe("one page", func() {
		It("asks for no cursor on the first page", func() {
			t := newTenant(func(w http.ResponseWriter, _ *http.Request, _ int) {
				fmt.Fprint(w, page([]string{"c1", "c2"}, 2))
			})

			got, err := client.List(ctx, unauthenticated(t), connections("BER-01", ""), "", 0)

			Expect(err).NotTo(HaveOccurred())
			Expect(ids(got.Items)).To(Equal([]string{"c1", "c2"}))
			Expect(t.asked(0)).NotTo(HaveKey("startAfterId"))
			Expect(t.asked(0)).NotTo(HaveKey("size"))
		})

		It("reads the total, which this envelope always carries", func() {
			t := newTenant(func(w http.ResponseWriter, _ *http.Request, _ int) {
				fmt.Fprint(w, page([]string{"c1"}, 47))
			})

			got, err := client.List(ctx, unauthenticated(t), connections("BER-01", ""), "", 0)

			Expect(err).NotTo(HaveOccurred())
			Expect(got.Total).To(Equal(47))
		})

		It("sends the filter as a query parameter", func() {
			t := newTenant(func(w http.ResponseWriter, _ *http.Request, _ int) {
				fmt.Fprint(w, page(nil, 0))
			})

			_, err := client.List(ctx, unauthenticated(t), connections("BER-01", "fra-uuid"), "", 25)

			Expect(err).NotTo(HaveOccurred())
			Expect(t.asked(0).Get("targetFacilityRef")).To(Equal("fra-uuid"))
			Expect(t.asked(0).Get("size")).To(Equal("25"))
		})

		// The API's answer to a size it dislikes is an opaque 400, so fft refuses it
		// first, with a sentence — the same bargain the cursor searches strike.
		It("refuses a size the API would reject, before sending it", func() {
			t := newTenant(func(w http.ResponseWriter, _ *http.Request, _ int) {
				fmt.Fprint(w, page(nil, 0))
			})

			_, err := client.List(ctx, unauthenticated(t), connections("BER-01", ""), "", 500)

			Expect(err).To(HaveOccurred())
			Expect(errors.As(err, &exitcode.UsageError{})).To(BeTrue())
			Expect(t.hits()).To(BeZero(), "nothing should have been sent")
		})

		It("says so when the envelope has no array under the name it expects", func() {
			t := newTenant(func(w http.ResponseWriter, _ *http.Request, _ int) {
				fmt.Fprint(w, `{"somethingElse":[],"total":0}`)
			})

			_, err := client.List(ctx, unauthenticated(t), connections("BER-01", ""), "", 0)

			Expect(err).To(MatchError(ContainSubstring(`no "interFacilityConnections" field`)))
		})
	})

	Describe("every page", func() {
		It("starts the next page after the last id of this one", func() {
			t := newTenant(func(w http.ResponseWriter, _ *http.Request, n int) {
				switch n {
				case 1:
					fmt.Fprint(w, page([]string{"c1", "c2"}, 3))
				default:
					fmt.Fprint(w, page([]string{"c3"}, 3))
				}
			})

			got, err := collect(client.ListAll(ctx, unauthenticated(t), connections("BER-01", ""), 2))

			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal([]string{"c1", "c2", "c3"}))
			Expect(t.hits()).To(Equal(2))
			Expect(t.asked(0)).NotTo(HaveKey("startAfterId"))
			Expect(t.asked(1).Get("startAfterId")).To(Equal("c2"))
		})

		// A short page is the only end-of-list signal this envelope has: no
		// hasNextPage, no cursor. So a *full* last page always costs one more request,
		// and that request is what proves there was nothing left.
		It("stops on a short page without asking again", func() {
			t := newTenant(func(w http.ResponseWriter, _ *http.Request, _ int) {
				fmt.Fprint(w, page([]string{"c1"}, 1))
			})

			got, err := collect(client.ListAll(ctx, unauthenticated(t), connections("BER-01", ""), 2))

			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal([]string{"c1"}))
			Expect(t.hits()).To(Equal(1))
		})

		// The spec puts no maximum on this endpoint's size, so a server is free to cap
		// it — to answer 2 to a request for 10. "A short page is the last page" would
		// read that first, capped page as the whole list: two connections, exit 0, no
		// truncation warning. The envelope's own total is what catches it.
		It("keeps paging when the server quietly caps the page size", func() {
			t := newTenant(func(w http.ResponseWriter, r *http.Request, _ int) {
				// Asked for 10, always answers at most 2 — and says there are 3.
				if r.URL.Query().Get("startAfterId") == "c2" {
					fmt.Fprint(w, page([]string{"c3"}, 3))
					return
				}
				fmt.Fprint(w, page([]string{"c1", "c2"}, 3))
			})

			got, err := collect(client.ListAll(ctx, unauthenticated(t), connections("BER-01", ""), 10))

			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal([]string{"c1", "c2", "c3"}), "the capped page was read as the end of the list")
		})

		It("asks once more after a full page, and stops when it comes back empty", func() {
			t := newTenant(func(w http.ResponseWriter, _ *http.Request, n int) {
				if n == 1 {
					fmt.Fprint(w, page([]string{"c1", "c2"}, 2))
					return
				}
				fmt.Fprint(w, page(nil, 2))
			})

			got, err := collect(client.ListAll(ctx, unauthenticated(t), connections("BER-01", ""), 2))

			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal([]string{"c1", "c2"}))
			Expect(t.hits()).To(Equal(2))
		})

		// Truncation is not a failure: every entity already yielded is real. It ends
		// the iteration and says so, because a truncated list that keeps quiet is a
		// wrong answer that looks like a right one.
		It("stops at --max-items and yields a TruncatedError saying so", func() {
			t := newTenant(func(w http.ResponseWriter, _ *http.Request, _ int) {
				fmt.Fprint(w, page([]string{"c1", "c2"}, 99))
			})

			got, err := collect(client.ListAll(ctx, unauthenticated(t), connections("BER-01", ""), 2, client.MaxItems(3)))

			Expect(got).To(Equal([]string{"c1", "c2", "c1"}))

			var truncated *client.TruncatedError
			Expect(errors.As(err, &truncated)).To(BeTrue())
			Expect(truncated.MaxItems).To(Equal(3))
			Expect(truncated.Hint()).To(ContainSubstring("--max-items"))
		})

		// An id that does not advance would ask for the same page until the user gives
		// up. A loop fft can see is a loop fft refuses to enter.
		It("refuses to page forever when the last id does not advance", func() {
			t := newTenant(func(w http.ResponseWriter, _ *http.Request, _ int) {
				// Always the same two ids, so the cursor never moves past "c2".
				fmt.Fprint(w, page([]string{"c1", "c2"}, 99))
			})

			_, err := collect(client.ListAll(ctx, unauthenticated(t), connections("BER-01", ""), 2, client.MaxItems(100)))

			Expect(err).To(MatchError(ContainSubstring("no way forward")))
			Expect(t.hits()).To(Equal(2))
		})

		// The cursor is the entity's own id, so an entity with no id is a list that
		// cannot be paged. Saying so beats guessing.
		It("says so when an entity has no id to page from", func() {
			t := newTenant(func(w http.ResponseWriter, _ *http.Request, _ int) {
				fmt.Fprint(w, `{"interFacilityConnections":[{"sourceFacilityRef":"BER-01"},{"sourceFacilityRef":"BER-01"}],"total":9}`)
			})

			_, err := collect(client.ListAll(ctx, unauthenticated(t), connections("BER-01", ""), 2))

			Expect(err).To(MatchError(ContainSubstring("no id")))
		})
	})
})
