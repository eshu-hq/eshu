// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// driverReadTimeout mirrors what the Neo4j Go driver returned in #7353 when
// the handler's bounded context expired mid-read: a ConnectivityError that
// wraps the driver's own read-timeout text, not ctx.Err(), so it does not
// match context.DeadlineExceeded under errors.Is.
func driverReadTimeout() error {
	return &neo4jdriver.ConnectivityError{
		Inner: errors.New("Timeout while reading from connection [server-side timeout hint: 2m0s, user-provided context deadline: 5ms]"),
	}
}

// entityContextEnvelopeRequest builds a GetEntityContext request that asks for
// the stable error envelope, bounded by timeout when timeout > 0.
func entityContextEnvelopeRequest(t *testing.T, timeout time.Duration) (*http.Request, context.CancelFunc) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v0/entities/entity-a/context", nil)
	req.SetPathValue("entity_id", "entity-a")
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	cancel := context.CancelFunc(func() {})
	if timeout > 0 {
		var ctx context.Context
		ctx, cancel = context.WithTimeout(req.Context(), timeout)
		req = req.WithContext(ctx)
	}
	return req, cancel
}

// decodeErrorCode returns the envelope's error.code, failing the test when the
// body is not an error envelope.
func decodeErrorCode(t *testing.T, rec *httptest.ResponseRecorder) querycontract.ErrorCode {
	t.Helper()
	var env querycontract.ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v; body=%s", err, rec.Body.String())
	}
	if env.Error == nil {
		t.Fatalf("envelope has no error; body=%s", rec.Body.String())
	}
	return env.Error.Code
}

// TestGetEntityContextExpiredBudgetWithDriverErrorAnswers504 pins the #7353
// hardening for readers that bypass Neo4jReader (test readers, fakes, any
// future GraphQuery). The production Neo4jReader already checks the context
// first and returns a deadline error. A reader that returns the raw driver
// error instead -- a ConnectivityError ("Timeout while reading from
// connection") that does not wrap context.DeadlineExceeded, as the entity
// live-test reader did on a cold Neo4j -- used to fall through the handler's
// errors.Is check to a generic 500. Once the bounded context has expired the
// handler must answer the graph-read deadline contract (504, backend_timeout)
// whatever error the reader returned.
func TestGetEntityContextExpiredBudgetWithDriverErrorAnswers504(t *testing.T) {
	t.Parallel()

	reader := graph.FakeGraphReader{
		RunSingleFn: func(ctx context.Context, _ string, _ map[string]any) (map[string]any, error) {
			<-ctx.Done() // the read outlives the shared budget
			return nil, driverReadTimeout()
		},
	}
	var logs bytes.Buffer
	handler := &Handler{
		Neo4j:   reader,
		Profile: querycontract.ProfileLocalAuthoritative,
		Logger:  slog.New(slog.NewJSONHandler(&logs, nil)),
	}
	req, cancel := entityContextEnvelopeRequest(t, 5*time.Millisecond)
	defer cancel()
	rec := httptest.NewRecorder()

	handler.GetEntityContext(rec, req)

	if got, want := rec.Code, http.StatusGatewayTimeout; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, rec.Body.String())
	}
	if got, want := decodeErrorCode(t, rec), querycontract.ErrorCodeBackendTimeout; got != want {
		t.Fatalf("error.code = %q, want %q", got, want)
	}
	// The operator signal must name the deadline, not a generic read error.
	if !bytes.Contains(logs.Bytes(), []byte(`"failure_class":"deadline"`)) {
		t.Fatalf("anchor-loop warning log does not carry failure_class=deadline; logs=%s", logs.String())
	}
}

// TestGetEntityContextLiveContextConnectivityErrorIsNotADeadline keeps the
// other side of the #7353 contract: a connectivity failure that arrives while
// the bounded context is still live is not a deadline, so it must not be
// reported as 504. The raw driver error keeps the existing generic 500 path
// (the production Neo4jReader maps it to 503 backend_unavailable first).
func TestGetEntityContextLiveContextConnectivityErrorIsNotADeadline(t *testing.T) {
	t.Parallel()

	reader := graph.FakeGraphReader{
		RunSingleFn: func(context.Context, string, map[string]any) (map[string]any, error) {
			return nil, driverReadTimeout()
		},
	}
	handler := &Handler{Neo4j: reader, Profile: querycontract.ProfileLocalAuthoritative}
	req, cancel := entityContextEnvelopeRequest(t, 0)
	defer cancel()
	rec := httptest.NewRecorder()

	handler.GetEntityContext(rec, req)

	if got, want := rec.Code, http.StatusInternalServerError; got != want {
		t.Fatalf("status = %d, want %d (a live-context connectivity error is not a deadline); body=%s", got, want, rec.Body.String())
	}
}

// TestGetEntityContextCanceledRequestIsNotADeadline pins that a caller
// cancellation (client disconnect) is not reclassified as a graph-read
// deadline: only an expired deadline earns the 504 backend_timeout contract.
func TestGetEntityContextCanceledRequestIsNotADeadline(t *testing.T) {
	t.Parallel()

	reader := graph.FakeGraphReader{
		RunSingleFn: func(ctx context.Context, _ string, _ map[string]any) (map[string]any, error) {
			<-ctx.Done()
			return nil, driverReadTimeout()
		},
	}
	handler := &Handler{Neo4j: reader, Profile: querycontract.ProfileLocalAuthoritative}
	req, done := entityContextEnvelopeRequest(t, 0)
	defer done()
	ctx, cancel := context.WithCancel(req.Context())
	cancel()
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.GetEntityContext(rec, req)

	if rec.Code == http.StatusGatewayTimeout {
		t.Fatalf("status = 504 for a canceled request; cancellation is not a graph-read deadline; body=%s", rec.Body.String())
	}
}

// TestGetEntityContextExpiredBudgetKeepsReaderUnavailableVerdict pins that
// the response and the operator log agree when a reader has already reported
// an outage (ErrGraphUnavailable) and the bounded ctx expires before the
// handler classifies it: 503 backend_unavailable, and no deadline
// failure_class in the log.
func TestGetEntityContextExpiredBudgetKeepsReaderUnavailableVerdict(t *testing.T) {
	t.Parallel()

	reader := graph.FakeGraphReader{
		RunSingleFn: func(ctx context.Context, _ string, _ map[string]any) (map[string]any, error) {
			<-ctx.Done()
			return nil, fmt.Errorf("private address: %w", querycontract.ErrGraphUnavailable)
		},
	}
	var logs bytes.Buffer
	handler := &Handler{
		Neo4j:   reader,
		Profile: querycontract.ProfileLocalAuthoritative,
		Logger:  slog.New(slog.NewJSONHandler(&logs, nil)),
	}
	req, cancel := entityContextEnvelopeRequest(t, 5*time.Millisecond)
	defer cancel()
	rec := httptest.NewRecorder()

	handler.GetEntityContext(rec, req)

	if got, want := rec.Code, http.StatusServiceUnavailable; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, rec.Body.String())
	}
	if got, want := decodeErrorCode(t, rec), querycontract.ErrorCodeBackendUnavailable; got != want {
		t.Fatalf("error.code = %q, want %q", got, want)
	}
	if bytes.Contains(logs.Bytes(), []byte(`"failure_class":"deadline"`)) {
		t.Fatalf("log says failure_class=deadline while the response is 503; logs=%s", logs.String())
	}
}
