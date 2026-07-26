// Package transportproto is the line protocol between the emulator and a transport
// component.
//
// # Why there is a protocol at all
//
// The emulator delivers a domain event to whatever a subscription's target names.
// It knows three target types; the community will want others — Kafka, NATS, SQS,
// RabbitMQ — and none of those belong in a binary everybody downloads.
//
// So a transport is a separate process, and this is what the two say to each other:
// newline-delimited JSON, requests on the child's stdin and responses on its stdout.
// It maps one-to-one onto the interface the emulator already had internally, which
// is what makes an out-of-process transport a drop-in for a compiled-in one rather
// than a second way of doing the same thing.
//
// # stdout is the protocol, stderr is the log
//
// The same split fft itself keeps. A transport writes diagnostics to stderr, which
// the emulator forwards to its own; anything it writes to stdout must be a frame.
// A component that prints a friendly startup banner on stdout breaks the protocol,
// and that is the right trade: one stream with one meaning is what makes the whole
// thing debuggable by reading it.
//
// # Frames
//
//	→ {"id":1,"op":"hello"}
//	← {"id":1,"ok":true,"targets":["GOOGLE_CLOUD_PUB_SUB"],"status":"publishing to the Pub/Sub emulator at localhost:8085"}
//
//	→ {"id":2,"op":"plan","target":{"type":"GOOGLE_CLOUD_PUB_SUB","projectId":"p","topicId":"orders"}}
//	← {"id":2,"ok":true,"label":"p/orders"}
//
//	→ {"id":3,"op":"send","target":{…},"event":"ORDER_CREATED","data":{…}}
//	← {"id":3,"ok":true}
//
// A refusal is ok:false with a reason, which is what the emulator logs when it skips
// a subscription. It is an ordinary answer, not an error: a target this transport
// cannot resolve is a subscription that will not fire, and the user needs to be told
// which and why.
//
// Every request carries the target, because the child holds no state between frames.
// That costs a few bytes and removes handle lifetimes, reconnection and leak
// questions from a protocol whose whole job is to be obviously correct.
//
// # Compatibility
//
// [Request], [Response], [Handler] and [Serve] — together with [MaxFrame] and the
// [OpHello]/[OpPlan]/[OpSend] constants — are the public surface an external transport
// builds against, so their shape is a commitment. [Version] numbers that contract: a
// change that alters what an existing frame or field means bumps it, while a new
// optional field an older component can ignore does not.
//
// The emulator hands the child the version it speaks in [EnvVersion]. fft does not
// enforce a match — it only reports it — so gating on it is the component's own choice:
// read [EnvVersion], compare it against [Version], and refuse a host you do not
// understand rather than fail in some more interesting way further in.
package transportproto

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
)

// Version is the protocol this build speaks. The emulator sets it in the child's
// environment as [EnvVersion] so a component can refuse a host it does not
// understand rather than failing in some more interesting way further in.
const Version = 1

// EnvVersion carries [Version] to the child.
const EnvVersion = "FFT_TRANSPORT_API"

// The operations a request can carry.
const (
	// OpHello asks what the transport delivers and how it would say so on the
	// emulator's startup notice. It is the first frame, always.
	OpHello = "hello"

	// OpPlan resolves a stored target into a label, or reports why it cannot.
	OpPlan = "plan"

	// OpSend delivers one event.
	OpSend = "send"
)

// MaxFrame caps a single line in either direction. An event payload is a domain
// document; anything past this is not one, and a transport that starts emitting
// unbounded output should be stopped rather than read.
const MaxFrame = 8 << 20

// Request is one frame from the emulator to a transport.
type Request struct {
	// ID correlates a response with its request.
	//
	// The emulator sends one request at a time today, so the correlation is not
	// strictly needed — it is here so that pipelining is a later change to the
	// emulator rather than a break in the protocol.
	ID int `json:"id"`

	// Op is one of [OpHello], [OpPlan], [OpSend].
	Op string `json:"op"`

	// Target is the subscription's target document, verbatim as it was registered.
	Target map[string]any `json:"target,omitempty"`

	// Event is the event's name, for [OpSend]. It is passed alongside the data
	// rather than baked into it because how an event is labelled on the wire is each
	// transport's own convention — a Pub/Sub attribute, an AMQP subject.
	Event string `json:"event,omitempty"`

	// Data is the event envelope to deliver, as JSON.
	Data json.RawMessage `json:"data,omitempty"`
}

// Response is one frame from a transport back to the emulator.
type Response struct {
	ID int `json:"id"`

	// OK reports whether the operation succeeded. False is an ordinary answer with a
	// Reason, not a protocol failure.
	OK bool `json:"ok"`

	// Reason says why, when OK is false. It is logged verbatim by the emulator, so
	// it should read as the end of the sentence "skip this subscription: …".
	Reason string `json:"reason,omitempty"`

	// Label names the destination a plan resolved to, for the emulator's report of
	// where an event went.
	Label string `json:"label,omitempty"`

	// Targets are the subscription target types this transport delivers, answered to
	// a hello. The emulator checks them against the manifest's own list.
	Targets []string `json:"targets,omitempty"`

	// Status is one line for the emulator's startup notice, saying where this
	// transport will deliver — or why it will not.
	Status string `json:"status,omitempty"`
}

// Handler is what a transport component implements. [Serve] turns one into a
// process that speaks the protocol.
type Handler interface {
	// Hello reports the target types this transport delivers, and one line for the
	// startup notice saying where.
	Hello() (targets []string, status string, err error)

	// Plan resolves a target into the label its deliveries are reported under, or
	// reports why it cannot — a malformed target, or one this transport will not
	// reach.
	Plan(target map[string]any) (label string, err error)

	// Send delivers one event to a target Plan accepted.
	Send(ctx context.Context, target map[string]any, event string, data []byte) error
}

// Serve reads requests from in, answers them with h, and writes the responses to
// out, until in reaches EOF.
//
// It returns nil at EOF: the emulator closes the child's stdin to say it is
// finished, and a clean shutdown is not an error. ctx is checked after each frame is
// read and before it is answered, and is passed to [Handler.Send]; a cancellation
// therefore drops the request in hand and stops the loop, returning nil as at EOF, but
// does not unblock a read already waiting on a stream that has gone quiet. Closing in
// unblocks that, which is what the emulator does on shutdown.
func Serve(ctx context.Context, in io.Reader, out io.Writer, h Handler) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64<<10), MaxFrame)

	enc := json.NewEncoder(out)

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			// A frame that will not parse has no id to answer under, so there is nobody to
			// tell. Stopping is the honest response: the stream is out of step, and
			// guessing where the next frame starts is how a protocol desynchronises quietly.
			return fmt.Errorf("decode a request: %w", err)
		}

		if err := enc.Encode(answer(ctx, h, req)); err != nil {
			return fmt.Errorf("write a response: %w", err)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read a request: %w", err)
	}
	return nil
}

// answer runs one request through the handler. A handler that fails is reported as
// ok:false rather than as a dead process: one target it cannot resolve is one
// subscription that does not fire, and the others still should.
func answer(ctx context.Context, h Handler, req Request) Response {
	res := Response{ID: req.ID}

	switch req.Op {
	case OpHello:
		targets, status, err := h.Hello()
		if err != nil {
			res.Reason = err.Error()
			return res
		}
		res.OK, res.Targets, res.Status = true, targets, status

	case OpPlan:
		label, err := h.Plan(req.Target)
		if err != nil {
			res.Reason = err.Error()
			return res
		}
		res.OK, res.Label = true, label

	case OpSend:
		if err := h.Send(ctx, req.Target, req.Event, req.Data); err != nil {
			res.Reason = err.Error()
			return res
		}
		res.OK = true

	default:
		res.Reason = fmt.Sprintf("unknown operation %q", req.Op)
	}

	return res
}
