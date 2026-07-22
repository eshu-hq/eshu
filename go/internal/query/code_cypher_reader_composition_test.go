// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

func TestCypherRouteComposesThirtySecondOuterWithTenSecondReaderBudget(t *testing.T) {
	t.Parallel()
	var (
		observedContextBudget time.Duration
		observedTxBudget      time.Duration
	)
	reader := newPolicyTestNeo4jReader(func(context.Context, neo4jdriver.SessionConfig) neo4jReadSession {
		return &fakeNeo4jReadSession{run: func(
			ctx context.Context,
			_ string,
			_ map[string]any,
			configurers ...func(*neo4jdriver.TransactionConfig),
		) (neo4jReadResult, error) {
			deadline, ok := ctx.Deadline()
			if !ok {
				t.Fatal("reader context has no deadline")
			}
			observedContextBudget = time.Until(deadline)
			cfg := neo4jdriver.TransactionConfig{}
			for _, configure := range configurers {
				configure(&cfg)
			}
			observedTxBudget = cfg.Timeout
			return &fakeNeo4jReadResult{records: []*neo4jdriver.Record{{Keys: []string{"value"}, Values: []any{int64(1)}}}}, nil
		}}
	})
	handler := &CodeHandler{Neo4j: reader}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/cypher", strings.NewReader(`{"cypher_query":"RETURN 1 AS value"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.handleCypherQuery(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	if cypherQueryTimeout != 30*time.Second {
		t.Fatalf("outer route timeout = %s, want 30s", cypherQueryTimeout)
	}
	if reader.policy.readTimeout != 10*time.Second {
		t.Fatalf("reader timeout = %s, want 10s", reader.policy.readTimeout)
	}
	if observedContextBudget <= 9*time.Second || observedContextBudget > 10*time.Second {
		t.Fatalf("reader context budget = %s, want (9s, 10s]", observedContextBudget)
	}
	if observedTxBudget <= 9*time.Second || observedTxBudget > 10*time.Second {
		t.Fatalf("backend transaction budget = %s, want (9s, 10s]", observedTxBudget)
	}
	budgetDifference := observedTxBudget - observedContextBudget
	if budgetDifference < 0 {
		budgetDifference = -budgetDifference
	}
	if budgetDifference > 20*time.Millisecond {
		t.Fatalf("reader/backend budget difference = %s, want <= 20ms", budgetDifference)
	}
}

func TestCypherRouteMapsRetryableNeo4jAvailabilityFailureToSanitized503(t *testing.T) {
	const privateCause = "bolt://private-availability.example.invalid:7687"
	attempts := 0
	reader := newPolicyTestNeo4jReader(func(context.Context, neo4jdriver.SessionConfig) neo4jReadSession {
		attempts++
		return &fakeNeo4jReadSession{run: func(
			context.Context,
			string,
			map[string]any,
			...func(*neo4jdriver.TransactionConfig),
		) (neo4jReadResult, error) {
			return nil, &neo4jdriver.Neo4jError{
				Code: "Neo.TransientError.General.DatabaseUnavailable",
				Msg:  privateCause,
			}
		}}
	})
	handler := &CodeHandler{Neo4j: reader}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/cypher", strings.NewReader(`{"cypher_query":"RETURN 1"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.handleCypherQuery(rec, req)

	if rec.Code != http.StatusServiceUnavailable || attempts != maxGraphReadAttempts {
		t.Fatalf("response = %d after %d attempts body=%s, want sanitized 503 after bounded retry", rec.Code, attempts, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, `"code":"backend_unavailable"`) || strings.Contains(body, privateCause) {
		t.Fatalf("response body = %s, want backend_unavailable without private cause", body)
	}
}
