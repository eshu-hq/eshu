// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// driverTimeoutAfterDeadline returns the error the Neo4j Go driver surfaced in
// #7353 once a read outlived its context: a ConnectivityError, not ctx.Err(),
// so it does not match context.DeadlineExceeded under errors.Is.
func driverTimeoutAfterDeadline(ctx context.Context) error {
	<-ctx.Done()
	return &neo4jdriver.ConnectivityError{
		Inner: errors.New("Timeout while reading from connection [server-side timeout hint: 2m0s]"),
	}
}

// driverTimeoutGraph answers every read with driverTimeoutAfterDeadline.
type driverTimeoutGraph struct{}

func (driverTimeoutGraph) Run(ctx context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
	return nil, driverTimeoutAfterDeadline(ctx)
}

func (driverTimeoutGraph) RunSingle(ctx context.Context, _ string, _ map[string]any) (map[string]any, error) {
	return nil, driverTimeoutAfterDeadline(ctx)
}

// assertBackendTimeout fails unless rec carries the 504 backend_timeout
// graph-read deadline contract.
func assertBackendTimeout(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if got, want := rec.Code, http.StatusGatewayTimeout; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), string(ErrorCodeBackendTimeout)) {
		t.Fatalf("body does not carry %q; body=%s", ErrorCodeBackendTimeout, rec.Body.String())
	}
}

// TestGetRelationshipsExpiredBudgetWithDriverErrorAnswers504 is the #7353
// sibling of the entity-context regression: getRelationships runs the same
// shared-deadline per-label anchor loop, and a driver ConnectivityError that
// arrives after that deadline expired must answer 504, not 500.
func TestGetRelationshipsExpiredBudgetWithDriverErrorAnswers504(t *testing.T) {
	t.Parallel()

	handler := &InfraHandler{Neo4j: driverTimeoutGraph{}, Profile: ProfileLocalAuthoritative}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest(http.MethodPost, "/api/v0/infra/relationships", strings.NewReader(`{"entity_id":"e1"}`)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.getRelationships(rec, req)

	assertBackendTimeout(t, rec)
}

// TestTagHistoryExpiredBudgetWithDriverErrorAnswers504 covers both tag-history
// read paths: the scoped refill loop (its own shared bounded deadline) and the
// unscoped single read. A driver ConnectivityError after the deadline expired
// must answer 504, not 500.
func TestTagHistoryExpiredBudgetWithDriverErrorAnswers504(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		auth *AuthContext // nil: no auth context, the unscoped path
	}{
		{name: "scoped refill", auth: scopedTagHistoryAuth("repo-granted")},
		{name: "unscoped read", auth: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
			defer cancel()
			handler := &TagHistoryHandler{
				Neo4j:   driverTimeoutGraph{},
				Profile: ProfileLocalAuthoritative,
				Cursors: tagHistoryTestCursorKeyring,
			}
			mux := http.NewServeMux()
			handler.Mount(mux)
			req := newTagHistoryRequest(tagHistoryGrantTarget)
			req.Header.Set("Accept", EnvelopeMIMEType)
			if tc.auth != nil {
				ctx = ContextWithAuthContext(ctx, *tc.auth)
			}
			req = req.WithContext(ctx)
			rec := httptest.NewRecorder()

			mux.ServeHTTP(rec, req)

			assertBackendTimeout(t, rec)
		})
	}
}
