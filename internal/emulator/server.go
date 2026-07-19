package emulator

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/Joessst-Dev/fft-cli/internal/api"
)

// shutdownGrace is how long a cancelled server waits for in-flight requests to
// finish before it stops. A local emulator has no long requests; this is only so a
// Ctrl-C is clean rather than abrupt.
const shutdownGrace = 5 * time.Second

// Config configures a Server.
type Config struct {
	// Host is the interface to bind. It defaults to loopback (127.0.0.1): the emulator
	// authenticates nothing, so it must not be reachable off the machine unless the
	// user deliberately widens it (a container that maps the port needs 0.0.0.0).
	Host string

	// Port is the TCP port to listen on.
	Port int

	// Seed is a directory of JSON fixtures to preload, or "" for an empty tenant.
	Seed string

	// Verbose logs one line per request to Log.
	Verbose bool

	// Log is where request logs and startup notices go — stderr, so stdout stays the
	// data contract even here.
	Log io.Writer

	// PubSubHost is the local Pub/Sub emulator to publish events to (the standard
	// PUBSUB_EMULATOR_HOST value). Empty disables eventing: subscriptions are still
	// stored and matched, but nothing is published. It is never a real Google Cloud
	// endpoint — the publisher pins every connection to this host with auth disabled.
	PubSubHost string

	// publisher overrides the Pub/Sub publisher. It exists for tests, which inject a
	// recorder in place of a real emulator connection; production leaves it nil and
	// New builds one from PubSubHost.
	publisher Publisher
}

// defaultHost is the loopback interface the emulator binds unless told otherwise.
const defaultHost = "127.0.0.1"

// Server is a running emulator: a Fiber app answering the whole API surface from one
// in-memory store.
type Server struct {
	app    *fiber.App
	addr   string
	events *eventEmitter
}

// New builds a server: it registers every operation in the spec, wires the store,
// and preloads any seed data. It does not listen yet.
func New(cfg Config) (*Server, error) {
	ops := api.Operations()
	store := NewStore(inferCollections(ops))

	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	if cfg.Verbose {
		app.Use(requestLogger(cfg.Log))
	}
	app.Use(permissiveAuth())

	events := newEventEmitter(cfg, store)
	registerRoutes(app, ops, &handlers{store: store, events: events})

	if cfg.Seed != "" {
		if err := seed(store, cfg.Seed); err != nil {
			return nil, fmt.Errorf("seed the emulator: %w", err)
		}
	}

	host := cfg.Host
	if host == "" {
		host = defaultHost
	}

	return &Server{app: app, addr: net.JoinHostPort(host, strconv.Itoa(cfg.Port)), events: events}, nil
}

// Listen binds the port and serves until ctx is cancelled, then shuts down
// gracefully and returns nil. It returns a non-nil error when the port could not be
// bound (e.g. already taken) or the server otherwise failed.
//
// ready, if non-nil, is called the moment the port is bound — before Listen starts
// blocking to serve — so a caller can print a "point fft here" recipe only once it is
// actually true, instead of racing a bind failure with a recipe that looks like it
// worked.
//
// ctx is the command's context, which the root cancels on SIGINT/SIGTERM — so Ctrl-C
// drains the server and exits 0.
func (s *Server) Listen(ctx context.Context, ready func()) error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	// Once the port is bound the emitter's clients may be dialed, so release them on
	// every exit path — a long-running session must not leak the Pub/Sub connections.
	defer func() { _ = s.events.Close() }()
	if ready != nil {
		ready()
	}

	errc := make(chan error, 1)
	go func() { errc <- s.app.Listener(ln) }()

	select {
	case <-ctx.Done():
		return s.app.ShutdownWithTimeout(shutdownGrace)
	case err := <-errc:
		return err
	}
}
