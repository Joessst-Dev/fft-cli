package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestEmulator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "cmd/fft-emulator")
}

var _ = Describe("emit", func() {
	// server stands in for a running emulator's /_emulator/emit endpoint, answering
	// with the given JSON body.
	server := func(body string) string {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer GinkgoRecover()
			Expect(r.URL.Path).To(Equal("/_emulator/emit"))
			_, _ = w.Write([]byte(body))
		}))
		DeferCleanup(srv.Close)
		return srv.URL
	}

	It("reports where a published event went", func() {
		url := server(`{"enabled":true,"published":1,"targets":["p/orders"]}`)

		var out bytes.Buffer
		Expect(emitEvent(context.Background(), &out, url, []byte(`{"event":"ORDER_CREATED"}`))).To(Succeed())
		Expect(out.String()).To(ContainSubstring("published 1 to p/orders"))
	})

	It("names the missing transport when nothing is configured", func() {
		url := server(`{"enabled":false,"published":0,"targets":[]}`)

		var out bytes.Buffer
		Expect(emitEvent(context.Background(), &out, url, []byte(`{}`))).To(Succeed())
		Expect(out.String()).To(ContainSubstring("no delivery transport configured"))
	})

	It("points at a subscription when eventing is on but nothing matched", func() {
		url := server(`{"enabled":true,"published":0,"targets":[]}`)

		var out bytes.Buffer
		Expect(emitEvent(context.Background(), &out, url, []byte(`{}`))).To(Succeed())
		Expect(out.String()).To(ContainSubstring("nothing matched"))
	})

	It("reads the older topics field an emulator predating the other targets answers with", func() {
		url := server(`{"enabled":true,"published":1,"topics":["p/orders"]}`)

		var out bytes.Buffer
		Expect(emitEvent(context.Background(), &out, url, []byte(`{}`))).To(Succeed())
		Expect(out.String()).To(ContainSubstring("published 1 to p/orders"))
	})

	It("surfaces a non-200 as an error", func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		}))
		DeferCleanup(srv.Close)

		var out bytes.Buffer
		Expect(emitEvent(context.Background(), &out, srv.URL, []byte(`{}`))).To(MatchError(ContainSubstring("500")))
	})
})

var _ = Describe("readBody", func() {
	It("rejects a file that is not valid JSON", func() {
		_, err := readBody(bytes.NewReader(nil), "-")
		Expect(err).To(MatchError(ContainSubstring("does not contain valid JSON")))
	})

	It("reads a JSON body from stdin", func() {
		got, err := readBody(bytes.NewReader([]byte(`{"a":1}`)), "-")
		Expect(err).NotTo(HaveOccurred())
		Expect(string(got)).To(Equal(`{"a":1}`))
	})
})
