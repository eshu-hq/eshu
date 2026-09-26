// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	schemagraph "github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// schemaUIDConstrainedLabels parses the graph schema DDL for one backend into
// the set of labels that carry a uid uniqueness constraint. It reads the DDL
// itself, not any query-side label list, so the test below cannot agree with
// the handler by sharing its data.
func schemaUIDConstrainedLabels(t *testing.T, backend schemagraph.SchemaBackend) map[string]bool {
	t.Helper()
	stmts, err := schemagraph.SchemaStatementsForBackend(backend)
	if err != nil {
		t.Fatalf("SchemaStatementsForBackend(%q) error = %v", backend, err)
	}
	uidRe := regexp.MustCompile(`FOR \((\w+):(\w+)\) REQUIRE (\w+)\.uid IS UNIQUE`)
	labels := make(map[string]bool)
	for _, stmt := range stmts {
		if m := uidRe.FindStringSubmatch(stmt); m != nil && m[1] == m[3] {
			labels[m[2]] = true
		}
	}
	if len(labels) == 0 {
		t.Fatalf("parsed no uid-constrained labels from the %q schema DDL; the DDL shape changed", backend)
	}
	return labels
}

// TestGetEntityContextAnchorsUIDConstrainedLabelsOnUID pins issue #7089. On
// Neo4j the code-entity labels carry a uid uniqueness constraint and no id
// index, so `MATCH (e:Function) WHERE e.id = $entity_id` plans as a
// NodeByLabelScan over every Function node (hundreds of thousands on
// ops-qa). Canonical code entities carry id == uid, so each per-label read of
// a uid-constrained label must anchor on
// `e.uid = $entity_id AND e.id = $entity_id`: an index seek on uid that keeps
// the exact id semantics (a File, which carries a uid but no id, must still
// not match). Labels without a uid constraint keep `e.id = $entity_id`, and
// the unlabeled fallback is unchanged. The expected set comes from the schema
// DDL of both backends, and the statements are the ones the production loop
// sends.
func TestGetEntityContextAnchorsUIDConstrainedLabelsOnUID(t *testing.T) {
	t.Parallel()

	neo4jUID := schemaUIDConstrainedLabels(t, schemagraph.SchemaBackendNeo4j)
	nornicUID := schemaUIDConstrainedLabels(t, schemagraph.SchemaBackendNornicDB)

	var calls []string
	reader := graph.FakeGraphReader{
		RunSingleFn: func(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
			if got := params["entity_id"]; got != "entity-miss" {
				t.Errorf("params[entity_id] = %v, want %q", got, "entity-miss")
			}
			calls = append(calls, cypher)
			return nil, nil // every read misses, so the loop visits every label
		},
	}
	handler := &Handler{Neo4j: reader, Profile: querycontract.ProfileLocalAuthoritative}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/entities/entity-miss/context", nil)
	req.SetPathValue("entity_id", "entity-miss")
	handler.GetEntityContext(httptest.NewRecorder(), req)

	if got, want := len(calls), len(EntityContextAnchorLabels)+1; got != want {
		t.Fatalf("graph reads = %d, want %d (every anchor label, then the unlabeled fallback)", got, want)
	}
	uidAnchored, idAnchored := 0, 0
	for i, label := range EntityContextAnchorLabels {
		if neo4jUID[label] != nornicUID[label] {
			t.Fatalf("label %q: uid constraint differs between the Neo4j (%t) and NornicDB (%t) schema", label, neo4jUID[label], nornicUID[label])
		}
		want := "MATCH (e:" + label + ") WHERE e.id = $entity_id\n"
		if neo4jUID[label] {
			want = "MATCH (e:" + label + ") WHERE e.uid = $entity_id AND e.id = $entity_id\n"
			uidAnchored++
		} else {
			idAnchored++
		}
		if !strings.Contains(calls[i], want) {
			t.Errorf("read %d (label %s) anchor = %q, want %q", i, label, anchorLine(calls[i]), strings.TrimSpace(want))
		}
	}
	if uidAnchored == 0 || idAnchored == 0 {
		t.Fatalf("uid-anchored labels = %d, id-anchored labels = %d; want both nonzero or the test proves nothing", uidAnchored, idAnchored)
	}
	if last := calls[len(calls)-1]; !strings.Contains(last, entityContextUnlabeledAnchor+"\n") {
		t.Errorf("fallback read anchor = %q, want %q", anchorLine(last), entityContextUnlabeledAnchor)
	}
}

// anchorLine returns the first MATCH line of a rendered statement, so a
// failure names the anchor instead of dumping the whole projection.
func anchorLine(cypher string) string {
	for _, line := range strings.Split(cypher, "\n") {
		if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, "MATCH ") {
			return trimmed
		}
	}
	return ""
}
