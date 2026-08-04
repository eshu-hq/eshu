// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// capturingLanguageQueryGraphReader records the limit parameter it observed
// on the most recent Run call, so tests can prove what value actually reached
// the Cypher LIMIT clause rather than trusting the request's own claim.
type capturingLanguageQueryGraphReader struct {
	capturedLimit any
}

func (m *capturingLanguageQueryGraphReader) Run(_ context.Context, _ string, params map[string]any) ([]map[string]any, error) {
	m.capturedLimit = params["limit"]
	return nil, nil
}

func (m *capturingLanguageQueryGraphReader) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

// TestHandleLanguageQuery_ClampsOversizedLimitTo200 proves an oversized
// caller-supplied limit never reaches the Cypher LIMIT clause unclamped
// (#5761 follow-up): handleLanguageQuery only floors a non-positive limit to
// 50 (language_queries.go), and all four language_query_cypher.go builders
// splice params["limit"] straight into `LIMIT $limit` with no ceiling of
// their own, so an unbounded request limit would otherwise reach Neo4j
// verbatim.
func TestHandleLanguageQuery_ClampsOversizedLimitTo200(t *testing.T) {
	reader := &capturingLanguageQueryGraphReader{}
	h := &LanguageQueryHandler{Neo4j: reader}
	mux := http.NewServeMux()
	h.Mount(mux)

	body := `{"language":"python","entity_type":"function","limit":10000}`
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/language-query", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if reader.capturedLimit != 200 {
		t.Fatalf("Neo4j.Run received limit param = %v, want 200 (silent clamp)", reader.capturedLimit)
	}
}
