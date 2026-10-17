// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// serveDeltaUnionTraced serves one GET of the completeness route through the
// real mux under a recording request span, canceling the request context
// first when cancel is set.
func serveDeltaUnionTraced(
	t *testing.T, handler *Handler, query string, cancel bool,
) (*httptest.ResponseRecorder, []sdktrace.ReadOnlySpan) {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	ctx, parent := provider.Tracer("delta-union-failure-test").Start(context.Background(), "request")
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	if cancel {
		stop()
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/v0/admin/delta-union/completeness"+query, nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	parent.End()
	return rec, recorder.Ended()
}

// TestDeltaUnionCompletenessFailuresAnswerFixedText is the #7674 regression
// for GET /api/v0/admin/delta-union/completeness. A failed ACTIVE or graph
// read answered 500 with err.Error() in "detail", so the body quoted the
// backend error, and a client cancel answered that 500 as a server fault.
// Both reads now answer the fixed detail through the server-failure helper:
// 500 with the error on the request span, or 499 for a client cancel. The
// {"status","detail"} body shape is unchanged.
func TestDeltaUnionCompletenessFailuresAnswerFixedText(t *testing.T) {
	t.Parallel()

	failActive := func(err error) *Handler {
		checker, active := deltaUnionChecker()
		active.err = err
		return &Handler{Completeness: checker}
	}
	failGraph := func(err error) *Handler {
		checker, _ := deltaUnionChecker()
		checker.Graph = &stubDeltaUnionGraph{err: err}
		return &Handler{Completeness: checker}
	}
	for _, tc := range []struct {
		name    string
		handler func(error) *Handler
	}{
		{"active read", failActive},
		{"graph read", failGraph},
	} {
		for _, kind := range []adminFailureKind{adminFailureBackend, adminFailureCancel} {
			t.Run(tc.name+"/"+kind.String(), func(t *testing.T) {
				t.Parallel()
				rec, ended := serveDeltaUnionTraced(t, tc.handler(adminFailureError(kind)), "?scope=scope-1", kind == adminFailureCancel)
				step := adminFailureStep{name: tc.name, message: deltaUnionCompletenessFailedMessage}
				assertAdminFailure(t, step, kind, rec, ended)
				if got := adminFailureBodyStatus(t, rec); got != "error" {
					t.Fatalf("body status = %q, want the unchanged \"error\"; body = %s", got, rec.Body.String())
				}
			})
		}
	}
}

// TestDeltaUnionCompletenessCapAnswersItsOwnFixedText pins that a filter past
// the repository cap keeps its 500 and its narrow-the-filter text, which the
// route builds from the counts alone, not from an error's text.
func TestDeltaUnionCompletenessCapAnswersItsOwnFixedText(t *testing.T) {
	t.Parallel()

	checker, _ := deltaUnionChecker()
	checker.MaxRepos = 1
	rec, _ := serveDeltaUnionTraced(t, &Handler{Completeness: checker}, "?scope=scope-1&scope=scope-2", false)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body = %s", rec.Code, rec.Body.String())
	}
	want := "delta union completeness covers 2 scopes, past the 1 cap: narrow the scope filter"
	if got := adminFailureBodyMessage(t, rec); got != want {
		t.Fatalf("detail = %q, want %q", got, want)
	}
}

// TestDeltaUnionCompletenessKeepsClientVerdicts pins the route's existing
// non-fault answers: a missing scope filter stays 400 with its sentinel text,
// and an unconfigured checker stays the plain 503.
func TestDeltaUnionCompletenessKeepsClientVerdicts(t *testing.T) {
	t.Parallel()

	checker, _ := deltaUnionChecker()
	rec, _ := serveDeltaUnionTraced(t, &Handler{Completeness: checker}, "", false)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("no-scope status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
	if got, want := adminFailureBodyMessage(t, rec), ErrDeltaUnionScopeRequired.Error()+": pass at least one ?scope= query value"; got != want {
		t.Fatalf("no-scope detail = %q, want %q", got, want)
	}
	rec, _ = serveDeltaUnionTraced(t, &Handler{}, "?scope=scope-1", false)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured status = %d, want 503; body = %s", rec.Code, rec.Body.String())
	}
}

// adminFailureBodyStatus reads the "status" field of a {"status","detail"}
// body.
func adminFailureBodyStatus(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v; body = %s", err, rec.Body.String())
	}
	return body.Status
}
