package client_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/client"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

const apiTooComplex = `[{"summary":"Query too complex - please simplify your search criteria or reduce the result set size"}]`

var _ = Describe("the API's 'Query too complex' 400", func() {
	var ctx context.Context

	BeforeEach(func() { ctx = context.Background() })

	refuse := func(status int, body string) *tenant {
		return newTenant(func(w http.ResponseWriter, _ *http.Request, _ int) {
			w.WriteHeader(status)
			fmt.Fprint(w, body)
		})
	}

	Describe("one page", func() {
		It("is a QueryTooComplexError that keeps the API's words and the 400's exit code", func() {
			t := refuse(http.StatusBadRequest, apiTooComplex)

			_, err := client.Search(ctx, unauthenticated(t), facilities(), client.FacilitySearchPayload{})

			var tooComplex *client.QueryTooComplexError
			Expect(errors.As(err, &tooComplex)).To(BeTrue())
			Expect(tooComplex.Walk).To(BeFalse())
			Expect(err).To(MatchError(ContainSubstring("the API returned HTTP 400: Query too complex")))
			Expect(tooComplex.Hint()).To(ContainSubstring("A smaller --size will not help"))

			var apiErr *client.APIError
			Expect(errors.As(err, &apiErr)).To(BeTrue(), "the API's own error stays reachable")
			Expect(exitcode.FromError(err)).To(Equal(exitcode.General))
		})

		It("matches the message whatever its case", func() {
			t := refuse(http.StatusBadRequest, `[{"summary":"QUERY TOO COMPLEX"}]`)

			_, err := client.Search(ctx, unauthenticated(t), facilities(), client.FacilitySearchPayload{})

			var tooComplex *client.QueryTooComplexError
			Expect(errors.As(err, &tooComplex)).To(BeTrue())
		})

		// The same 400 is what an unknown enum value gets: only the message says which.
		DescribeTable("leaves any other refusal alone",
			func(status int, body string) {
				t := refuse(status, body)

				_, err := client.Search(ctx, unauthenticated(t), facilities(), client.FacilitySearchPayload{})

				Expect(err).To(HaveOccurred())
				var tooComplex *client.QueryTooComplexError
				Expect(errors.As(err, &tooComplex)).To(BeFalse())
			},
			Entry("another 400", http.StatusBadRequest, `[{"summary":"status must be one of OPEN, CLOSED"}]`),
			Entry("a 400 with no envelope", http.StatusBadRequest, `not json`),
			Entry("a 400 with an empty envelope", http.StatusBadRequest, `[]`),
			Entry("the same words on another status", http.StatusForbidden, apiTooComplex),
		)
	})

	Describe("an --all walk", func() {
		It("reports how far it got when a later page is refused", func() {
			t := newTenant(func(w http.ResponseWriter, _ *http.Request, n int) {
				if n == 1 {
					fmt.Fprint(w, `{"facilities":[{"name":"A"},{"name":"B"}],"pageInfo":{"hasNextPage":true,"endCursor":"cursor-1"}}`)
					return
				}
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, apiTooComplex)
			})

			var (
				items int
				last  error
			)
			for _, err := range client.SearchAll(ctx, unauthenticated(t), facilities(), client.FacilitySearchPayload{}) {
				if err != nil {
					last = err
					break
				}
				items++
			}

			Expect(items).To(Equal(2))
			var tooComplex *client.QueryTooComplexError
			Expect(errors.As(last, &tooComplex)).To(BeTrue())
			Expect(tooComplex.Walk).To(BeTrue())
			Expect(tooComplex.Pages).To(Equal(1))
			Expect(tooComplex.Items).To(Equal(2))
			Expect(last).To(MatchError(ContainSubstring("after 1 page (2 items) of an --all walk")))
			Expect(last).To(MatchError(ContainSubstring("Query too complex")))
			Expect(exitcode.FromError(last)).To(Equal(exitcode.General))
		})

		It("stays a plain refusal when the first page is refused: nothing was walked yet", func() {
			t := refuse(http.StatusBadRequest, apiTooComplex)

			var last error
			for _, err := range client.SearchAll(ctx, unauthenticated(t), facilities(), client.FacilitySearchPayload{}) {
				last = err
			}

			var tooComplex *client.QueryTooComplexError
			Expect(errors.As(last, &tooComplex)).To(BeTrue())
			Expect(tooComplex.Walk).To(BeFalse())
			Expect(last).NotTo(MatchError(ContainSubstring("--all walk")))
		})
	})

	Describe("ExplainQueryTooComplex", func() {
		It("passes nil and foreign errors through untouched", func() {
			Expect(client.ExplainQueryTooComplex("op", nil)).To(BeNil())

			foreign := errors.New("boom")
			Expect(client.ExplainQueryTooComplex("op", foreign)).To(BeIdenticalTo(foreign))
		})

		It("does not wrap an error it already explained", func() {
			t := refuse(http.StatusBadRequest, apiTooComplex)
			_, err := client.Search(ctx, unauthenticated(t), facilities(), client.FacilitySearchPayload{})

			Expect(client.ExplainQueryTooComplex("op", err)).To(BeIdenticalTo(err))
		})
	})
})
