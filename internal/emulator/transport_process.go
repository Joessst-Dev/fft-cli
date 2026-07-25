package emulator

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"sync/atomic"
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
	stderr  *linePrefixer
	timeout time.Duration

	// mu serialises the request/response pairs.
	//
	// The emitter fans out concurrently, so several deliveries can reach one child at
	// once. Pairing them under a lock is the simple correct thing; the protocol
	// carries a correlation id so that pipelining, if a local broker ever turns out to
	// be the bottleneck, is a change here and not a change to the wire.
	mu     sync.Mutex
	nextID int

	// closed is an atomic rather than mu-guarded, because fail sets it from the
	// watchdog goroutine while do holds mu — taking mu there would deadlock. do reads
	// it before acquiring mu, so a delivery to a child killed for a protocol violation
	// gets the defined ErrClosed instead of a raw pipe error. It is the ErrClosed
	// gate, not the shutdown latch: fail sets it to stop new deliveries but does not
	// reap the process — reaping is [processTransport.reap]'s job.
	closed atomic.Bool

	// reap runs the wait-for-exit exactly once, whoever triggers the shutdown. It has
	// to be independent of closed: fail sets closed and kills the child but must not
	// block on cmd.Wait (it runs from inside do, under mu), so the actual wait is left
	// to Close — and Close must run it even when fail already marked the transport
	// closed, or a killed-but-unreaped process keeps its executable locked on Windows
	// and the next temp-dir cleanup fails with "access is denied".
	reap sync.Once

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
	// os.Environ, not nil: a transport is a headless component like any other and keeps
	// the machine's environment — PATH, HOME, a proxy, the CA bundle — which a broker
	// client reaches for. Environ strips the FFT_ namespace and puts back only what the
	// (none) session allows, so the credential boundary still holds; what survives is
	// the ambient configuration the in-process transport used to see for free.
	env, err := component.Environ(os.Environ(), c, component.Command{Session: component.SessionNone}, component.EnvOptions{
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

	// A grandchild that inherited the stdout pipe would otherwise keep cmd.Wait
	// blocked long after the child itself was killed. WaitDelay closes the child's
	// fds a bounded time after the context is cancelled, so shutdown cannot hang on a
	// process the transport no longer controls.
	cmd.WaitDelay = childGrace

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
	stderr := prefixWriter(log, c.Name+": ")
	cmd.Stderr = stderr

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
		stderr:  stderr,
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
			// Honour the emitter's shared fan-out deadline *before* starting a round trip,
			// not by killing the child. Deliveries to one target serialize under the
			// transport's lock, each with its own per-request timeout, so a slow-but-alive
			// broker with many subscriptions could otherwise hold a mutation's request path
			// well past the publishTimeout eventing.go documents. Checking here caps that at
			// the deadline without ever treating a wedged-vs-cancelled child as a failure —
			// the per-request timer below remains the only thing that kills one.
			if err := ctx.Err(); err != nil {
				return err
			}

			// The emitter's ctx is deliberately not threaded into the kill path. Its
			// deadline is shared across the whole fan-out, so killing this child when it
			// fires would take down a healthy transport because some *other* target was
			// slow. The target travels with every frame: the child keeps no state between
			// them, which removes handle lifetimes from a protocol whose job is to be
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
				// The child confirmed the publish. Returning ctx.Err() here would report a
				// delivery that succeeded as failed the instant the shared deadline lapsed.
				return nil
			}
		},
	}, nil
}

// status reports where this transport delivers, for the startup notice.
func (t *processTransport) describe() string { return t.status }

// do sends one request and reads its response, bounded by the transport's own
// per-request timer.
func (t *processTransport) do(req transportproto.Request) (transportproto.Response, error) {
	// Read before mu, because fail sets it while a wedged do still holds mu — this is
	// how a delivery to an already-killed child gets ErrClosed rather than blocking on
	// a lock the dying call still owns.
	if t.closed.Load() {
		return transportproto.Response{}, transportproto.ErrClosed
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed.Load() {
		return transportproto.Response{}, transportproto.ErrClosed
	}

	t.nextID++
	req.ID = t.nextID

	// A child that stops answering must not hold the lock — and with it every other
	// delivery — for longer than the per-request timer. It closes the pipes, which is
	// what unblocks the read below.
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
		t.fail(err)
		return transportproto.Response{}, fmt.Errorf("ask the %s transport: %w", t.name, err)
	}

	if !t.out.Scan() {
		err := t.out.Err()
		if err == nil {
			err = io.EOF
		}
		// A read that ends is a transport that ended: mark it dead so the next delivery
		// gets ErrClosed rather than trying to write to a broken pipe.
		t.fail(err)
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

// fail marks a child that has stopped speaking the protocol dead and kills it.
//
// It takes no mutex and does not wait for the process: it is called from inside do,
// and from the timer goroutine do started, precisely because mu may be held by a read
// that will not return until the pipes are closed, and cmd.Wait would block. Closing
// stdin and cancelling the context unblock the read; the reap is left to Close, which
// every failed transport still goes through. Idempotent via the closed swap, so the
// watchdog firing after do already failed does not log twice.
func (t *processTransport) fail(cause error) {
	if t.closed.Swap(true) {
		return
	}
	fmt.Fprintf(orDiscard(t.log), "emulator: the %s transport stopped speaking the protocol (%v); it will deliver nothing more\n",
		t.name, cause)
	t.kill()
	_ = t.in.Close()
}

// Close shuts the child down and waits for it to exit.
//
// It always reaps, even when fail already marked the transport closed: an unreaped
// child that was killed still holds its executable open, which on Windows fails the
// next attempt to delete it. The wait itself runs once, whichever path gets here.
func (t *processTransport) Close() error {
	t.closed.Store(true)

	t.reap.Do(func() {
		// stdin first, which is how the protocol says "no more requests"; fail may have
		// closed it already, and a second close is harmless.
		_ = t.in.Close()

		exited := make(chan error, 1)
		go func() { exited <- t.cmd.Wait() }()

		select {
		case <-exited:
			t.kill()
		case <-time.After(childGrace):
			t.kill()
			<-exited
		}

		// The child is gone; write out its last, newline-less log line — usually the most
		// telling one — rather than leaving it in the buffer.
		t.stderr.flush()
	})
	return nil
}

// maxLogLine caps how much of one unterminated log line the prefixer buffers. A child
// that logs without ever writing a newline would otherwise grow the buffer without
// bound and take the emulator's memory with it; past this, the line is flushed as-is.
const maxLogLine = 64 << 10

// prefixWriter tags every line a child logs with which transport wrote it, so one
// emulator's stderr with three transports on it is still readable.
func prefixWriter(w io.Writer, prefix string) *linePrefixer {
	if w == nil {
		w = io.Discard
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
// than wherever the pipe happened to split. A line longer than [maxLogLine] is
// flushed without waiting for its newline, so a child logging unbounded output cannot
// grow the buffer without limit.
func (p *linePrefixer) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.buf = append(p.buf, b...)
	for {
		i := bytes.IndexByte(p.buf, '\n')
		if i < 0 {
			if len(p.buf) >= maxLogLine {
				if _, err := fmt.Fprintf(p.w, "%s%s\n", p.prefix, p.buf); err != nil {
					return len(b), err
				}
				p.buf = p.buf[:0]
			}
			return len(b), nil
		}
		if _, err := fmt.Fprintf(p.w, "%s%s\n", p.prefix, p.buf[:i]); err != nil {
			return len(b), err
		}
		p.buf = p.buf[i+1:]
	}
}

// flush writes whatever partial line is left, prefixed. A child's last log before it
// crashes often has no trailing newline, and it is the most useful line there is; the
// transport calls this when the child is gone so it is not lost.
func (p *linePrefixer) flush() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.buf) > 0 {
		fmt.Fprintf(p.w, "%s%s\n", p.prefix, p.buf)
		p.buf = p.buf[:0]
	}
}

func orDiscard(w io.Writer) io.Writer {
	if w == nil {
		return io.Discard
	}
	return w
}
