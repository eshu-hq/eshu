// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/infra/fulltext"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// readyProbeGraph answers the readiness probes as an ONLINE, fully keyed
// index.
type readyProbeGraph struct{}

func (readyProbeGraph) Run(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
	if strings.HasPrefix(cypher, "SHOW INDEXES") {
		return []map[string]any{{"state": "ONLINE"}}, nil
	}
	return []map[string]any{{"unkeyed": int64(0)}}, nil
}

func readySearchKeyReader(t *testing.T, enabled bool) *fulltext.Reader {
	t.Helper()
	readiness := fulltext.NewReadiness(readyProbeGraph{}, nil, nil)
	if !readiness.Refresh(context.Background()).Ready {
		t.Fatal("readiness not ready")
	}
	return &fulltext.Reader{Enabled: enabled, Readiness: readiness}
}

// Every shape the cutover must not touch keeps its pinned statements even
// with the flag on and the index ready: NornicDB, structured search, and the
// ArgoCD category-only read.
func TestInfraSearchFulltextLeavesLegacyShapesByteIdentical(t *testing.T) {
	t.Parallel()
	for _, c := range infraSearchLegacyCases() {
		usesIndex := c.backend == querycontract.GraphBackendNeo4j &&
			!strings.HasSuffix(c.name, "/structured") && !strings.HasSuffix(c.name, "/argocd_only")
		if usesIndex {
			continue
		}
		reader := readySearchKeyReader(t, true)
		got := statementDigest(t, serveInfraSearchLegacy(t, c, func(h *InfraHandler) { h.SearchKey = reader }))
		if want := infraSearchLegacyDigests[c.name]; got != want {
			t.Errorf("%s digest = %s, want %s (the full-text cutover changed a legacy shape)", c.name, got, want)
		}
	}
}

// With the flag off, or the index not ready, every Neo4j free-text shape keeps
// its pinned legacy statement.
func TestInfraSearchFulltextFlagOffOrNotReadyIsLegacy(t *testing.T) {
	t.Parallel()
	readers := map[string]func() *fulltext.Reader{
		"flag off":  func() *fulltext.Reader { return readySearchKeyReader(t, false) },
		"not ready": func() *fulltext.Reader { return &fulltext.Reader{Enabled: true} },
		"nil":       func() *fulltext.Reader { return nil },
	}
	for name, build := range readers {
		for _, c := range infraSearchLegacyCases() {
			reader := build()
			got := statementDigest(t, serveInfraSearchLegacy(t, c, func(h *InfraHandler) { h.SearchKey = reader }))
			if want := infraSearchLegacyDigests[c.name]; got != want {
				t.Errorf("%s: %s digest = %s, want %s", name, c.name, got, want)
			}
		}
	}
}

func TestInfraSearchFulltextSelectedOnNeo4jWhenReady(t *testing.T) {
	t.Parallel()
	for _, scoped := range []bool{false, true} {
		for _, body := range []string{`{"query":"aws_iam_role"}`, `{"query":"arn:aws:sns::123:topic"}`, `{"query":"svc","category":"cloud","kind":"x"}`} {
			c := infraSearchLegacyCase{name: body, backend: querycontract.GraphBackendNeo4j, scoped: scoped, body: body}
			reader := readySearchKeyReader(t, true)
			calls := serveInfraSearchLegacy(t, c, func(h *InfraHandler) { h.SearchKey = reader })
			if len(calls) != 1 {
				t.Fatalf("%s scoped=%v: %d statements, want 1", body, scoped, len(calls))
			}
			cypher, params := calls[0].Cypher, calls[0].Params
			if !strings.HasPrefix(cypher, "CALL db.index.fulltext.queryNodes('infra_search_key_ft', $infra_search_lucene)") {
				t.Fatalf("%s scoped=%v: statement is not the full-text read:\n%s", body, scoped, cypher)
			}
			if _, ok := params[fulltext.LuceneParam]; !ok {
				t.Fatalf("%s: missing %s param", body, fulltext.LuceneParam)
			}
			if strings.Contains(body, "::") && !strings.Contains(cypher, "= $resource_type_query") {
				t.Fatalf("%s: equality branch predicate missing:\n%s", body, cypher)
			}
			if !strings.Contains(body, "::") && !strings.Contains(cypher, infraResourceFreeTextPredicate) {
				t.Fatalf("%s: free-text predicate missing:\n%s", body, cypher)
			}
			if scoped {
				if !strings.Contains(cypher, infraResourceScopeListPredicate("n")) || strings.Contains(cypher, "DISTINCT") {
					t.Fatalf("%s: scoped statement must carry the hoisted grant predicate and no DISTINCT:\n%s", body, cypher)
				}
				if countScopeGrantIndexParams(params) != 0 || params[infraScopeGrantsParam] == nil {
					t.Fatalf("%s: scoped params = %v, want list-EXISTS params only", body, params)
				}
			} else if !strings.Contains(cypher, "RETURN DISTINCT") || strings.Contains(cypher, "allowed_repository_ids") {
				t.Fatalf("%s: unscoped statement must be DISTINCT with no grant predicate:\n%s", body, cypher)
			}
		}
	}
}
