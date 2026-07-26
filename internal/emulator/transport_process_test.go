package emulator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"

	"github.com/Joessst-Dev/fft-cli/internal/component"
	"github.com/Joessst-Dev/fft-cli/pkg/transportproto"
)

// The specs here drive a real child process, because everything worth pinning about
// an out-of-process transport is about the process: that its stderr is forwarded and
// its stdout is not, that a refusal is a skipped subscription rather than a dead
// emulator, and that a child which stops answering does not take the emulator with it.
//
// The child is this test binary re-executed behind a non-FFT_ environment marker —
// the same trick the cmd/fft specs use, and for the same reason: a shell script does
// not run on Windows, and one file copy for the suite gets a real executable on all
// three platforms.
const (
	envFakeTransport = "SPEC_TRANSPORT"

	// The behaviours a fake transport can be told to have.
	fakeNormal      = "normal"  // answers everything
	fakeRefuseHello = "refuse"  // says no at the handshake
	fakeNoisy       = "noisy"   // logs to stderr, which the emulator must forward
	fakeGarbage     = "garbage" // answers with something that is not a frame
	fakeSilent      = "silent"  // reads, and never answers
)

// init makes this binary a transport component when it is run as one.
func init() {
	mode := os.Getenv(envFakeTransport)
	if mode == "" {
		return
	}
	runFakeTransport(mode)
}

func runFakeTransport(mode string) {
	switch mode {
	case fakeGarbage:
		// A frame the emulator cannot parse. It has to give up on this transport rather
		// than try to find the next frame boundary.
		fmt.Println("this is not a frame")
		select {}

	case fakeSilent:
		// Reads and never answers, so the emulator's per-request timeout is the only
		// thing that ends it.
		_, _ = io.Copy(io.Discard, os.Stdin)
		os.Exit(0)
	}

	if mode == fakeNoisy {
		fmt.Fprintln(os.Stderr, "starting up")
		fmt.Fprintln(os.Stderr, "two lines of it")
	}

	// An ambient, non-FFT_ variable the child echoes so a spec can prove it was
	// inherited — which it is only because newProcessTransport passes os.Environ() as
	// the base rather than nil, the thing the in-process transport got for free.
	if probe := os.Getenv("SPEC_TRANSPORT_PROBE"); probe != "" {
		fmt.Fprintf(os.Stderr, "probe=%s\n", probe)
	}

	// The protocol version the emulator says it speaks, echoed so a spec can prove the
	// handshake the public docs tell external transports to gate on actually arrives.
	fmt.Fprintf(os.Stderr, "transport-api=%s\n", os.Getenv(transportproto.EnvVersion))

	handler := &fakeHandler{refuseHello: mode == fakeRefuseHello}
	_ = transportproto.Serve(context.Background(), os.Stdin, os.Stdout, handler)
	os.Exit(0)
}

type fakeHandler struct{ refuseHello bool }

func (h *fakeHandler) Hello() ([]string, string, error) {
	if h.refuseHello {
		return nil, "", fmt.Errorf("no broker was configured")
	}
	return []string{targetGoogleCloudPubSub}, "publishing to a spec", nil
}

func (h *fakeHandler) Plan(target map[string]any) (string, error) {
	topic := mapString(target, "topicId")
	if topic == "" {
		return "", fmt.Errorf("target names no topicId")
	}
	return "spec/" + topic, nil
}

func (h *fakeHandler) Send(_ context.Context, _ map[string]any, event string, data []byte) error {
	if event == "BOOM" {
		return fmt.Errorf("the broker refused it")
	}
	// Echoed to stderr so a spec can prove the payload crossed the process boundary
	// intact rather than merely that the call returned nil.
	fmt.Fprintf(os.Stderr, "sent %s %s\n", event, data)
	return nil
}

// fakeTransportBinary is an executable copy of this test binary, made once.
var (
	fakeBinaryOnce sync.Once
	fakeBinaryDir  string
	fakeBinaryPath string
	fakeBinaryErr  error
)

var _ = AfterSuite(func() {
	if fakeBinaryDir != "" {
		Expect(os.RemoveAll(fakeBinaryDir)).To(Succeed())
	}
})

func fakeTransportBinary() string {
	GinkgoHelper()

	fakeBinaryOnce.Do(func() {
		self, err := os.Executable()
		if err != nil {
			fakeBinaryErr = err
			return
		}
		fakeBinaryDir, fakeBinaryErr = os.MkdirTemp("", "fft-fake-transport-")
		if fakeBinaryErr != nil {
			return
		}
		fakeBinaryPath = filepath.Join(fakeBinaryDir, "transport")
		fakeBinaryErr = copyExecutable(self, fakeBinaryPath)
	})

	Expect(fakeBinaryErr).NotTo(HaveOccurred())
	return fakeBinaryPath
}

func copyExecutable(src, dst string) error {
	in, err := os.Open(src) //nolint:gosec // this test binary's own path
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755) //nolint:gosec // a temp dir this spec made
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// installFakeTransport writes a transport component into a fresh root and returns a
// registry that has found it.
func installFakeTransport() *component.Registry {
	GinkgoHelper()

	root := GinkgoT().TempDir()
	writeFakeTransport(root, "spec-transport", targetGoogleCloudPubSub)
	return component.Open(root, component.WithFirstParty(nil))
}

// writeFakeTransport lays one transport component down under root, delivering the
// given targets. Several may share a root, which is how a collision is set up.
func writeFakeTransport(root, name string, targets ...string) {
	GinkgoHelper()

	dir := filepath.Join(root, name)
	Expect(os.MkdirAll(filepath.Join(dir, "bin"), 0o755)).To(Succeed())
	Expect(copyExecutable(fakeTransportBinary(), filepath.Join(dir, "bin", execName("transport")))).To(Succeed())

	m := component.Manifest{
		APIVersion: component.APIVersion,
		Name:       name,
		Version:    "1.0.0",
		Kind:       component.KindTransport,
		Exec:       "bin/" + execName("transport"),
		Targets:    targets,
		Env:        []string{envFakeTransport},
	}
	data, err := yaml.Marshal(m)
	Expect(err).NotTo(HaveOccurred())
	Expect(os.WriteFile(filepath.Join(dir, component.ManifestName), data, 0o600)).To(Succeed())
}

// execName is what the executable is called on this platform.
func execName(name string) string {
	if os.PathSeparator == '\\' {
		return name + ".exe"
	}
	return name
}

// syncBuffer is a log the emulator writes from its stderr-forwarding goroutine while
// a spec reads it with Eventually. A plain strings.Builder is not safe for that; this
// is the smallest thing that is.
type syncBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

var _ = Describe("a transport component", func() {
	// newSet builds the transports an emulator with this fake installed would have.
	newSet := func(mode string) (map[string]transport, map[string]string, *syncBuffer) {
		log := &syncBuffer{}
		transports, reasons := newTransports(Config{
			Components:   installFakeTransport(),
			TransportEnv: map[string]string{envFakeTransport: mode},
			Log:          log,
		})
		DeferCleanup(func() { _ = closeTransports(transports) })

		return transports, reasons, log
	}

	It("registers the target types it answers hello with", func() {
		transports, _, _ := newSet(fakeNormal)

		Expect(transports).To(HaveKey(targetGoogleCloudPubSub))
		Expect(transports).To(HaveKey(targetWebhook))
	})

	It("reports where it delivers, in the transport's own words", func() {
		transports, _, _ := newSet(fakeNormal)

		// This is what the startup notice prints, which is how the notice says where a
		// broker is without the emulator knowing anything about brokers.
		d, ok := transports[targetGoogleCloudPubSub].(describer)
		Expect(ok).To(BeTrue())
		Expect(d.describe()).To(Equal("publishing to a spec"))
	})

	It("plans and sends across the process boundary", func() {
		transports, _, log := newSet(fakeNormal)

		d, err := transports[targetGoogleCloudPubSub].plan(map[string]any{"topicId": "orders"})
		Expect(err).NotTo(HaveOccurred())
		Expect(d.label).To(Equal("spec/orders"))

		envelope := []byte(`{"event":"ORDER_CREATED","eventId":"abc"}`)
		Expect(d.send(context.Background(), "ORDER_CREATED", envelope)).To(Succeed())

		// The child echoed what it received to stderr, so this proves the envelope
		// crossed intact rather than merely that the call returned nil.
		Eventually(log.String).Should(ContainSubstring(`sent ORDER_CREATED {"event":"ORDER_CREATED","eventId":"abc"}`))
	})

	It("skips a send whose deadline has already lapsed, without killing the transport", func() {
		// Two properties at once. The emitter's ctx is a deadline shared across the whole
		// fan-out: a delivery must not START once it has lapsed (round 3 — otherwise queued
		// deliveries to one slow target blow past the shared cap). But a lapsed deadline
		// must not KILL the child either (round 1 — a slow unrelated target would take a
		// healthy transport down with it). So a cancelled send returns the cancellation and
		// leaves the transport able to deliver the next one.
		transports, _, log := newSet(fakeNormal)

		d, err := transports[targetGoogleCloudPubSub].plan(map[string]any{"topicId": "orders"})
		Expect(err).NotTo(HaveOccurred())

		cancelled, cancel := context.WithCancel(context.Background())
		cancel()

		Expect(d.send(cancelled, "ORDER_CREATED", []byte(`{"marker":"cancelled"}`))).To(MatchError(context.Canceled))

		// The round trip never happened — the child was never asked to send it.
		Consistently(log.String, "200ms", "50ms").ShouldNot(ContainSubstring("cancelled"))

		// And the transport is alive, not killed by the lapsed deadline.
		Expect(d.send(context.Background(), "ORDER_CREATED", []byte(`{"marker":"live"}`))).To(Succeed())
		Eventually(log.String).Should(ContainSubstring(`"marker":"live"`))
	})

	It("reports a target the transport refuses as an ordinary error", func() {
		transports, _, _ := newSet(fakeNormal)

		_, err := transports[targetGoogleCloudPubSub].plan(map[string]any{})
		Expect(err).To(MatchError(ContainSubstring("target names no topicId")))
	})

	It("reports a failed send as an error, leaving the transport usable", func() {
		transports, _, _ := newSet(fakeNormal)
		t := transports[targetGoogleCloudPubSub]

		d, err := t.plan(map[string]any{"topicId": "orders"})
		Expect(err).NotTo(HaveOccurred())
		Expect(d.send(context.Background(), "BOOM", []byte("{}"))).To(MatchError(ContainSubstring("the broker refused it")))

		// One failed delivery is not a dead transport: the next subscription still has
		// to be attempted.
		Expect(d.send(context.Background(), "ORDER_CREATED", []byte("{}"))).To(Succeed())
	})

	It("prefixes the transport's own log with its name", func() {
		_, _, log := newSet(fakeNoisy)

		Eventually(log.String).Should(ContainSubstring("spec-transport: starting up"))
		Eventually(log.String).Should(ContainSubstring("spec-transport: two lines of it"))
	})

	It("registers one of two transports that claim the same target, and stops the other", func() {
		// Two components both delivering GOOGLE_CLOUD_PUB_SUB. Only one may own the
		// target; the other must be closed, not left running with nothing routed to it,
		// and not overwrite the winner's map entry.
		root := GinkgoT().TempDir()
		writeFakeTransport(root, "aaa-transport", targetGoogleCloudPubSub)
		writeFakeTransport(root, "bbb-transport", targetGoogleCloudPubSub)

		log := &syncBuffer{}
		transports, _ := newTransports(Config{
			Components:   component.Open(root, component.WithFirstParty(nil)),
			TransportEnv: map[string]string{envFakeTransport: fakeNormal},
			Log:          log,
		})
		DeferCleanup(func() { _ = closeTransports(transports) })

		Expect(transports).To(HaveKey(targetGoogleCloudPubSub))

		// The loser said so, and the target it lost still resolves — to the winner, a
		// live transport, not a closed one.
		Expect(log.String()).To(ContainSubstring("already does"))
		d, err := transports[targetGoogleCloudPubSub].plan(map[string]any{"topicId": "orders"})
		Expect(err).NotTo(HaveOccurred())
		Expect(d.send(context.Background(), "ORDER_CREATED", []byte("{}"))).To(Succeed())
	})

	Describe("a transport that will not work", func() {
		// Not fatal, and the reason is remembered: the startup notice has to tell a
		// component that is not installed from one that is installed and would not
		// start, because they have completely different fixes.
		It("is left out, with a reason the notice can use, when it refuses at hello", func() {
			transports, reasons, log := newSet(fakeRefuseHello)

			Expect(transports).NotTo(HaveKey(targetGoogleCloudPubSub))
			Expect(transports).To(HaveKey(targetWebhook))

			Expect(reasons[targetGoogleCloudPubSub]).To(ContainSubstring("installed but did not start"))
			Expect(log.String()).To(ContainSubstring("no broker was configured"))
		})

		// The ordinary state of a broker target you are not using: the transport is
		// installed, but you did not point it anywhere. That must read as "off", not as a
		// component that failed — so it is not even started, and the reason names the flag
		// rather than the install step, which is already done.
		It("is left unstarted when its configuration was not given", func() {
			log := &syncBuffer{}
			transports, reasons := newTransports(Config{
				Components:   installFakeTransport(),
				TransportEnv: map[string]string{}, // no SPEC_TRANSPORT value
				Log:          log,
			})
			DeferCleanup(func() { _ = closeTransports(transports) })

			Expect(transports).NotTo(HaveKey(targetGoogleCloudPubSub))
			Expect(reasons[targetGoogleCloudPubSub]).To(HavePrefix("off ("))
			Expect(log.String()).To(BeEmpty())
		})

		It("is left out when it does not speak the protocol at all", func() {
			transports, reasons, _ := newSet(fakeGarbage)

			Expect(transports).NotTo(HaveKey(targetGoogleCloudPubSub))
			Expect(reasons[targetGoogleCloudPubSub]).To(ContainSubstring("installed but did not start"))
		})

		// A child that reads and never answers would otherwise hold the emitter's whole
		// fan-out deadline, and with it every other subscription to the same event.
		It("gives up on one that never answers, rather than waiting for it", func() {
			previous := requestTimeout
			requestTimeout = 200 * time.Millisecond
			DeferCleanup(func() { requestTimeout = previous })

			transports, reasons, log := newSet(fakeSilent)

			Expect(transports).NotTo(HaveKey(targetGoogleCloudPubSub))
			Expect(reasons[targetGoogleCloudPubSub]).To(ContainSubstring("installed but did not start"))
			Expect(log.String()).To(ContainSubstring("stopped speaking the protocol"))
		})
	})

	It("closes the child when the emulator shuts down", func() {
		transports, _, _ := newSet(fakeNormal)

		t := transports[targetGoogleCloudPubSub]
		Expect(closeTransports(transports)).To(Succeed())

		// Closing is final: a delivery after shutdown must fail rather than reach a
		// process that is on its way out.
		_, err := t.plan(map[string]any{"topicId": "orders"})
		Expect(err).To(HaveOccurred())
	})

	It("gives the child the machine's environment, not a bare one", func() {
		// A transport is a broker client: it reads PATH, a proxy, the CA bundle. The
		// in-process transport saw all of it; the out-of-process one must too. This sets
		// an ambient variable the manifest does not declare — so it can only reach the
		// child by being inherited — and the child echoes it back.
		GinkgoT().Setenv("SPEC_TRANSPORT_PROBE", "inherited")

		_, _, log := newSet(fakeNormal)
		Eventually(log.String).Should(ContainSubstring("probe=inherited"))
	})

	It("tells the child which protocol version it speaks", func() {
		// The public compatibility docs tell an external transport to gate on
		// FFT_TRANSPORT_API, and that guard fails open — the got == "" branch skips the
		// check — if the emulator ever stops setting it. component.Environ strips the
		// FFT_ namespace and the version is appended back after; this pins that it
		// survives to the child with this build's version.
		_, _, log := newSet(fakeNormal)
		Eventually(log.String).Should(ContainSubstring(fmt.Sprintf("transport-api=%d", transportproto.Version)))
	})
})

var _ = Describe("unconfigured", func() {
	// The manifest's env is what a transport reads to know where to deliver. A value
	// from either the emulator's flags or the ambient environment counts — the second
	// is what makes a community transport work, since fft has no flag for its broker.
	It("counts a value from the emulator's flags", func() {
		c := component.Component{Manifest: component.Manifest{Env: []string{"KAFKA_BROKER"}}}
		Expect(unconfigured(c, map[string]string{"KAFKA_BROKER": "localhost:9092"})).To(BeFalse())
	})

	It("counts a value from the ambient environment", func() {
		GinkgoT().Setenv("KAFKA_BROKER", "localhost:9092")
		c := component.Component{Manifest: component.Manifest{Env: []string{"KAFKA_BROKER"}}}
		Expect(unconfigured(c, nil)).To(BeFalse())
	})

	It("is unconfigured when neither provides its declared var", func() {
		c := component.Component{Manifest: component.Manifest{Env: []string{"KAFKA_BROKER_DEFINITELY_UNSET"}}}
		Expect(unconfigured(c, nil)).To(BeTrue())
	})

	It("is always configured when it declares no variables", func() {
		c := component.Component{Manifest: component.Manifest{}}
		Expect(unconfigured(c, nil)).To(BeFalse())
	})
})

// requestJSON is here so the protocol's own encoding is exercised from this side too,
// not only from the child's.
var _ = Describe("the frames the emulator sends", func() {
	It("carries the target on every send, because the child keeps no state", func() {
		req := transportproto.Request{
			Op:     transportproto.OpSend,
			Target: map[string]any{"topicId": "orders"},
			Event:  "ORDER_CREATED",
			Data:   json.RawMessage(`{"a":1}`),
		}

		raw, err := json.Marshal(req)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(raw)).To(ContainSubstring(`"target":{"topicId":"orders"}`))
		Expect(string(raw)).To(ContainSubstring(`"data":{"a":1}`))
	})
})
