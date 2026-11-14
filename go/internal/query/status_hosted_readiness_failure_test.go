// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// hostedReadinessFailureRoute is the hosted readiness snapshot-read failure
// site for #10658: the status snapshot read fails with the case error.
func hostedReadinessFailureRoute() failure.Route {
	return failure.Route{
		Name: "hosted readiness", Path: "/api/v0/status/hosted-readiness",
		Message: hostedReadinessStatusReadFailedMessage,
		Handler: func(err error) failure.Mounter {
			return &StatusHandler{StatusReader: fakeStatusReader{err: err}, LiveActivity: &fakeLiveActivityReader{}}
		},
	}
}

// serveHostedReadinessFailure serves one hosted readiness request whose
// snapshot read fails with err, negotiating the envelope when envelope is
// true, and returns the response. It is failure.Serve without the forced
// envelope negotiation, so the plain-shape contract stays pinned too.
func serveHostedReadinessFailure(t *testing.T, err error, envelope bool) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	(&StatusHandler{StatusReader: fakeStatusReader{err: err}, LiveActivity: &fakeLiveActivityReader{}}).Mount(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/v0/status/hosted-readiness", nil)
	if envelope {
		req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// decodeHostedReadinessErrorEnvelope decodes rec's body as the negotiated
// error envelope and returns its error section.
func decodeHostedReadinessErrorEnvelope(t *testing.T, rec *httptest.ResponseRecorder) *querycontract.ErrorEnvelope {
	t.Helper()
	var envelope querycontract.ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode error envelope: %v; body = %s", err, rec.Body.String())
	}
	if envelope.Error == nil {
		t.Fatalf("body has no error envelope; body = %s", rec.Body.String())
	}
	return envelope.Error
}

// TestHostedReadinessFaultAnswersEnvelopeWithCodeAndCapability is the #10658
// regression for the bare-500 body: a status reader that returns a plain
// error must answer the shared failure envelope with a stable code and the
// capability name, never backend error text.
func TestHostedReadinessFaultAnswersEnvelopeWithCodeAndCapability(t *testing.T) {
	t.Parallel()

	route := hostedReadinessFailureRoute()
	rec, _ := failure.Serve(failure.Suite{}, route, route.Handler(errors.New("backend: "+failure.Canary)), false)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body = %s", rec.Code, rec.Body.String())
	}
	errEnv := decodeHostedReadinessErrorEnvelope(t, rec)
	if errEnv.Code != querycontract.ErrorCodeInternalError {
		t.Errorf("error.code = %q, want %q; body = %s", errEnv.Code, querycontract.ErrorCodeInternalError, rec.Body.String())
	}
	if errEnv.Capability != hostedReadinessStatusCapability {
		t.Errorf("error.capability = %q, want %q; body = %s", errEnv.Capability, hostedReadinessStatusCapability, rec.Body.String())
	}
	if errEnv.Message != hostedReadinessStatusReadFailedMessage {
		t.Errorf("error.message = %q, want %q; body = %s", errEnv.Message, hostedReadinessStatusReadFailedMessage, rec.Body.String())
	}
}

// TestHostedReadinessFaultPlainShapeKeepsFixedDetail pins the plain-shape
// half of the #10658 contract: a caller that does not negotiate the envelope
// still answers the fixed message with no backend error text.
func TestHostedReadinessFaultPlainShapeKeepsFixedDetail(t *testing.T) {
	t.Parallel()

	rec := serveHostedReadinessFailure(t, errors.New("backend: "+failure.Canary), false)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode plain body: %v; body = %s", err, rec.Body.String())
	}
	if body.Detail != hostedReadinessStatusReadFailedMessage {
		t.Errorf("detail = %q, want %q; body = %s", body.Detail, hostedReadinessStatusReadFailedMessage, rec.Body.String())
	}
}

// TestHostedReadinessSnapshotBudgetTimeoutAnswersRetryable503 is the #10658
// regression for a snapshot read that outruns its bounded budget: the
// guarded reader surfaces a bare context.DeadlineExceeded with no fence
// sentinel, so the shared mapper cannot see it. The route must still answer
// the retryable 503 with Retry-After instead of a 500.
func TestHostedReadinessSnapshotBudgetTimeoutAnswersRetryable503(t *testing.T) {
	t.Parallel()

	const marker = "budget-timeout-probe-marker"
	timeout := fmt.Errorf("read status snapshot: read active work summary %s: %w", marker, context.DeadlineExceeded)

	t.Run("envelope", func(t *testing.T) {
		t.Parallel()
		rec := serveHostedReadinessFailure(t, timeout, true)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503; body = %s", rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("Retry-After"); got != strconv.Itoa(querycontract.BackendUnavailableRetryAfterSeconds) {
			t.Errorf("Retry-After = %q, want %q; body = %s", got, strconv.Itoa(querycontract.BackendUnavailableRetryAfterSeconds), rec.Body.String())
		}
		errEnv := decodeHostedReadinessErrorEnvelope(t, rec)
		if errEnv.Code != querycontract.ErrorCodeBackendUnavailable {
			t.Errorf("error.code = %q, want %q; body = %s", errEnv.Code, querycontract.ErrorCodeBackendUnavailable, rec.Body.String())
		}
		if errEnv.Capability != hostedReadinessStatusCapability {
			t.Errorf("error.capability = %q, want %q; body = %s", errEnv.Capability, hostedReadinessStatusCapability, rec.Body.String())
		}
		for _, leaked := range []string{marker, "context deadline exceeded"} {
			if strings.Contains(rec.Body.String(), leaked) {
				t.Errorf("body leaks backend error text %q; body = %s", leaked, rec.Body.String())
			}
		}
	})

	t.Run("plain", func(t *testing.T) {
		t.Parallel()
		rec := serveHostedReadinessFailure(t, timeout, false)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503; body = %s", rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("Retry-After"); got == "" {
			t.Errorf("plain 503 carries no Retry-After; body = %s", rec.Body.String())
		}
		var body struct {
			Detail string `json:"detail"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode plain body: %v; body = %s", err, rec.Body.String())
		}
		if body.Detail != querycontract.ErrReaderRetryable.Error() {
			t.Errorf("detail = %q, want %q; body = %s", body.Detail, querycontract.ErrReaderRetryable.Error(), rec.Body.String())
		}
	})
}

// TestHostedReadinessReadFailureLogsFaultOnly is the #10658 regression for
// the missing log line: a failed snapshot read writes one ERROR line with
// the wrapped error and the request id. A client cancel, a mapped fence
// verdict, and a budget-timeout 503 write no such line. Not parallel: it
// swaps the default slog logger.
func TestHostedReadinessReadFailureLogsFaultOnly(t *testing.T) {
	logger, records := newRecordingLogger()
	previous := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(previous) })

	route := hostedReadinessFailureRoute()
	const marker = "snapshot-read-probe-marker"
	cases := []struct {
		name    string
		err     error
		cancel  bool
		wantLog int
	}{
		{name: "server fault", err: errors.New("backend: " + marker), wantLog: 1},
		{
			name:    "client cancel",
			err:     fmt.Errorf("backend: %s: %w", marker, context.Canceled),
			cancel:  true,
			wantLog: 0,
		},
		{name: "reader fence", err: fmt.Errorf("backend: %s: %w", marker, db.ErrReaderStale), wantLog: 0},
		{
			name:    "snapshot budget timeout",
			err:     fmt.Errorf("read status snapshot: read active work summary %s: %w", marker, context.DeadlineExceeded),
			wantLog: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			*records = (*records)[:0]
			rec, _ := failure.Serve(failure.Suite{}, route, route.Handler(tc.err), tc.cancel)
			if got := countRecords(*records, slog.LevelError, hostedReadinessStatusReadFailedMessage); got != tc.wantLog {
				t.Errorf("ERROR %q records = %d, want %d; body = %s", hostedReadinessStatusReadFailedMessage, got, tc.wantLog, rec.Body.String())
			}
			if tc.wantLog == 0 {
				return
			}
			record := (*records)[len(*records)-1]
			if value, ok := recordAttr(record, "error"); !ok || !strings.Contains(value, marker) {
				t.Errorf("log error attr = %q, want it to carry %q", value, marker)
			}
			if value, ok := recordAttr(record, "request_id"); !ok || value == "" {
				t.Errorf("log request_id attr = %q, want non-empty", value)
			}
		})
	}
}

// TestHostedReadinessReadFailureLogCarriesRequestIDHeader pins that the
// #10658 log line honors a caller-supplied correlation id. Not parallel: it
// swaps the default slog logger.
func TestHostedReadinessReadFailureLogCarriesRequestIDHeader(t *testing.T) {
	logger, records := newRecordingLogger()
	previous := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(previous) })

	mux := http.NewServeMux()
	(&StatusHandler{StatusReader: fakeStatusReader{err: errors.New("backend down")}, LiveActivity: &fakeLiveActivityReader{}}).Mount(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/v0/status/hosted-readiness", nil)
	req.Header.Set("X-Request-ID", "req-10658-probe")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body = %s", rec.Code, rec.Body.String())
	}
	if got := countRecords(*records, slog.LevelError, hostedReadinessStatusReadFailedMessage); got != 1 {
		t.Fatalf("ERROR %q records = %d, want 1", hostedReadinessStatusReadFailedMessage, got)
	}
	record := (*records)[len(*records)-1]
	if value, ok := recordAttr(record, "request_id"); !ok || value != "req-10658-probe" {
		t.Errorf("log request_id attr = %q, want %q", value, "req-10658-probe")
	}
}

// TestHostedReadinessNilReaderAnswersUnavailable pins the unchanged
// configuration verdict: no status reader answers 503 (#10658 acceptance
// criterion).
func TestHostedReadinessNilReaderAnswersUnavailable(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	(&StatusHandler{}).Mount(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/v0/status/hosted-readiness", nil)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body = %s", rec.Code, rec.Body.String())
	}
	if got := failure.Message(rec.Body.Bytes()); got != "status reader not configured" {
		t.Errorf("body message = %q, want %q; body = %s", got, "status reader not configured", rec.Body.String())
	}
}
