// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package apilib

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

// readinessProbeTimeout bounds each backend check behind /readyz. The probe
// answers whether the process can serve traffic, not whether a full query
// round-trip would finish, so each check stays far below the satellite
// relay bound (30 s).
const readinessProbeTimeout = 2 * time.Second

// Not-configured probe causes: a nil probe means the process was built
// without that backend handle. The cause text is fixed so a missing handle
// never leaks wiring detail.
var (
	errReadinessPostgresNotConfigured = errors.New("postgres: handle not configured")
	errReadinessGraphNotConfigured    = errors.New("graph: driver not configured")
)

// ReadinessProbe is one backend check behind /readyz: postgres pings the
// database handle, graph verifies pinned-Neo4j connectivity.
//
// The returned error is served verbatim in the 503 body, so a probe MUST
// return only pre-sanitized errors: fixed cause strings, never driver text,
// SQL, hosts, or connection settings. A nil probe is never called; it
// reports the fixed not-configured cause instead.
type ReadinessProbe func(ctx context.Context) error

// readinessCheckResult is the JSON body of the backend-gated /readyz probe:
// the core shape {service, probe: readyz, status: ok|error, error?}.
type readinessCheckResult struct {
	Service string `json:"service"`
	Probe   string `json:"probe"`
	Status  string `json:"status"`
	Error   string `json:"error,omitempty"`
}

// writeReadinessVerdict writes the /readyz verdict: 200 only when every
// dependency check passes, otherwise the permanent 503 with no Retry-After.
// The 503 is permanent (never retryable): a caller must fix the backend, not
// retry.
func writeReadinessVerdict(w http.ResponseWriter, service string, err error) {
	body := readinessCheckResult{Service: service, Probe: "readyz", Status: "ok"}
	status := http.StatusOK
	if err != nil {
		status = http.StatusServiceUnavailable
		body.Status = "error"
		body.Error = err.Error()
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// checkReadiness runs both probes concurrently, each under its own timeout,
// and returns the first failure. Both must answer: a process-liveness 200
// without live backends is non-compliant.
func checkReadiness(ctx context.Context, postgres, graph ReadinessProbe) error {
	probes := []struct {
		probe ReadinessProbe
		cause error
	}{
		{postgres, errReadinessPostgresNotConfigured},
		{graph, errReadinessGraphNotConfigured},
	}
	results := make(chan error, len(probes))
	for _, p := range probes {
		go func() {
			if p.probe == nil {
				results <- p.cause
				return
			}
			probeCtx, cancel := context.WithTimeout(ctx, readinessProbeTimeout)
			defer cancel()
			results <- p.probe(probeCtx)
		}()
	}
	var first error
	for range probes {
		if err := <-results; err != nil && first == nil {
			first = err
		}
	}
	return first
}

// MountReadiness registers the backend-gated /readyz probe. It stays public
// and never sets Retry-After on either verdict. The mux MUST be wrapped by
// the caller's auth chain; this mount does no auth itself.
func MountReadiness(mux *http.ServeMux, service string, postgres, graph ReadinessProbe) {
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		writeReadinessVerdict(w, service, checkReadiness(r.Context(), postgres, graph))
	})
}
