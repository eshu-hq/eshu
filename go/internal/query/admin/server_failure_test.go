// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// adminFailureCanary stands in for backend error text: a connection setting,
// a host, and a relation name that must never reach a response body (#7674).
const adminFailureCanary = "pq: password=hunter2 host=db.internal.example relation fact_work_items"

// adminFailureKind is one way an admin route step fails.
type adminFailureKind int

const (
	// adminFailureBackend is a store or recovery fault whose text carries the
	// canary: 500 with the step's fixed message and an Error span.
	adminFailureBackend adminFailureKind = iota
	// adminFailureCancel is a client cancel: the request context is canceled
	// and the error wraps context.Canceled. It answers 499 with the fixed
	// message, an Unset span, and only the client-cancel event.
	adminFailureCancel
	// adminFailureStale is a stale PostgreSQL reader: 503 backend_unavailable
	// with Retry-After. Only steps that read through the guarded reader get it.
	adminFailureStale
	// adminFailureTimeout is the route's own read budget expiring: 504 with the
	// route's fixed timeout message. Only bounded inspection reads get it. No
	// admin step meets querycontract.ErrGraphReadDeadline: the guarded
	// PostgreSQL reader never returns it, so it has no kind here and no admin
	// route outside the three bounded reads declares 504.
	adminFailureTimeout
)

func (k adminFailureKind) String() string {
	return [...]string{"backend failure", "client cancel", "reader stale", "route timeout"}[k]
}

// adminFailureError builds the error a failing step returns for kind.
func adminFailureError(kind adminFailureKind) error {
	switch kind {
	case adminFailureCancel:
		return fmt.Errorf("%s: %w", adminFailureCanary, context.Canceled)
	case adminFailureStale:
		// The guarded reader joins the stale sentinel with the replay-window
		// deadline (runtime/postgres reader.go), so a route that checks its
		// own timeout before the reader verdict would misreport it as 504.
		return fmt.Errorf("%s: %w", adminFailureCanary, errors.Join(db.ErrReaderStale, context.DeadlineExceeded))
	case adminFailureTimeout:
		return fmt.Errorf("%s: %w", adminFailureCanary, context.DeadlineExceeded)
	default:
		return errors.New(adminFailureCanary)
	}
}

// adminFailureStep is one admin route step that answers a server failure,
// and the handler that fails exactly that step with the given error.
type adminFailureStep struct {
	name    string
	path    string
	body    string
	message string
	// reader marks a step that reads through the guarded PostgreSQL reader,
	// so a stale reader answers the shared retryable 503.
	reader bool
	// timeoutMessage, when set, is the step's own 504 text for its route
	// budget expiring.
	timeoutMessage string
	// finalize marks a post-commit idempotency finalize. It runs on a context
	// detached from the request's cancel, so it has no 499 variant here: a
	// client cancel there is covered by the finalize_test.go tables.
	finalize bool
	handler  func(err error) *Handler
}

// runAdminFailureSteps drives every step through every failure kind it can
// meet. Each case builds its own tracer provider, so the cases run in
// parallel.
func runAdminFailureSteps(t *testing.T, steps []adminFailureStep) {
	t.Helper()
	for _, step := range steps {
		kinds := []adminFailureKind{adminFailureBackend}
		if !step.finalize {
			kinds = append(kinds, adminFailureCancel)
		}
		if step.reader {
			kinds = append(kinds, adminFailureStale)
		}
		if step.timeoutMessage != "" {
			kinds = append(kinds, adminFailureTimeout)
		}
		for _, kind := range kinds {
			t.Run(step.name+"/"+kind.String(), func(t *testing.T) {
				t.Parallel()
				rec, ended := serveAdminTraced(t, step.handler(adminFailureError(kind)), step.path, step.body, kind == adminFailureCancel)
				assertAdminFailure(t, step, kind, rec, ended)
			})
		}
	}
}

// serveAdminTraced serves one POST under a recording request span and
// returns the response and every ended span. cancel cancels the request
// context before the handler runs.
func serveAdminTraced(
	t *testing.T, handler *Handler, path, body string, cancel bool,
) (*httptest.ResponseRecorder, []sdktrace.ReadOnlySpan) {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	ctx, parent := provider.Tracer("admin-server-failure-test").Start(context.Background(), "request")
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	if cancel {
		stop()
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	parent.End()
	return rec, recorder.Ended()
}

// assertAdminFailure checks the status, the fixed body, and the request span
// for one failure kind.
func assertAdminFailure(
	t *testing.T, step adminFailureStep, kind adminFailureKind, rec *httptest.ResponseRecorder, ended []sdktrace.ReadOnlySpan,
) {
	t.Helper()
	body := rec.Body.String()
	for _, fragment := range []string{"hunter2", "db.internal.example", "fact_work_items", "pq:"} {
		if strings.Contains(body, fragment) {
			t.Fatalf("body leaked backend error text %q: %s", fragment, body)
		}
	}
	wantStatus, wantMessage := http.StatusInternalServerError, step.message
	switch kind {
	case adminFailureCancel:
		wantStatus = querycontract.StatusClientClosedRequest
	case adminFailureStale:
		wantStatus = http.StatusServiceUnavailable
	case adminFailureTimeout:
		wantStatus, wantMessage = http.StatusGatewayTimeout, step.timeoutMessage
	}
	if rec.Code != wantStatus {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, wantStatus, body)
	}
	if kind == adminFailureStale {
		if rec.Header().Get("Retry-After") == "" {
			t.Fatalf("reader fence answer has no Retry-After; body = %s", body)
		}
		if got := adminFailureBodyMessage(t, rec); got != querycontract.ErrReaderRetryable.Error() {
			t.Fatalf("reader fence message = %q, want %q", got, querycontract.ErrReaderRetryable.Error())
		}
		return
	}
	if got := adminFailureBodyMessage(t, rec); got != wantMessage {
		t.Fatalf("body message = %q, want the fixed %q; body = %s", got, wantMessage, body)
	}
	assertAdminFailureSpan(t, wantMessage, kind == adminFailureCancel, ended)
}

// adminFailureBodyMessage reads the client-facing message from a plain
// {"error","detail"} body or from a {data, error} envelope.
func adminFailureBodyMessage(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Detail string          `json:"detail"`
		Error  json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v; body = %s", err, rec.Body.String())
	}
	var envelope querycontract.ErrorEnvelope
	if len(body.Error) > 0 && body.Error[0] == '{' && json.Unmarshal(body.Error, &envelope) == nil {
		return envelope.Message
	}
	return body.Detail
}

// assertAdminFailureSpan checks the request span the failure marked. A
// client cancel leaves the status Unset with only the cancel event; a server
// fault sets Error with message as its description and records exactly one
// exception.
func assertAdminFailureSpan(t *testing.T, message string, cancel bool, ended []sdktrace.ReadOnlySpan) {
	t.Helper()
	var span sdktrace.ReadOnlySpan
	for _, candidate := range ended {
		if candidate.Name() == "request" {
			span = candidate
		}
	}
	if span == nil {
		t.Fatal("request span not recorded")
	}
	exceptions, hasCanceled := 0, false
	for _, event := range span.Events() {
		if event.Name == "exception" {
			exceptions++
		}
		hasCanceled = hasCanceled || event.Name == tracing.ClientCanceledEvent
	}
	status := span.Status()
	if cancel {
		if status.Code != codes.Unset || exceptions != 0 || !hasCanceled {
			t.Fatalf("cancel span status = %v, exceptions = %d, %s = %v; want Unset, 0, true",
				status.Code, exceptions, tracing.ClientCanceledEvent, hasCanceled)
		}
		return
	}
	if status.Code != codes.Error || status.Description != message || exceptions != 1 || hasCanceled {
		t.Fatalf("fault span status = %v (%q), exceptions = %d, canceled event = %v; want Error (%q), 1, false",
			status.Code, status.Description, exceptions, hasCanceled, message)
	}
}

// TestPoisonedLinkChildSpanSkipsClientCancel pins that the changed-since
// poisoned-links child span records a backend fault but not a client cancel,
// matching the request span (#7674). It swaps the global tracer provider, so
// it must not be parallel.
func TestPoisonedLinkChildSpanSkipsClientCancel(t *testing.T) {
	for _, tc := range []struct {
		kind           adminFailureKind
		wantExceptions int
	}{
		{adminFailureBackend, 1},
		{adminFailureCancel, 0},
	} {
		t.Run(tc.kind.String(), func(t *testing.T) {
			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			previous := otel.GetTracerProvider()
			otel.SetTracerProvider(provider)
			t.Cleanup(func() {
				otel.SetTracerProvider(previous)
				_ = provider.Shutdown(context.Background())
			})

			handler := &Handler{Store: &stubAdminStore{changedSincePoisonedLinkErr: adminFailureError(tc.kind)}}
			_, _ = serveAdminTraced(t, handler, "/api/v0/admin/changed-since/poisoned-links/query",
				`{"limit":10,"timeout_ms":5000}`, tc.kind == adminFailureCancel)
			exceptions, found := 0, false
			for _, span := range recorder.Ended() {
				if span.Name() != "query.changed_since_poisoned_links" {
					continue
				}
				found = true
				for _, event := range span.Events() {
					if event.Name == "exception" {
						exceptions++
					}
				}
			}
			if !found {
				t.Fatal("query.changed_since_poisoned_links span not recorded")
			}
			if exceptions != tc.wantExceptions {
				t.Fatalf("child span exceptions = %d, want %d", exceptions, tc.wantExceptions)
			}
		})
	}
}
