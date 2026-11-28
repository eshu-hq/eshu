// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/apilib"
)

// serveApilibVerdict serves one GET /readyz from the apilib mount with the
// given fixture probes. The core side is never routed through apilib: this
// is a test-only pin, and the comparison target below calls core
// writeReadiness directly.
func serveApilibVerdict(t *testing.T, service string, postgres, graph apilib.ReadinessProbe) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	apilib.MountReadiness(mux, service, postgres, graph)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	return rec
}

// serveCoreVerdict runs the same verdict inputs through core writeReadiness.
func serveCoreVerdict(t *testing.T, service string, err error) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	writeReadiness(rec, service, err)
	return rec
}

// requireVerdictParity pins the two writers byte-for-byte: same status, same
// bare application/json Content-Type, identical body bytes (hence the same
// {service,probe,status,error?} shape), and no Retry-After on either side.
// Any drift on either side — a charset on the Content-Type, a renamed field,
// a reordered struct — fails here.
func requireVerdictParity(t *testing.T, apilibRec, coreRec *httptest.ResponseRecorder) {
	t.Helper()
	if apilibRec.Code != coreRec.Code {
		t.Fatalf("status: apilib = %d, core = %d", apilibRec.Code, coreRec.Code)
	}
	if got, want := apilibRec.Header().Get("Content-Type"), coreRec.Header().Get("Content-Type"); got != want {
		t.Fatalf("Content-Type: apilib = %q, core = %q", got, want)
	}
	if got := apilibRec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want bare application/json", got)
	}
	if apilibRec.Body.String() != coreRec.Body.String() {
		t.Fatalf("body differs:\n apilib: %s\n core:   %s", apilibRec.Body.String(), coreRec.Body.String())
	}
	for _, rec := range []*httptest.ResponseRecorder{apilibRec, coreRec} {
		if got := rec.Header().Get("Retry-After"); got != "" {
			t.Fatalf("Retry-After = %q, want empty", got)
		}
	}
}

// decodeVerdictBody decodes a /readyz body into a field map.
func decodeVerdictBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("readyz body is not JSON: %v", err)
	}
	return body
}

func okReadinessProbe() apilib.ReadinessProbe {
	return func(context.Context) error { return nil }
}

// TestApilibReadyzParityBothOK pins the both-up 200: same status, header,
// and {service,probe,status} shape with no error key on either side.
func TestApilibReadyzParityBothOK(t *testing.T) {
	t.Parallel()
	const service = "eshu-core-api"
	apilibRec := serveApilibVerdict(t, service, okReadinessProbe(), okReadinessProbe())
	coreRec := serveCoreVerdict(t, service, nil)
	requireVerdictParity(t, apilibRec, coreRec)
	if apilibRec.Code != http.StatusOK {
		t.Fatalf("readyz both-ok = %d, want 200", apilibRec.Code)
	}
	body := decodeVerdictBody(t, apilibRec)
	if body["service"] != service || body["probe"] != "readyz" || body["status"] != "ok" {
		t.Fatalf("readyz both-ok shape = %v, want {service,probe:readyz,status:ok}", body)
	}
	if _, ok := body["error"]; ok {
		t.Fatalf("readyz both-ok carries error key: %v", body)
	}
}

// TestApilibReadyzParityNotConfigured pins both not-configured cause strings:
// a nil probe on either side answers the same 503 the core writer produces
// for that backend's fixed cause. Only one side is nil per case so the
// reported cause is deterministic.
func TestApilibReadyzParityNotConfigured(t *testing.T) {
	t.Parallel()
	const service = "eshu-core-api"
	cases := []struct {
		name     string
		postgres apilib.ReadinessProbe
		graph    apilib.ReadinessProbe
		coreErr  error
	}{
		{"postgres", nil, okReadinessProbe(), errPostgresNotConfigured},
		{"graph", okReadinessProbe(), nil, errGraphNotConfigured},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			apilibRec := serveApilibVerdict(t, service, tc.postgres, tc.graph)
			coreRec := serveCoreVerdict(t, service, tc.coreErr)
			requireVerdictParity(t, apilibRec, coreRec)
			if apilibRec.Code != http.StatusServiceUnavailable {
				t.Fatalf("readyz nil-%s = %d, want 503", tc.name, apilibRec.Code)
			}
			if got := decodeVerdictBody(t, apilibRec)["error"]; got != tc.coreErr.Error() {
				t.Fatalf("readyz nil-%s error = %q, want core cause %q", tc.name, got, tc.coreErr.Error())
			}
		})
	}
}

// TestApilibReadyzParityProbeError pins a backend failure verbatim: the
// served error text matches on both sides with a 503.
func TestApilibReadyzParityProbeError(t *testing.T) {
	t.Parallel()
	const service = "eshu-core-api"
	probeErr := errors.New("backend down")
	failing := apilib.ReadinessProbe(func(context.Context) error { return probeErr })
	apilibRec := serveApilibVerdict(t, service, failing, okReadinessProbe())
	coreRec := serveCoreVerdict(t, service, probeErr)
	requireVerdictParity(t, apilibRec, coreRec)
	if apilibRec.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz probe-error = %d, want 503", apilibRec.Code)
	}
}

// TestApilibReadyzParityProbeTimeout pins the 2s probe bound on both sides:
// core's timeout constant stays 2s, and the context apilib hands each probe
// carries a deadline ~2s out. A drift to 1s or 5s on either side fails.
func TestApilibReadyzParityProbeTimeout(t *testing.T) {
	t.Parallel()
	if coreReadinessProbeTimeout != 2*time.Second {
		t.Fatalf("coreReadinessProbeTimeout = %v, want 2s", coreReadinessProbeTimeout)
	}
	deadlineSaw := make(chan time.Time, 1)
	observing := apilib.ReadinessProbe(func(ctx context.Context) error {
		deadline, ok := ctx.Deadline()
		if !ok {
			return errors.New("probe context has no deadline")
		}
		deadlineSaw <- deadline
		return nil
	})
	if rec := serveApilibVerdict(t, "eshu-core-api", observing, okReadinessProbe()); rec.Code != http.StatusOK {
		t.Fatalf("readyz observing-probe = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	remaining := time.Until(<-deadlineSaw)
	if remaining <= 1500*time.Millisecond || remaining > 2*time.Second {
		t.Fatalf("probe deadline in %v, want (1500ms, 2s]", remaining)
	}
}

// TestApilibReadyzProbesRunConcurrently pins the fan-out semantics: the
// postgres probe waits for proof the graph probe started, so sequential
// execution would stall past the wait budget and fail the verdict instead
// of answering 200. The slow-probe case proves the verdict still waits for
// — and runs — both probes when one fails fast.
func TestApilibReadyzProbesRunConcurrently(t *testing.T) {
	t.Parallel()
	const service = "eshu-core-api"
	t.Run("waiter", func(t *testing.T) {
		t.Parallel()
		var postgresRan, graphRan atomic.Bool
		graphStarted := make(chan struct{})
		postgres := apilib.ReadinessProbe(func(ctx context.Context) error {
			postgresRan.Store(true)
			select {
			case <-graphStarted:
				return nil
			case <-ctx.Done():
				return errors.New("graph probe never started concurrently")
			}
		})
		graph := apilib.ReadinessProbe(func(context.Context) error {
			graphRan.Store(true)
			close(graphStarted)
			return nil
		})
		rec := serveApilibVerdict(t, service, postgres, graph)
		if rec.Code != http.StatusOK {
			t.Fatalf("readyz concurrent = %d, want 200: %s", rec.Code, rec.Body.String())
		}
		if !postgresRan.Load() || !graphRan.Load() {
			t.Fatalf("probes ran: postgres=%v graph=%v, want both true", postgresRan.Load(), graphRan.Load())
		}
	})
	t.Run("slow probe still runs on fast failure", func(t *testing.T) {
		t.Parallel()
		sentinel := errors.New("postgres unreachable")
		var graphRan atomic.Bool
		postgres := apilib.ReadinessProbe(func(context.Context) error { return sentinel })
		graph := apilib.ReadinessProbe(func(context.Context) error {
			time.Sleep(100 * time.Millisecond)
			graphRan.Store(true)
			return nil
		})
		apilibRec := serveApilibVerdict(t, service, postgres, graph)
		coreRec := serveCoreVerdict(t, service, sentinel)
		requireVerdictParity(t, apilibRec, coreRec)
		if apilibRec.Code != http.StatusServiceUnavailable {
			t.Fatalf("readyz fast-failure = %d, want 503", apilibRec.Code)
		}
		if !graphRan.Load() {
			t.Fatal("graph probe did not run: verdict returned on the fast failure alone")
		}
	})
}
