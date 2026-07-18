package emulator

// These specs drive the *real* client against a *real* emulator over a real socket.
// The unit tests prove the store and the paginators in isolation; this proves the
// thing that actually matters — that the envelopes the emulator writes are the ones
// the client decodes. A wrong items-key or a cursor that does not advance passes
// every unit test and fails here.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/auth"
	"github.com/Joessst-Dev/fft-cli/internal/client"
)

var _ = Describe("driving the real client against the emulator", func() {
	var (
		c       *client.Client
		baseURL string
		ctx     context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()

		ln, err := net.Listen("tcp", "127.0.0.1:0")
		Expect(err).NotTo(HaveOccurred())

		srv, err := New(Config{})
		Expect(err).NotTo(HaveOccurred())

		go func() { _ = srv.app.Listener(ln) }()
		DeferCleanup(func() { _ = srv.app.Shutdown() })

		baseURL = "http://" + ln.Addr().String()
		waitReady(baseURL)

		c, err = client.New(baseURL, client.WithTokenSource(auth.StaticTokenSource("emulator")))
		Expect(err).NotTo(HaveOccurred())
	})

	It("walks a cursor search across pages", func() {
		for range 5 {
			createOrder(baseURL, map[string]any{"tenantOrderId": "t"})
		}

		// Size 2 forces three pages (2, 2, 1); SearchAll must follow the cursor to the
		// end without looping.
		size := 2
		payload := client.OrderSearchPayload{Size: &size}

		var got []json.RawMessage
		for item, err := range client.SearchAll(ctx, c, client.OrderSearch[json.RawMessage](), payload) {
			Expect(err).NotTo(HaveOccurred())
			got = append(got, item)
		}
		Expect(got).To(HaveLen(5))
	})

	It("walks a startAfterId list across pages, terminating on the total", func() {
		for range 5 {
			createOrder(baseURL, map[string]any{"tenantOrderId": "t"})
		}

		var listed []json.RawMessage
		for item, err := range client.ListAll(ctx, c, client.Orders("", ""), 2) {
			Expect(err).NotTo(HaveOccurred())
			listed = append(listed, item)
		}
		Expect(listed).To(HaveLen(5))
	})

	It("returns an empty page, not an error, for a collection with nothing in it", func() {
		var got []json.RawMessage
		for item, err := range client.SearchAll(ctx, c, client.OrderSearch[json.RawMessage](), client.OrderSearchPayload{}) {
			Expect(err).NotTo(HaveOccurred())
			got = append(got, item)
		}
		Expect(got).To(BeEmpty())
	})

	It("answers a stale update with the 409 the client decodes into a conflict", func() {
		created := createOrder(baseURL, map[string]any{"tenantOrderId": "t"})
		id, _ := created["id"].(string)

		// A PATCH carrying the wrong version — orders update by PATCH. The store holds
		// version 1.
		status, body := patchJSON(baseURL, "/api/orders/"+id, map[string]any{"version": 99})
		Expect(status).To(Equal(http.StatusConflict))

		err := client.Check(status, body)
		var apiErr *client.APIError
		Expect(errors.As(err, &apiErr)).To(BeTrue())

		sent, current, ok := apiErr.Conflict()
		Expect(ok).To(BeTrue(), "the 409 envelope must carry requestVersion and version")
		Expect(sent).To(Equal(int64(99)))
		Expect(current).To(Equal(int64(1)))
	})

	It("shapes an error as the JSON array the client decodes, not an object", func() {
		// An object envelope decodes into APIError.Errors as empty and the CLI prints
		// nothing — so the array shape is the contract, even for a plain 404.
		status, body := getJSON(baseURL, "/api/orders/does-not-exist")
		Expect(status).To(Equal(http.StatusNotFound))

		var arr []map[string]any
		Expect(json.Unmarshal(body, &arr)).To(Succeed(), "the error body must be a JSON array: %s", body)
		Expect(arr).NotTo(BeEmpty())
		Expect(arr[0]).To(HaveKey("summary"))
	})

	It("reflects a create in a subsequent get", func() {
		created := createOrder(baseURL, map[string]any{"tenantOrderId": "abc"})
		id, _ := created["id"].(string)

		status, body := getJSON(baseURL, "/api/orders/"+id)
		Expect(status).To(Equal(http.StatusOK))

		var got map[string]any
		Expect(json.Unmarshal(body, &got)).To(Succeed())
		Expect(got).To(HaveKeyWithValue("tenantOrderId", "abc"))
		Expect(got).To(HaveKeyWithValue("id", id))
	})
})

// waitReady blocks until the emulator answers, so a spec never races the listener.
func waitReady(baseURL string) {
	Eventually(func() error {
		resp, err := http.Get(baseURL + "/api/orders")
		if err != nil {
			return err
		}
		return resp.Body.Close()
	}, 2*time.Second, 10*time.Millisecond).Should(Succeed())
}

func createOrder(baseURL string, body map[string]any) map[string]any {
	GinkgoHelper()
	status, raw := postJSON(baseURL, "/api/orders", body)
	Expect(status).To(Equal(http.StatusCreated))

	var out map[string]any
	Expect(json.Unmarshal(raw, &out)).To(Succeed())
	return out
}

func postJSON(baseURL, path string, body map[string]any) (int, []byte) {
	return sendJSON(http.MethodPost, baseURL+path, body)
}

func patchJSON(baseURL, path string, body map[string]any) (int, []byte) {
	return sendJSON(http.MethodPatch, baseURL+path, body)
}

func getJSON(baseURL, path string) (int, []byte) {
	return sendJSON(http.MethodGet, baseURL+path, nil)
}

func sendJSON(method, url string, body map[string]any) (int, []byte) {
	GinkgoHelper()

	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		Expect(err).NotTo(HaveOccurred())
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}

	req, err := http.NewRequest(method, url, reader)
	Expect(err).NotTo(HaveOccurred())
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	Expect(err).NotTo(HaveOccurred())
	defer func() { _ = resp.Body.Close() }()

	out := &bytes.Buffer{}
	_, err = out.ReadFrom(resp.Body)
	Expect(err).NotTo(HaveOccurred())
	return resp.StatusCode, out.Bytes()
}
