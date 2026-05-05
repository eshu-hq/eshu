package runtime

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const defaultHTTPServerShutdownTimeout = 5 * time.Second

// HTTPServerConfig configures a shared runtime-owned HTTP server lifecycle.
type HTTPServerConfig struct {
	Addr            string
	Handler         http.Handler
	ShutdownTimeout time.Duration
}

// HTTPServer owns one runtime-mounted HTTP server and graceful shutdown path.
type HTTPServer struct {
	mu       sync.Mutex
	config   HTTPServerConfig
	listener net.Listener
	server   *http.Server
}

// NewHTTPServer validates and freezes the shared HTTP server lifecycle config.
func NewHTTPServer(cfg HTTPServerConfig) (*HTTPServer, error) {
	cfg.Addr = strings.TrimSpace(cfg.Addr)
	if cfg.Addr == "" {
		return nil, fmt.Errorf("http server addr is required")
	}
	if cfg.Handler == nil {
		return nil, fmt.Errorf("http server handler is required")
	}
	if cfg.ShutdownTimeout <= 0 {
		cfg.ShutdownTimeout = defaultHTTPServerShutdownTimeout
	}

	return &HTTPServer{config: cfg}, nil
}

// Start opens the shared runtime HTTP listener and begins serving in the
// background.
func (s *HTTPServer) Start(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.server != nil {
		return nil
	}

	listener, err := net.Listen("tcp", s.config.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.config.Addr, err)
	}

	server := &http.Server{Handler: s.config.Handler}
	s.listener = listener
	s.server = server

	go func() {
		_ = server.Serve(listener)
	}()

	return nil
}

// Stop gracefully shuts down the shared runtime HTTP server.
func (s *HTTPServer) Stop(ctx context.Context) error {
	s.mu.Lock()
	server := s.server
	listener := s.listener
	s.server = nil
	s.listener = nil
	timeout := s.config.ShutdownTimeout
	s.mu.Unlock()

	if server == nil {
		return nil
	}

	shutdownCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	err := server.Shutdown(shutdownCtx)
	if listener != nil {
		_ = listener.Close()
	}
	return err
}

// Addr returns the bound address after Start.
func (s *HTTPServer) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.listener != nil {
		return s.listener.Addr().String()
	}

	return s.config.Addr
}
