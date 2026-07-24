package emulator

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/Joessst-Dev/fft-cli/internal/component"
	"github.com/Joessst-Dev/fft-cli/internal/transportproto"
)

// defaultRequestTimeout bounds one frame's round trip.
//
// The emitter's own deadline covers a whole fan-out, and would be spent entirely by
// a single child that never answers. This is what keeps one wedged transport from
// starving every other subscription of the same event.
//
// A spec shortens it through [requestTimeout], which each transport reads once at
// construction — never from the round-trip goroutine, so shortening it races nothing.
const defaultRequestTimeout = 8 * time.Second

// requestTimeout is what a new transport will use. It is a var only so a spec can
// lower it before constructing one; the value is copied into the transport at
// construction and the mutable global is never read again.
var requestTimeout = defaultRequestTimeout

// childGrace is how long a transport child gets to exit after its stdin is closed.
const childGrace = 3 * time.Second

// processTransport is a transport that lives in another process.
//
// It satisfies the same interface a compiled-in transport does, which is the point:
// the emitter cannot tell them apart, so an out-of-process transport is a drop-in
// rather than a second delivery path with its own rules.
type processTransport struct {
	name string
	log  io.Writer

	cmd     *exec.Cmd
	in      io.WriteCloser
	out     *bufio.Scanner
	enc     *json.Encoder
	kill    context.CancelFunc
	timeout time.Duration

	// mu serialises the request/response pairs.
	//
	// The emitter fans out concurrently, so several deliveries can reach one child at
	// once. Pairing them under a lock is the simple correct thing; the protocol
	// carries a correlation id so that pipelining, if a local broker ever turns out to
	// be the bottleneck, is a change here and not a change to the wire.
	mu     sync.Mutex
	closed bool
	nextID int

	// status is what the child said at hello, for the startup notice.
	status string

	// targets are the target types it answered hello with.
	targets []string
}

// newProcessTransport starts a transport component and shakes hands with it.
//
// A child that cannot be started, or that does not answer hello, is an error rather
// than a transport that is registered and fails later: the startup notice has to be
// able to say whether a target type will be delivered, and "it started but we will
// find out at the first event" is not an answer.
func newProcessTransport(c component.Component, extra map[string]string, log io.Writer) (*processTransport, error) {
	env, err := component.Environ(nil, c, component.Command{Session: component.SessionNone}, component.EnvOptions{
		Extra: extra,
	})
	if err != nil {
		return nil, err
	}
	env = append(env, transportproto.EnvVersion+"="+strconv.Itoa(transportproto.Version))

	// Its own cancel rather than the server's context: a transport is closed by
	// closing its stdin, and this is only the backstop for a child that ignores that.
	ctx, cancel := context.WithCancel(context.Background())

	cmd := exec.CommandContext(ctx, c.ExecPath()) //nolint:gosec // a validated manifest inside the managed component root
	cmd.Env = env

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	// The child's diagnostics land on the emulator's own stderr, prefixed with which
	// transport said them. That is the whole of its logging: stdout is the protocol.
	cmd.Stderr = prefixWriter(log, c.Name+": ")

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start the %s transport: %w", c.Name, err)
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64<<10), transportproto.MaxFrame)

	t := &processTransport{
		name:    c.Name,
		log:     log,
		cmd:     cmd,
		in:      stdin,
		out:     scanner,
		enc:     json.NewEncoder(stdin),
		kill:    cancel,
		timeout: requestTimeout,
	}

	hello, err := t.do(transportproto.Request{Op: transportproto.OpHello})
	if err != nil {
		_ = t.Close()
		return nil, fmt.Errorf("greet the %s transport: %w", c.Name, err)
	}
	if !hello.OK {
		_ = t.Close()
		return nil, fmt.Errorf("the %s transport refused to start: %s", c.Name, hello.Reason)
	}

	t.status, t.targets = hello.Status, hello.Targets
	return t, nil
}

// plan asks the child to resolve a target.
func (t *processTransport) plan(target map[string]any) (delivery, error) {
	res, err := t.do(transportproto.Request{Op: transportproto.OpPlan, Target: target})
	if err != nil {
		return delivery{}, err
	}
	if !res.OK {
		return delivery{}, errors.New(res.Reason)
	}

	return delivery{
		label: res.Label,
		send: func(ctx context.Context, event string, data []byte) error {
			// The target travels with every frame: the child keeps no state between them,
			// which is what removes handle lifetimes from a protocol whose job is to be
			// obviously correct.
			sent, err := t.do(transportproto.Request{
				Op: transportproto.OpSend, Target: target, Event: event, Data: data,
			})
			switch {
			case err != nil:
				return err
			case !sent.OK:
				return errors.New(sent.Reason)
			default:
				return ctx.Err()
			}
		},
	}, nil
}

// status reports where this transport delivers, for the startup notice.
func (t *processTransport) describe() string { return t.status }

// do sends one request and reads its response.
func (t *processTransport) do(req transportproto.Request) (transportproto.Response, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return transportproto.Response{}, transportproto.ErrClosed
	}

	t.nextID++
	req.ID = t.nextID

	// A child that stops answering must not hold the lock — and with it every other
	// delivery — for the emitter's whole deadline. The timer closes the pipes, which
	// is what unblocks the read below.
	done := make(chan struct{})
	defer close(done)

	go func() {
		select {
		case <-done:
		case <-time.After(t.timeout):
			t.fail(fmt.Errorf("no answer in %s", t.timeout))
		}
	}()

	if err := t.enc.Encode(req); err != nil {
		return transportproto.Response{}, fmt.Errorf("ask the %s transport: %w", t.name, err)
	}

	if !t.out.Scan() {
		err := t.out.Err()
		if err == nil {
			err = io.EOF
		}
		return transportproto.Response{}, fmt.Errorf("read from the %s transport: %w", t.name, err)
	}

	var res transportproto.Response
	if err := json.Unmarshal(t.out.Bytes(), &res); err != nil {
		// The stream is out of step and there is no way back to a frame boundary, so the
		// transport is finished rather than merely unlucky.
		t.fail(err)
		return transportproto.Response{}, fmt.Errorf("decode the %s transport's answer: %w", t.name, err)
	}
	if res.ID != req.ID {
		t.fail(fmt.Errorf("answered request %d with %d", req.ID, res.ID))
		return transportproto.Response{}, fmt.Errorf("the %s transport answered the wrong request", t.name)
	}
	return res, nil
}

// fail kills a child that has stopped speaking the protocol.
//
// It does not take the lock: it is called from inside do, and from the timer
// goroutine do started, precisely because the lock may be held by a read that will
// not return until the pipes are closed.
func (t *processTransport) fail(cause error) {
	fmt.Fprintf(orDiscard(t.log), "emulator: the %s transport stopped speaking the protocol (%v); it will deliver nothing more\n",
		t.name, cause)
	t.kill()
	_ = t.in.Close()
}

// Close shuts the child down: stdin first, which is how the protocol says "no more
// requests", and the context only if it does not take the hint.
func (t *processTransport) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	t.mu.Unlock()

	_ = t.in.Close()

	exited := make(chan error, 1)
	go func() { exited <- t.cmd.Wait() }()

	select {
	case <-exited:
		t.kill()
		return nil
	case <-time.After(childGrace):
		t.kill()
		<-exited
		return nil
	}
}

// prefixWriter tags every line a child logs with which transport wrote it, so one
// emulator's stderr with three transports on it is still readable.
func prefixWriter(w io.Writer, prefix string) io.Writer {
	if w == nil {
		return io.Discard
	}
	return &linePrefixer{w: w, prefix: prefix}
}

type linePrefixer struct {
	w      io.Writer
	prefix string
	mu     sync.Mutex
	buf    []byte
}

// Write buffers until a newline, so the prefix lands at the start of a line rather
// than wherever the pipe happened to split.
func (p *linePrefixer) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.buf = append(p.buf, b...)
	for {
		i := bytes.IndexByte(p.buf, '\n')
		if i < 0 {
			return len(b), nil
		}
		if _, err := fmt.Fprintf(p.w, "%s%s\n", p.prefix, p.buf[:i]); err != nil {
			return len(b), err
		}
		p.buf = p.buf[i+1:]
	}
}

func orDiscard(w io.Writer) io.Writer {
	if w == nil {
		return io.Discard
	}
	return w
}
