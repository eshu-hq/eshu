// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	entitycontract "github.com/eshu-hq/eshu/go/internal/query/querycontract/entity"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// routeFailureCanary stands in for backend error text: a credential and a
// Cypher fragment that must never reach a response body (#7674).
const routeFailureCanary = "password=hunter2 MATCH (n)"

// routeFailureCase is one failure an entity route step must classify.
type routeFailureCase struct {
	name   string
	err    error
	cancel bool
	// wantStatus is the answered status. wantBody is the exact detail text,
	// or "" for a fence 503 whose body comes from WriteGraphReadError.
	wantStatus    int
	wantBody      string
	wantSpanError bool
}

// routeFailureCases returns the canary 500, client-cancel 499, and
// reader-fence 503 cases for a route step whose fixed message is message.
func routeFailureCases(message string) []routeFailureCase {
	return []routeFailureCase{
		{
			name:          "backend failure",
			err:           errors.New("private backend: " + routeFailureCanary),
			wantStatus:    http.StatusInternalServerError,
			wantBody:      message,
			wantSpanError: true,
		},
		{
			name:       "client cancel",
			err:        fmt.Errorf("private backend %s: %w", routeFailureCanary, context.Canceled),
			cancel:     true,
			wantStatus: querycontract.StatusClientClosedRequest,
			wantBody:   message,
		},
		{
			name:       "reader fence",
			err:        fmt.Errorf("private read store %s: %w", routeFailureCanary, db.ErrReaderStale),
			wantStatus: http.StatusServiceUnavailable,
		},
	}
}

// sentinelFailureCase is a configuration-state 503: err wraps sentinel under
// wrapper text that carries the canary, and the body must be only the
// sentinel's own text.
func sentinelFailureCase(sentinel error) routeFailureCase {
	return routeFailureCase{
		name:          "sentinel " + sentinel.Error(),
		err:           fmt.Errorf("private wrapper %s: %w", routeFailureCanary, sentinel),
		wantStatus:    http.StatusServiceUnavailable,
		wantBody:      sentinel.Error(),
		wantSpanError: true,
	}
}

// serveRouteFailure serves one request through handler's mux on a recorded
// span, canceling the request context first when tc.cancel is set, and
// asserts the classified answer.
func serveRouteFailure(t *testing.T, handler *Handler, method, path string, body string, tc routeFailureCase) {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	ctx, span := provider.Tracer("entity-route-failure-test").Start(context.Background(), "request")
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if tc.cancel {
		cancel()
	}

	var reqBody io.Reader
	if body != "" {
		reqBody = strings.NewReader(body)
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(method, path, reqBody).WithContext(ctx))
	span.End()

	assertRouteFailure(t, rec, recorder.Ended(), tc)
}

// assertRouteFailure checks the status, the exact fixed body, the absence of
// the canary, Retry-After on a fence 503, and the span signal: Error with the
// fixed message and exactly one exception event for a server fault, or Unset
// with only the client-cancel event for a client cancel.
func assertRouteFailure(t *testing.T, rec *httptest.ResponseRecorder, ended []sdktrace.ReadOnlySpan, tc routeFailureCase) {
	t.Helper()
	raw := rec.Body.String()
	if rec.Code != tc.wantStatus {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, tc.wantStatus, raw)
	}
	if strings.Contains(raw, "hunter2") || strings.Contains(raw, "MATCH (n)") || strings.Contains(raw, "private") {
		t.Fatalf("body leaked backend error text: %s", raw)
	}
	if tc.wantBody == "" {
		if rec.Header().Get("Retry-After") == "" {
			t.Fatalf("reader fence 503 is missing Retry-After; body = %s", raw)
		}
	} else if got := routeFailureDetail(t, raw); got != tc.wantBody {
		t.Fatalf("body detail = %q, want the fixed %q", got, tc.wantBody)
	}
	if len(ended) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(ended))
	}
	exceptions, canceled := 0, false
	for _, event := range ended[0].Events() {
		if event.Name == "exception" {
			exceptions++
		}
		canceled = canceled || event.Name == tracing.ClientCanceledEvent
	}
	status := ended[0].Status()
	if tc.wantSpanError {
		if status.Code != codes.Error || status.Description != tc.wantBody || exceptions != 1 {
			t.Fatalf("span status = %v (%q), exceptions = %d; want Error (%q) with one exception",
				status.Code, status.Description, exceptions, tc.wantBody)
		}
	} else if status.Code == codes.Error || exceptions != 0 {
		t.Fatalf("span status = %v (%q), exceptions = %d; want no error recorded", status.Code, status.Description, exceptions)
	}
	if canceled != tc.cancel {
		t.Fatalf("%s event recorded = %v, want %v", tracing.ClientCanceledEvent, canceled, tc.cancel)
	}
}

// routeFailureDetail returns the message of a WriteError body ("detail") or
// an envelope body ("error.message").
func routeFailureDetail(t *testing.T, raw string) string {
	t.Helper()
	var body struct {
		Detail string `json:"detail"`
		Error  any    `json:"error"`
	}
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatalf("decode body %q: %v", raw, err)
	}
	if body.Detail != "" {
		return body.Detail
	}
	if envelope, ok := body.Error.(map[string]any); ok {
		message, _ := envelope["message"].(string)
		return message
	}
	return ""
}

// routeFailureContent is a content store whose reads fail with the
// configured errors; a nil error keeps the embedded fake's empty answer.
type routeFailureContent struct {
	content.FakePortContentStore
	entity        *querycontract.EntityContent
	getEntityErr  error
	byNameErr     error
	byIDsErr      error
	nameSearchErr error
}

func (f routeFailureContent) GetEntityContent(context.Context, string) (*querycontract.EntityContent, error) {
	return f.entity, f.getEntityErr
}

func (f routeFailureContent) SearchEntitiesByName(context.Context, string, string, string, int) ([]querycontract.EntityContent, error) {
	return nil, f.byNameErr
}

func (f routeFailureContent) ListRepoEntitiesByIDs(ctx context.Context, repoID string, ids []string, limit int) ([]querycontract.EntityContent, error) {
	if f.byIDsErr != nil {
		return nil, f.byIDsErr
	}
	return f.FakePortContentStore.ListRepoEntitiesByIDs(ctx, repoID, ids, limit)
}

// ListRepoEntitiesByKeys finds no key match and, unlike the embedded fake,
// ignores a canceled context, so a client-cancel case reaches the step under
// test instead of failing in metadata enrichment first.
func (f routeFailureContent) ListRepoEntitiesByKeys(context.Context, string, []querycontract.EntityContentKey) ([]querycontract.EntityContent, error) {
	return nil, nil
}

func (f routeFailureContent) SearchEntityNames(context.Context, entitycontract.EntityNameSearch) ([]querycontract.EntityContent, error) {
	return nil, f.nameSearchErr
}
