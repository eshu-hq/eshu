// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package intake

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/webhook"
)

// tracedLogger returns a production telemetry logger writing JSON lines into
// the returned buffer, so a test reads exactly what an operator's log pipeline
// would receive, including the trace_id and span_id the TraceHandler injects.
func tracedLogger(t *testing.T) (*bytes.Buffer, *slog.Logger) {
	t.Helper()
	bootstrap, err := telemetry.NewBootstrap("webhook-intake")
	if err != nil {
		t.Fatalf("NewBootstrap() error = %v", err)
	}
	var buf bytes.Buffer
	return &buf, telemetry.NewLoggerWithWriter(bootstrap, "webhook-intake", "webhook-intake", &buf)
}

// logRecord returns the first JSON log line whose message equals message.
func logRecord(t *testing.T, buf *bytes.Buffer, message string) map[string]any {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("log line %q is not JSON: %v", line, err)
		}
		if record["message"] == message {
			return record
		}
	}
	t.Fatalf("no log line with message %q in %q", message, buf.String())
	return nil
}

// TestIntakeLogsCarryTheHandleSpan pins the 3 AM join on all five routes: an
// intake log line written while a request is traced carries that request's
// webhook.handle trace_id and span_id, for both the stored and the store-failed
// outcome. A request with no tracer logs exactly as before, with no trace fields.
func TestIntakeLogsCarryTheHandleSpan(t *testing.T) {
	t.Parallel()

	for _, route := range intakeRoutes() {
		for _, tc := range []struct {
			name    string
			message string
			fail    bool
		}{
			{"stored", "intake trigger stored", false},
			{"store_failed", "intake store failed", true},
		} {
			t.Run(route.name+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				h := newSpanHarness()
				buf, logger := tracedLogger(t)
				stores := testStores()
				if tc.fail {
					route.failStore(stores)
				}
				mux := NewServer(ServerConfig{Token: "core-intake-token", Tracer: h.tracer, Logger: logger}, stores.stores()).Mux()

				postIntake(t, mux, route.path, route.payload)

				handle := onlySpan(t, h, spanHandle)
				record := logRecord(t, buf, tc.message)
				if got := record["trace_id"]; got != handle.SpanContext().TraceID().String() {
					t.Fatalf("log trace_id = %v, want the handle span's %s", got, handle.SpanContext().TraceID())
				}
				if got := record["span_id"]; got != handle.SpanContext().SpanID().String() {
					t.Fatalf("log span_id = %v, want the handle span's %s", got, handle.SpanContext().SpanID())
				}
			})
		}

		t.Run(route.name+"/nil_tracer", func(t *testing.T) {
			t.Parallel()
			buf, logger := tracedLogger(t)
			mux := NewServer(ServerConfig{Token: "core-intake-token", Logger: logger}, testStores().stores()).Mux()

			postIntake(t, mux, route.path, route.payload)

			record := logRecord(t, buf, "intake trigger stored")
			if _, ok := record["trace_id"]; ok {
				t.Fatalf("an untraced request logged trace_id %v, want none", record["trace_id"])
			}
		})
	}
}

// TestIntakeRejectionsEndTheHandleSpanOnce pins every pre-store rejection path
// (not only a bad bearer): one ended handle span with outcome rejected, the
// matching reason, and no store span.
func TestIntakeRejectionsEndTheHandleSpanOnce(t *testing.T) {
	t.Parallel()

	repo := intakeRoutes()[0]
	invalid := cloneRoute(repo.payload)
	invalid["event_kind"] = "bogus"
	for _, tc := range []struct {
		name, method, auth, reason string
		body                       []byte
		maxBody                    int64
	}{
		{"bad_method", http.MethodGet, "Bearer core-intake-token", "bad_method", marshalRoute(t, repo.payload), 0},
		{"body_too_large", http.MethodPost, "Bearer core-intake-token", "body_too_large", []byte(strings.Repeat("x", 65)), 64},
		{"empty_body", http.MethodPost, "Bearer core-intake-token", "empty_body", nil, 0},
		{"invalid_json", http.MethodPost, "Bearer core-intake-token", "malformed_event", []byte("{not json"), 0},
		{"invalid_trigger", http.MethodPost, "Bearer core-intake-token", "invalid_trigger", marshalRoute(t, invalid), 0},
		{"wrong_bearer", http.MethodPost, "Bearer nope", "auth_failed", marshalRoute(t, repo.payload), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := newSpanHarness()
			stores := testStores()
			mux := NewServer(ServerConfig{Token: "core-intake-token", Tracer: h.tracer, MaxBodyBytes: tc.maxBody}, stores.stores()).Mux()

			rec := rawIntake(mux, tc.method, repo.path, tc.auth, tc.body)
			if rec.Code < 400 || rec.Code >= 500 {
				t.Fatalf("rejection = %d, want 4xx", rec.Code)
			}

			attrs := spanAttrs(onlySpan(t, h, spanHandle))
			if attrs["outcome"] != "rejected" || attrs["reason"] != tc.reason {
				t.Fatalf("handle outcome/reason = %q/%q, want rejected/%s", attrs["outcome"], attrs["reason"], tc.reason)
			}
			if got := len(h.named(spanStore)); got != 0 {
				t.Fatalf("store spans = %d, want 0", got)
			}
			if stores.repository.calls != 0 {
				t.Fatalf("store calls = %d, want 0", stores.repository.calls)
			}
		})
	}
}

// panicRepositoryStore panics inside the store call, as a driver bug would.
type panicRepositoryStore struct{}

func (panicRepositoryStore) StoreTrigger(context.Context, webhook.Trigger, time.Time) (webhook.StoredTrigger, error) {
	panic("driver bug")
}

// TestIntakeStorePanicStillEndsBothSpans pins that a panic inside the store
// call cannot leave webhook.store or webhook.handle open: net/http recovers
// the panic after the handler's deferred finish has run.
func TestIntakeStorePanicStillEndsBothSpans(t *testing.T) {
	t.Parallel()

	h := newSpanHarness()
	stores := testStores().stores()
	stores.Repository = panicRepositoryStore{}
	mux := h.server(stores).Mux()
	route := intakeRoutes()[0]

	func() {
		defer func() { _ = recover() }()
		postIntake(t, mux, route.path, route.payload)
	}()

	store := onlySpan(t, h, spanStore)
	if got := spanAttrs(store)["outcome"]; got != "failed" {
		t.Fatalf("store outcome = %q, want failed for a panicking store", got)
	}
	onlySpan(t, h, spanHandle)
}

// TestIntakeStoreContextKeepsRequestCancellation pins that running the store
// under the span context does not detach it from the request: a cancelled
// request reaches the store already cancelled.
func TestIntakeStoreContextKeepsRequestCancellation(t *testing.T) {
	t.Parallel()

	h := newSpanHarness()
	stores := testStores()
	mux := h.server(stores.stores()).Mux()
	route := intakeRoutes()[0]

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodPost, route.path, bytes.NewReader(marshalRoute(t, route.payload))).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer core-intake-token")
	mux.ServeHTTP(httptest.NewRecorder(), req)

	if stores.repository.ctx == nil || stores.repository.ctx.Err() == nil {
		t.Fatal("store context is not cancelled, want the request's cancellation to reach the store")
	}
}
