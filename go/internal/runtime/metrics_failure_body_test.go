// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

const metricsFailureCanary = "pq: password authentication failed for user eshu host=db.internal.example"

// TestStatusMetricsHandlerReadFailureHidesBackendText proves a failing status
// reader answers the fixed metrics message, never the backend text.
func TestStatusMetricsHandlerReadFailureHidesBackendText(t *testing.T) {
	handler, err := NewStatusMetricsHandler("api", &fakeStatusReader{err: errors.New(metricsFailureCanary)})
	if err != nil {
		t.Fatalf("NewStatusMetricsHandler() error = %v", err)
	}
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "password") || strings.Contains(rec.Body.String(), "db.internal") {
		t.Fatalf("body leaks backend text: %q", rec.Body.String())
	}
	if got := strings.TrimSpace(rec.Body.String()); got != metricsLoadFailureMessage {
		t.Fatalf("body = %q, want %q", got, metricsLoadFailureMessage)
	}
}

// TestStatusMetricsHandlerClientCancelIsNotAServerFault proves a canceled
// scrape answers 499, not a 500 server fault.
func TestStatusMetricsHandlerClientCancelIsNotAServerFault(t *testing.T) {
	err := fmt.Errorf("%s: %w", metricsFailureCanary, context.Canceled)
	handler, herr := NewStatusMetricsHandler("api", &fakeStatusReader{err: err})
	if herr != nil {
		t.Fatalf("NewStatusMetricsHandler() error = %v", herr)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil).WithContext(ctx))

	if rec.Code != statuspkg.StatusClientClosedRequest {
		t.Fatalf("status = %d, want %d", rec.Code, statuspkg.StatusClientClosedRequest)
	}
	if strings.Contains(rec.Body.String(), "password") {
		t.Fatalf("body leaks backend text: %q", rec.Body.String())
	}
}
