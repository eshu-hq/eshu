// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
	"github.com/eshu-hq/eshu/go/internal/status"
)

// TestStatusReadFailureMatchesQueryServerFailure pins the admin-route
// WriteReadFailure (status package, which cannot import the query packages
// without pulling in their dependency closure) to the query routes' contract:
// the same 499 constant, the same client-cancel span event, and the same
// 499/500 split and span signal for every cancel and fault shape.
func TestStatusReadFailureMatchesQueryServerFailure(t *testing.T) {
	if status.StatusClientClosedRequest != querycontract.StatusClientClosedRequest {
		t.Fatalf("status 499 constant = %d, query = %d",
			status.StatusClientClosedRequest, querycontract.StatusClientClosedRequest)
	}
	if status.ClientCanceledEvent != tracing.ClientCanceledEvent {
		t.Fatalf("status cancel event = %q, query = %q",
			status.ClientCanceledEvent, tracing.ClientCanceledEvent)
	}

	cases := []struct {
		name     string
		err      error
		canceled bool
		want     int
	}{
		{"client cancel", fmt.Errorf("read: %w", context.Canceled), true, status.StatusClientClosedRequest},
		{"inner cancel on live request", fmt.Errorf("read: %w", context.Canceled), false, http.StatusInternalServerError},
		{"canceled request with unrelated fault", errors.New("boom"), true, http.StatusInternalServerError},
		{"deadline exceeded", fmt.Errorf("read: %w", context.DeadlineExceeded), false, http.StatusInternalServerError},
		{"plain fault", errors.New("boom"), false, http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			statusCode, statusEvents := writeStatusFailure(t, tc.err, tc.canceled)
			queryCode, queryEvents := writeQueryFailure(t, tc.err, tc.canceled)
			if statusCode != tc.want || queryCode != tc.want {
				t.Fatalf("status = %d, query = %d, want %d", statusCode, queryCode, tc.want)
			}
			if fmt.Sprint(statusEvents) != fmt.Sprint(queryEvents) {
				t.Fatalf("span events differ: status %v, query %v", statusEvents, queryEvents)
			}
		})
	}
}

func parityRequest(canceled bool) (*http.Request, func() []string) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	ctx, span := provider.Tracer("parity").Start(context.Background(), "request")
	ctx, cancel := context.WithCancel(ctx)
	if canceled {
		cancel()
	}
	req := httptest.NewRequest(http.MethodGet, "/x", nil).WithContext(ctx)
	return req, func() []string {
		defer cancel()
		span.End()
		var names []string
		for _, ev := range recorder.Ended()[0].Events() {
			names = append(names, ev.Name)
		}
		return names
	}
}

func writeStatusFailure(t *testing.T, err error, canceled bool) (int, []string) {
	t.Helper()
	req, events := parityRequest(canceled)
	rec := httptest.NewRecorder()
	status.WriteReadFailure(rec, req, err, "failed")
	return rec.Code, events()
}

func writeQueryFailure(t *testing.T, err error, canceled bool) (int, []string) {
	t.Helper()
	req, events := parityRequest(canceled)
	rec := httptest.NewRecorder()
	tracing.WriteServerFailure(rec, req, err, http.StatusInternalServerError, "failed")
	return rec.Code, events()
}
