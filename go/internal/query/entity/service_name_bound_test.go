// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// TestGetServiceContextScopedNameReadBoundsGrantedRows pins the #6801 review
// F-R5-1 fix for service context: the scoped name-keyed candidate read
// carries the grant predicate on its WHERE line and binds the grant params, so
// the 50-row bound counts granted workloads only. The Go re-check stays as
// defense in depth; this guards the predicate against a later removal.
func TestGetServiceContextScopedNameReadBoundsGrantedRows(t *testing.T) {
	t.Parallel()

	var nameCypher string
	var nameParams map[string]any
	graph := graph.FakeGraphReader{RunFn: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
		graph.AssertCypherHasNoBrokenAndOr(t, cypher)
		if strings.Contains(cypher, "w.name = $service_name") {
			nameCypher, nameParams = cypher, params
		}
		return nil, nil
	}}
	handler := &Handler{Neo4j: graph}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/services/api/context", nil).WithContext(scopedRepoAContext())
	req.SetPathValue("service_name", "api")
	rec := httptest.NewRecorder()
	handler.GetServiceContext(rec, req)

	if !strings.Contains(nameCypher, "WHERE (w.name = $service_name) AND (w.repo_id IN $allowed_repository_ids") {
		t.Fatalf("scoped name read = %q, want the grant predicate on the WHERE line", nameCypher)
	}
	if _, ok := nameParams["scope_grant_0"]; !ok {
		t.Fatalf("scoped name read params = %v, want scope_grant_0 bound for the DEFINES term", nameParams)
	}
}
