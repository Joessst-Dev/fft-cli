package transportproto_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/pkg/transportproto"
)

func TestTransportProto(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "pkg/transportproto")
}

// handler is a transport that records what it was asked and answers as told.
type handler struct {
	targets []string
	status  string

	planErr error
	sendErr error

	sent []sent
}

type sent struct {
	target map[string]any
	event  string
	data   string
}

func (h *handler) Hello() ([]string, string, error) { return h.targets, h.status, nil }

func (h *handler) Plan(target map[string]any) (string, error) {
	if h.planErr != nil {
		return "", h.planErr
	}
	name, _ := target["topicId"].(string)
	return "topic/" + name, nil
}

func (h *handler) Send(_ context.Context, target map[string]any, event string, data []byte) error {
	if h.sendErr != nil {
		return h.sendErr
	}
	h.sent = append(h.sent, sent{target: target, event: event, data: string(data)})
	return nil
}

var _ = Describe("Serve", func() {
	var h *handler

	BeforeEach(func() {
		h = &handler{targets: []string{"GOOGLE_CLOUD_PUB_SUB"}, status: "publishing to localhost:8085"}
	})

	// serve runs the requests through a handler and returns the responses, in order.
	serve := func(requests ...transportproto.Request) []transportproto.Response {
		GinkgoHelper()

		var in strings.Builder
		enc := json.NewEncoder(&in)
		for _, req := range requests {
			Expect(enc.Encode(req)).To(Succeed())
		}

		var out strings.Builder
		Expect(transportproto.Serve(context.Background(), strings.NewReader(in.String()), &out, h)).To(Succeed())

		var got []transportproto.Response
		dec := json.NewDecoder(strings.NewReader(out.String()))
		for dec.More() {
			var res transportproto.Response
			Expect(dec.Decode(&res)).To(Succeed())
			got = append(got, res)
		}
		return got
	}

	It("answers a hello with what the transport delivers", func() {
		got := serve(transportproto.Request{ID: 1, Op: transportproto.OpHello})

		Expect(got).To(HaveLen(1))
		Expect(got[0].ID).To(Equal(1))
		Expect(got[0].OK).To(BeTrue())
		Expect(got[0].Targets).To(Equal([]string{"GOOGLE_CLOUD_PUB_SUB"}))
		Expect(got[0].Status).To(Equal("publishing to localhost:8085"))
	})

	It("answers a plan with the label deliveries are reported under", func() {
		got := serve(transportproto.Request{
			ID: 7, Op: transportproto.OpPlan, Target: map[string]any{"topicId": "orders"},
		})

		Expect(got[0].ID).To(Equal(7))
		Expect(got[0].Label).To(Equal("topic/orders"))
	})

	It("delivers a send to the handler with the event and the envelope", func() {
		serve(transportproto.Request{
			ID:     2,
			Op:     transportproto.OpSend,
			Target: map[string]any{"topicId": "orders"},
			Event:  "ORDER_CREATED",
			Data:   json.RawMessage(`{"eventId":"abc"}`),
		})

		Expect(h.sent).To(HaveLen(1))
		Expect(h.sent[0].event).To(Equal("ORDER_CREATED"))
		Expect(h.sent[0].data).To(Equal(`{"eventId":"abc"}`))
		Expect(h.sent[0].target).To(HaveKeyWithValue("topicId", "orders"))
	})

	// A refusal is an ordinary answer, not a dead process: one target this transport
	// cannot resolve is one subscription that will not fire, and the others still
	// should. The reason is what the emulator logs when it skips it.
	It("reports a refused plan as an answer, and keeps serving", func() {
		h.planErr = errors.New("target names no topicId")

		got := serve(
			transportproto.Request{ID: 1, Op: transportproto.OpPlan},
			transportproto.Request{ID: 2, Op: transportproto.OpHello},
		)

		Expect(got).To(HaveLen(2))
		Expect(got[0].OK).To(BeFalse())
		Expect(got[0].Reason).To(Equal("target names no topicId"))
		Expect(got[1].OK).To(BeTrue())
	})

	It("reports a failed send the same way", func() {
		h.sendErr = errors.New("the broker is not there")

		got := serve(transportproto.Request{ID: 1, Op: transportproto.OpSend})

		Expect(got[0].OK).To(BeFalse())
		Expect(got[0].Reason).To(Equal("the broker is not there"))
	})

	It("refuses an operation it does not know", func() {
		got := serve(transportproto.Request{ID: 1, Op: "explode"})

		Expect(got[0].OK).To(BeFalse())
		Expect(got[0].Reason).To(ContainSubstring(`unknown operation "explode"`))
	})

	It("answers every request under its own id", func() {
		got := serve(
			transportproto.Request{ID: 10, Op: transportproto.OpHello},
			transportproto.Request{ID: 20, Op: transportproto.OpPlan, Target: map[string]any{"topicId": "a"}},
			transportproto.Request{ID: 30, Op: transportproto.OpSend},
		)

		Expect([]int{got[0].ID, got[1].ID, got[2].ID}).To(Equal([]int{10, 20, 30}))
	})

	It("returns cleanly at EOF, because that is how the emulator says it is finished", func() {
		Expect(transportproto.Serve(context.Background(), strings.NewReader(""), &strings.Builder{}, h)).To(Succeed())
	})

	It("skips a blank line rather than treating it as a frame", func() {
		var out strings.Builder
		in := strings.NewReader("\n\n" + `{"id":1,"op":"hello"}` + "\n")

		Expect(transportproto.Serve(context.Background(), in, &out, h)).To(Succeed())
		Expect(strings.Count(strings.TrimSpace(out.String()), "\n")).To(Equal(0))
	})

	// There is no id to answer under, so there is nobody to tell — and guessing where
	// the next frame starts is how a protocol desynchronises quietly.
	It("stops on a frame it cannot parse", func() {
		err := transportproto.Serve(context.Background(),
			strings.NewReader("not json\n"+`{"id":1,"op":"hello"}`+"\n"), &strings.Builder{}, h)

		Expect(err).To(MatchError(ContainSubstring("decode a request")))
	})
})
