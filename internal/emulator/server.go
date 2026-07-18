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
}

// defaultHost is the loopback interface the emulator binds unless told otherwise.
const defaultHost = "127.0.0.1"

// Server is a running emulator: a Fiber app answering the whole API surface from one
// in-memory store.
type Server struct {
	app  *fiber.App
	addr string
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

	registerRoutes(app, ops, &handlers{store: store})

	if cfg.Seed != "" {
		if err := seed(store, cfg.Seed); err != nil {
			return nil, fmt.Errorf("seed the emulator: %w", err)
		}
	}

	host := cfg.Host
	if host == "" {
		host = defaultHost
	}

	return &Server{app: app, addr: net.JoinHostPort(host, strconv.Itoa(cfg.Port))}, nil
}

// Listen serves until ctx is cancelled, then shuts down gracefully and returns nil.
// It returns a non-nil error only when it could not listen at all (a taken port).
//
// ctx is the command's context, which the root cancels on SIGINT/SIGTERM — so Ctrl-C
// drains the server and exits 0.
func (s *Server) Listen(ctx context.Context) error {
	errc := make(chan error, 1)
	go func() { errc <- s.app.Listen(s.addr) }()

	select {
	case <-ctx.Done():
		return s.app.ShutdownWithTimeout(shutdownGrace)
	case err := <-errc:
		return err
	}
}
