// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strings"
	"testing"

	schemagraph "github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// schemaIDConstrainedLabels parses one backend's schema DDL into the labels
// that carry an id uniqueness constraint. Like schemaUIDConstrainedLabels it
// reads the DDL, not any query-side list.
func schemaIDConstrainedLabels(t *testing.T, backend schemagraph.SchemaBackend) map[string]bool {
	t.Helper()
	stmts, err := schemagraph.SchemaStatementsForBackend(backend)
	if err != nil {
		t.Fatalf("SchemaStatementsForBackend(%q) error = %v", backend, err)
	}
	idRe := regexp.MustCompile(`FOR \((\w+):(\w+)\) REQUIRE (\w+)\.id IS UNIQUE`)
	labels := make(map[string]bool)
	for _, stmt := range stmts {
		if m := idRe.FindStringSubmatch(stmt); m != nil && m[1] == m[3] {
			labels[m[2]] = true
		}
	}
	if len(labels) == 0 {
		t.Fatalf("parsed no id-constrained labels from the %q schema DDL; the DDL shape changed", backend)
	}
	return labels
}

// neo4jContextRequest drives GetEntityContext on a Neo4j-dialect handler and
// returns every statement the anchor loop sent, in order. scoped adds a
// repository grant so the scoped statement text (its own plan-cache entry) is
// the one exercised.
func neo4jContextRequest(
	t *testing.T,
	entityID string,
	scoped bool,
	run func(cypher string) (map[string]any, error),
) ([]string, *httptest.ResponseRecorder) {
	t.Helper()
	var calls []string
	reader := graph.FakeGraphReader{
		RunSingleFn: func(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
			if got := params["entity_id"]; got != entityID {
				t.Errorf("params[entity_id] = %v, want %q", got, entityID)
			}
			calls = append(calls, cypher)
			return run(cypher)
		},
	}
	handler := &Handler{
		GraphBackend: querycontract.GraphBackendNeo4j,
		Neo4j:        reader,
		Profile:      querycontract.ProfileLocalAuthoritative,
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/entities/"+entityID+"/context", nil)
	if scoped {
		req = req.WithContext(auth.ContextWithAuthContext(req.Context(), auth.AuthContext{
			Mode:                 auth.AuthModeScoped,
			AllowedRepositoryIDs: []string{"repo-a"},
		}))
	}
	req.SetPathValue("entity_id", entityID)
	rec := httptest.NewRecorder()
	handler.GetEntityContext(rec, req)
	return calls, rec
}

// TestNeo4jEntityContextAnchorHitSkipsFallback: an anchor hit answers after
// one read. The one-statement miss path, and the removal of the unlabeled
// fallback (#7212), are pinned in context_anchor_closed_test.go.
func TestNeo4jEntityContextAnchorHitSkipsFallback(t *testing.T) {
	t.Parallel()

	calls, rec := neo4jContextRequest(t, "fn-1", false, func(string) (map[string]any, error) {
		return map[string]any{
			"id": "fn-1", "labels": []any{"Function"}, "name": "Main",
			"relationships": []any{},
		}, nil
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if len(calls) != 1 {
		t.Fatalf("graph reads on an anchor hit = %d, want 1", len(calls))
	}
}

// TestNeo4jEntityContextStatementTextIsConstantAcrossIDs: the id travels as a
// bound parameter, so the same text serves every request. A per-id
// literal would defeat the plan cache the whole change relies on.
func TestNeo4jEntityContextStatementTextIsConstantAcrossIDs(t *testing.T) {
	t.Parallel()

	miss := func(string) (map[string]any, error) { return nil, nil }
	first, _ := neo4jContextRequest(t, "id-one", false, miss)
	second, _ := neo4jContextRequest(t, "id-two", false, miss)
	if len(first) != len(second) {
		t.Fatalf("statement counts differ: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Errorf("statement %d text differs between ids:\n%s\n----\n%s", i, first[i], second[i])
		}
		if strings.Contains(first[i], "id-one") {
			t.Errorf("statement %d inlines the entity id instead of binding $entity_id", i)
		}
	}
}

// labelSet splits a "A|B|C" disjunction into a sorted set.
func labelSet(disjunction string) []string {
	labels := strings.Split(disjunction, "|")
	sort.Strings(labels)
	return labels
}

// TestEntityContextNonNeo4jBackendsKeepThePerLabelLoop: NornicDB (and the
// zero value, which defaults to it) must not see the CALL () anchor. A label
// disjunction silently returns zero rows there (#7006), so its 16-statement
// loop is the only correct shape.
func TestEntityContextNonNeo4jBackendsKeepThePerLabelLoop(t *testing.T) {
	t.Parallel()

	for _, backend := range []querycontract.GraphBackend{"", querycontract.GraphBackendNornicDB} {
		var calls []string
		reader := graph.FakeGraphReader{
			RunSingleFn: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
				calls = append(calls, cypher)
				return nil, nil
			},
		}
		handler := &Handler{GraphBackend: backend, Neo4j: reader, Profile: querycontract.ProfileLocalAuthoritative}
		req := httptest.NewRequest(http.MethodGet, "/api/v0/entities/x/context", nil)
		req.SetPathValue("entity_id", "x")
		handler.GetEntityContext(httptest.NewRecorder(), req)
		if got, want := len(calls), len(EntityContextAnchorLabels)+1; got != want {
			t.Errorf("backend %q: graph reads = %d, want %d (per-label loop)", backend, got, want)
		}
		for _, cypher := range calls {
			if strings.Contains(cypher, "CALL () {") {
				t.Errorf("backend %q: statement uses the Neo4j-only CALL () anchor:\n%s", backend, cypher)
			}
		}
	}
}
