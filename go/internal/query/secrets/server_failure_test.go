// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secrets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// secretsFailureCanary stands in for backend error text: a credential and a
// Cypher fragment that must never reach a response body (#7674).
const secretsFailureCanary = "password=hunter2 MATCH (n)"

// secretsFailureCase is one way a backend read fails: a server fault whose
// text carries the canary, a client cancel, or a stale PostgreSQL reader.
type secretsFailureCase struct {
	name   string
	err    error
	cancel bool
	stale  bool
}

func secretsFailureCases() []secretsFailureCase {
	return []secretsFailureCase{
		{name: "backend failure", err: errors.New("backend: " + secretsFailureCanary)},
		{name: "client cancel", err: fmt.Errorf("backend: %s: %w", secretsFailureCanary, context.Canceled), cancel: true},
		{name: "reader stale", err: fmt.Errorf("backend: %s: %w", secretsFailureCanary, db.ErrReaderStale), stale: true},
	}
}

// TestSecretsIAMReadFailuresAnswerFixedText drives every secrets/IAM read
// failure step through a backend fault, a client cancel, and a stale reader
// (#7674). It swaps the package-level secretsHandlerTracer, so it must not be
// parallel.
func TestSecretsIAMReadFailuresAnswerFixedText(t *testing.T) {
	routes := []struct {
		name    string
		path    string
		message string
		handler func(err error) *Handler
	}{
		{
			name:    "identity trust chains",
			path:    "/api/v0/secrets-iam/identity-trust-chains?scope_id=scope-a&limit=10",
			message: iamIdentityTrustChainsFailedMessage,
			handler: func(err error) *Handler { return &Handler{IdentityTrustChains: failingSecretsStores{err: err}} },
		},
		{
			name:    "privilege posture observations",
			path:    "/api/v0/secrets-iam/privilege-posture-observations?scope_id=scope-a&limit=10",
			message: iamPrivilegePostureObservationsFailedMessage,
			handler: func(err error) *Handler {
				return &Handler{PrivilegePostureObservations: failingSecretsStores{err: err}}
			},
		},
		{
			name:    "secret access paths",
			path:    "/api/v0/secrets-iam/secret-access-paths?scope_id=scope-a&limit=10",
			message: iamAccessPathsFailedMessage,
			handler: func(err error) *Handler { return &Handler{SecretAccessPaths: failingSecretsStores{err: err}} },
		},
		{
			name:    "posture gaps",
			path:    "/api/v0/secrets-iam/posture-gaps?scope_id=scope-a&limit=10",
			message: iamPostureGapsFailedMessage,
			handler: func(err error) *Handler { return &Handler{PostureGaps: failingSecretsStores{err: err}} },
		},
		{
			name:    "posture summary",
			path:    "/api/v0/secrets-iam/posture-summary?scope_id=scope-a",
			message: iamPostureSummaryFailedMessage,
			handler: func(err error) *Handler { return &Handler{Summary: failingSecretsStores{err: err}} },
		},
		{
			name:    "posture summary grant section",
			path:    "/api/v0/secrets-iam/posture-summary?scope_id=scope-a",
			message: iamGrantPostureFailedMessage,
			handler: func(err error) *Handler {
				return &Handler{Summary: failingSecretsStores{}, GrantPosture: failingSecretsStores{err: err}}
			},
		},
	}
	for _, route := range routes {
		for _, tc := range secretsFailureCases() {
			t.Run(route.name+"/"+tc.name, func(t *testing.T) {
				rec, ended := serveSecretsTraced(t, route.handler(tc.err), route.path, tc.cancel)
				assertSecretsFailure(t, route.message, tc, rec, ended)
			})
		}
	}
}

// serveSecretsTraced serves one GET under a recording tracer and returns the
// response and every ended span. cancel cancels the request context first.
func serveSecretsTraced(
	t *testing.T, handler *Handler, path string, cancel bool,
) (*httptest.ResponseRecorder, []sdktrace.ReadOnlySpan) {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	previous := secretsHandlerTracer
	secretsHandlerTracer = provider.Tracer("secrets-server-failure-test")
	t.Cleanup(func() { secretsHandlerTracer = previous })

	ctx, parent := provider.Tracer("secrets-server-failure-test").Start(context.Background(), "request")
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	if cancel {
		stop()
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	parent.End()
	return rec, recorder.Ended()
}

func assertSecretsFailure(
	t *testing.T, message string, tc secretsFailureCase, rec *httptest.ResponseRecorder, ended []sdktrace.ReadOnlySpan,
) {
	t.Helper()
	body := rec.Body.String()
	if strings.Contains(body, "hunter2") || strings.Contains(body, "MATCH (n)") {
		t.Fatalf("body leaked the backend error text: %s", body)
	}
	wantStatus := http.StatusInternalServerError
	switch {
	case tc.cancel:
		wantStatus = querycontract.StatusClientClosedRequest
	case tc.stale:
		wantStatus = http.StatusServiceUnavailable
	}
	if rec.Code != wantStatus {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, wantStatus, body)
	}
	if tc.stale {
		var envelope querycontract.ResponseEnvelope
		if rec.Header().Get("Retry-After") == "" || json.Unmarshal(rec.Body.Bytes(), &envelope) != nil ||
			envelope.Error == nil || envelope.Error.Code != querycontract.ErrorCodeBackendUnavailable {
			t.Fatalf("reader fence answer = %s (Retry-After %q), want a backend_unavailable envelope with Retry-After",
				body, rec.Header().Get("Retry-After"))
		}
		return
	}
	var plain struct {
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &plain); err != nil || plain.Detail != message {
		t.Fatalf("body detail = %q (decode err %v), want the fixed %q", plain.Detail, err, message)
	}
	assertSecretsFailureSpan(t, message, tc.cancel, ended)
}

// assertSecretsFailureSpan checks the handler span: a client cancel leaves
// the status Unset with only the cancel event; a server fault sets Error with
// message as its description and records exactly one exception event.
func assertSecretsFailureSpan(t *testing.T, message string, cancel bool, ended []sdktrace.ReadOnlySpan) {
	t.Helper()
	var span sdktrace.ReadOnlySpan
	for _, candidate := range ended {
		if candidate.Name() != "request" {
			span = candidate
			break
		}
	}
	if span == nil {
		t.Fatal("no handler span recorded")
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

// failingSecretsStores implements every secrets/IAM store port and fails each
// read with err; a nil err answers an empty result.
type failingSecretsStores struct{ err error }

func (s failingSecretsStores) ListSecretsIAMIdentityTrustChains(
	context.Context, IAMIdentityTrustChainFilter,
) ([]IAMIdentityTrustChainRow, error) {
	return nil, s.err
}

func (s failingSecretsStores) ListSecretsIAMPrivilegePostureObservations(
	context.Context, IAMPrivilegePostureObservationFilter,
) ([]IAMPrivilegePostureObservationRow, error) {
	return nil, s.err
}

func (s failingSecretsStores) ListSecretsIAMSecretAccessPaths(
	context.Context, IAMSecretAccessPathFilter,
) ([]IAMSecretAccessPathRow, error) {
	return nil, s.err
}

func (s failingSecretsStores) ListSecretsIAMPostureGaps(context.Context, IAMPostureGapFilter) ([]IAMPostureGapRow, error) {
	return nil, s.err
}

func (s failingSecretsStores) SummarizeSecretsIAMPosture(context.Context, string) (IAMPostureSummary, error) {
	return IAMPostureSummary{}, s.err
}

func (s failingSecretsStores) SummarizeS3ExternalPrincipalGrantPosture(context.Context, string) (IAMGrantPosture, error) {
	return IAMGrantPosture{}, s.err
}
