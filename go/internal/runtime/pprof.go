package runtime

import (
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"strings"
)

// PprofAddrEnvVar is the env var that controls the opt-in pprof endpoint.
// Operators set it to bind the runtime profiler; leaving it unset disables the
// endpoint entirely.
const PprofAddrEnvVar = "ESHU_PPROF_ADDR"

// pprofLoopbackHost is the default bind host when an operator supplies only a
// port. pprof exposes goroutine dumps, heap snapshots, and CPU profiles, so
// the default must not reach beyond the local host.
const pprofLoopbackHost = "127.0.0.1"

// NewPprofServer builds the opt-in pprof HTTP server for a runtime binary.
//
// When ESHU_PPROF_ADDR is unset or whitespace-only, the function returns
// (nil, nil); every caller must check for a nil *HTTPServer before calling
// Start, matching the precedent set by NewStatusMetricsServer.
//
// When the env value supplies only a port (":6060"), the bind host is forced
// to 127.0.0.1 so a typo or a habit picked up from public listeners does not
// silently expose profiling endpoints on a routable interface. Explicit hosts
// — including 0.0.0.0 — are preserved.
func NewPprofServer(getenv func(string) string) (*HTTPServer, error) {
	raw := getenv(PprofAddrEnvVar)
	addr, err := normalizePprofAddr(raw)
	if err != nil {
		return nil, err
	}
	if addr == "" {
		return nil, nil
	}

	return NewHTTPServer(HTTPServerConfig{
		Addr:    addr,
		Handler: newPprofHandler(),
	})
}

// normalizePprofAddr trims the env value, returns "" when disabled, and forces
// the loopback host when the operator supplied only a port. It rejects values
// that cannot be parsed as host:port so a typo fails fast at startup instead
// of producing a confusing listener error later.
func normalizePprofAddr(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}

	host, port, err := net.SplitHostPort(trimmed)
	if err != nil {
		return "", fmt.Errorf("parse %s=%q: %w", PprofAddrEnvVar, raw, err)
	}
	if host == "" {
		host = pprofLoopbackHost
	}
	return net.JoinHostPort(host, port), nil
}

// newPprofHandler builds a dedicated mux carrying the net/http/pprof named
// handlers. Importing net/http/pprof for its side effect would register the
// same routes on http.DefaultServeMux, leaking profiling endpoints onto any
// other server in the process that happened to use the default mux; an owned
// mux scopes exposure to the listener this helper returns.
func newPprofHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	return mux
}
