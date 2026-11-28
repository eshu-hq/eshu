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
)

// serveGraphVerdict runs the exported handler as the non-nil hook through Serve.
func serveGraphVerdict(t *testing.T, err error, accept string) *httptest.ResponseRecorder {
	t.Helper()
	store := &fakeDeltaUnionStore{err: err}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/admin/delta-union/completeness?scope=scope-1", nil)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	rec := httptest.NewRecorder()
	ServeDeltaUnionCompleteness(rec, req, store, DeltaUnionGraphErrorHandler)
	return rec
}

// TestDeltaUnionGraphErrorHandlerVerdicts pins verdict parity: 503+Retry-After
// (retryable only), 504, permanent 503 without Retry-After, {status,detail}
// body, and no backend text leakage.
func TestDeltaUnionGraphErrorHandlerVerdicts(t *testing.T) {
	canary := "hunter2 db.internal.example"
	for _, tc := range []struct {
		name       string
		err        error
		wantStatus int
		wantDetail string
		retryAfter bool
	}{
		{name: "graph unavailable", err: fmt.Errorf("read delta union graph for repo repo-1: %w: %s", ErrGraphUnavailable, canary), wantStatus: http.StatusServiceUnavailable, wantDetail: ErrGraphUnavailable.Error(), retryAfter: true},
		{name: "reader retryable", err: fmt.Errorf("read delta union active: %w: %s", ErrReaderRetryable, canary), wantStatus: http.StatusServiceUnavailable, wantDetail: ErrReaderRetryable.Error(), retryAfter: true},
		{name: "graph deadline", err: fmt.Errorf("read delta union graph for repo repo-1: %w: %s", ErrGraphReadDeadline, canary), wantStatus: http.StatusGatewayTimeout, wantDetail: ErrGraphReadDeadline.Error()},
		{name: "snapshot refused permanent", err: fmt.Errorf("read: %w: %s", ErrSnapshotExportRefused, canary), wantStatus: http.StatusServiceUnavailable, wantDetail: ErrSnapshotExportRefused.Error()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := serveGraphVerdict(t, tc.err, "")
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), "hunter2") || strings.Contains(rec.Body.String(), "db.internal") {
				t.Fatalf("body leaked backend text: %s", rec.Body.String())
			}
			retryAfter := rec.Header().Get("Retry-After")
			if tc.retryAfter && retryAfter != fmt.Sprint(BackendUnavailableRetryAfterSeconds) {
				t.Fatalf("Retry-After = %q, want %d", retryAfter, BackendUnavailableRetryAfterSeconds)
			}
			if !tc.retryAfter && retryAfter != "" {
				t.Fatalf("Retry-After = %q, want none", retryAfter)
			}
			var decoded map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
				t.Fatalf("body not JSON: %v", err)
			}
			if len(decoded) != 2 || decoded["status"] != "error" || decoded["detail"] != tc.wantDetail {
				t.Fatalf("body = %v, want exactly {status:error detail:%q}", decoded, tc.wantDetail)
			}
		})
	}
}

// TestDeltaUnionGraphErrorHandlerEnvelopeNamesCapability pins the negotiated
// envelope carrying operator.delta_union_completeness.check.
func TestDeltaUnionGraphErrorHandlerEnvelopeNamesCapability(t *testing.T) {
	rec := serveGraphVerdict(t, fmt.Errorf("graph: %w", ErrGraphUnavailable), EnvelopeMIMEType)
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("status=%d Retry-After=%q body=%s", rec.Code, rec.Header().Get("Retry-After"), rec.Body.String())
	}
	var envelope struct {
		Error *struct {
			Code       string         `json:"code"`
			Message    string         `json:"message"`
			Capability string         `json:"capability"`
			Details    map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("envelope decode: %v body=%s", err, rec.Body.String())
	}
	if envelope.Error == nil || envelope.Error.Capability != DeltaUnionCompletenessCapability || envelope.Error.Code != "backend_unavailable" || envelope.Error.Message != ErrGraphUnavailable.Error() {
		t.Fatalf("error = %+v, want backend_unavailable under %q", envelope.Error, DeltaUnionCompletenessCapability)
	}
	if DeltaUnionCompletenessCapability != "operator.delta_union_completeness.check" {
		t.Fatalf("capability = %q", DeltaUnionCompletenessCapability)
	}
	// Deadline envelope is backend_timeout with no Retry-After.
	rec = serveGraphVerdict(t, ErrGraphReadDeadline, EnvelopeMIMEType)
	if rec.Code != http.StatusGatewayTimeout || rec.Header().Get("Retry-After") != "" {
		t.Fatalf("deadline status=%d Retry-After=%q", rec.Code, rec.Header().Get("Retry-After"))
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil || envelope.Error == nil || envelope.Error.Code != "backend_timeout" {
		t.Fatalf("deadline envelope = %s err=%v", rec.Body.String(), err)
	}
	// Snapshot refusal details carry the closed reason.
	rec = serveGraphVerdict(t, ErrSnapshotExportRefused, EnvelopeMIMEType)
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil || envelope.Error == nil || envelope.Error.Details["reason"] != SnapshotExportUnqualifiedReason {
		t.Fatalf("snapshot envelope = %s err=%v", rec.Body.String(), err)
	}
}

// TestDeltaUnionGraphErrorHandlerFallthrough pins false for unclaimed errors so
// Serve falls through to its fixed 500 without leaking backend text.
func TestDeltaUnionGraphErrorHandlerFallthrough(t *testing.T) {
	rec := serveGraphVerdict(t, errors.New("graph: connection refused password=hunter2"), "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 fallthrough", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "hunter2") {
		t.Fatalf("body leaked: %s", rec.Body.String())
	}
	// Direct false return leaves the recorder untouched (no status written).
	direct := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	if DeltaUnionGraphErrorHandler(direct, req, errors.New("plain failure")) {
		t.Fatal("handler claimed a plain error, want false")
	}
	if DeltaUnionGraphErrorHandler(direct, req, nil) {
		t.Fatal("handler claimed nil, want false")
	}
}

// TestClassifyDeltaUnionGraphReadError pins deadline classification without
// reclassifying reader-retryable or already-classified verdicts.
func TestClassifyDeltaUnionGraphReadError(t *testing.T) {
	if got := ClassifyDeltaUnionGraphReadError(context.Background(), nil); got != nil {
		t.Fatalf("nil = %v, want nil", got)
	}
	plain := errors.New("connectivity timeout")
	if got := ClassifyDeltaUnionGraphReadError(context.Background(), fmt.Errorf("run: %w", context.DeadlineExceeded)); !errors.Is(got, ErrGraphReadDeadline) {
		t.Fatalf("deadline classify = %v, want ErrGraphReadDeadline", got)
	}
	expired, cancel := context.WithCancel(context.Background())
	_ = expired
	cancel()
	// Canceled (not deadline) context leaves err unchanged.
	if got := ClassifyDeltaUnionGraphReadError(expired, plain); got != plain {
		t.Fatalf("canceled classify = %v, want unchanged", got)
	}
	readerErr := fmt.Errorf("reader: %w: %w", ErrReaderRetryable, context.DeadlineExceeded)
	if got := ClassifyDeltaUnionGraphReadError(context.Background(), readerErr); errors.Is(got, ErrGraphReadDeadline) {
		t.Fatalf("reader classify = %v, must not become deadline", got)
	}
}
