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

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// driverReadTimeoutSession is the production-reader session double for #7353:
// the result cursor blocks until the read context is done and then fails the
// way the Neo4j Go driver did live -- a ConnectivityError whose text mentions
// the deadline but which does not wrap context.DeadlineExceeded.
func driverReadTimeoutSession(context.Context, neo4jdriver.SessionConfig) neo4jReadSession {
	return &fakeNeo4jReadSession{result: &fakeNeo4jReadResult{collect: func(ctx context.Context) ([]*neo4jdriver.Record, error) {
		<-ctx.Done()
		return nil, &neo4jdriver.ConnectivityError{
			Inner: errors.New("Timeout while reading from connection [server-side timeout hint: 2m0s]: context deadline exceeded"),
		}
	}}}
}

// TestNeo4jReaderDriverTimeoutUnderExpiredContextIsADeadline answers #7353's
// open question for the production read path: when a read outlives its
// context and the driver returns a ConnectivityError rather than ctx.Err(),
// Neo4jReader still hands the handler a deadline error. graphReadResult checks
// the parent context before the driver error, so the driver's error type never
// decides the outcome.
func TestNeo4jReaderDriverTimeoutUnderExpiredContextIsADeadline(t *testing.T) {
	t.Parallel()

	t.Run("policy budget", func(t *testing.T) {
		t.Parallel()
		reader := newPolicyTestNeo4jReader(driverReadTimeoutSession)
		ctx, cancel := querycontract.WithBoundedGraphReadDeadlineFor(context.Background(), 20*time.Millisecond)
		defer cancel()

		_, err := reader.RunSingle(ctx, "MATCH (n) RETURN n", nil)

		if !errors.Is(err, ErrGraphReadDeadline) {
			t.Fatalf("err = %v, want ErrGraphReadDeadline", err)
		}
		status, env, ok := querycontract.GraphReadErrorEnvelope(err, "code_search.fuzzy_symbol")
		if !ok || status != http.StatusGatewayTimeout || env.Code != ErrorCodeBackendTimeout {
			t.Fatalf("envelope = (%d, %#v, %t), want 504 backend_timeout", status, env, ok)
		}
	})

	t.Run("caller deadline", func(t *testing.T) {
		t.Parallel()
		reader := newPolicyTestNeo4jReader(driverReadTimeoutSession)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()

		_, err := reader.RunSingle(ctx, "MATCH (n) RETURN n", nil)

		// A deadline the graph-read policy did not set comes back as the raw
		// context.DeadlineExceeded, which every bounded-loop handler maps to
		// ErrGraphReadDeadline before WriteGraphReadError.
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v, want context.DeadlineExceeded", err)
		}
	})
}

// TestGetRelationshipsProductionReaderDriverTimeoutAnswers504 drives a
// bounded-loop handler end to end over the production Neo4jReader with the
// #7353 driver failure. It answers 504 backend_timeout, the same contract the
// entity-context handler shares; the live tests' 500 came from their own
// reader returning the raw driver error, not from this path.
func TestGetRelationshipsProductionReaderDriverTimeoutAnswers504(t *testing.T) {
	t.Parallel()

	handler := &InfraHandler{Neo4j: newPolicyTestNeo4jReader(driverReadTimeoutSession), Profile: ProfileLocalAuthoritative}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest(http.MethodPost, "/api/v0/infra/relationships", strings.NewReader(`{"entity_id":"e1"}`)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.getRelationships(rec, req)

	assertBackendTimeout(t, rec)
}
