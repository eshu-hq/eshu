// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/taxonomy"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil/graph"
)

// TestGetEntityContextAnchorsOneLabelPerMatch pins the #7006 fix's final
// shape: the bare `MATCH (e) WHERE e.id = $entity_id` anchor scanned every
// node in the graph on every call -- proven live on ops-qa to blow the 10s
// bounded-read deadline. A single-clause label DISJUNCTION looked like the
// fix, but on this NornicDB pin `MATCH (n:A|B) WHERE n.id = $id` (and the
// inline-map form) silently returns ZERO rows for an id a single-label
// MATCH resolves correctly -- proven live for both a code-entity and an
// infra-entity id. The handler must instead issue one single-label MATCH
// per candidate in EntityContextAnchorLabels, most-common-first, and use the
// first non-nil result.
func TestGetEntityContextAnchorsOneLabelPerMatch(t *testing.T) {
	t.Parallel()

	var triedLabels []string
	reader := graph.FakeGraphReader{
		RunSingleFn: func(ctx context.Context, cypher string, _ map[string]any) (map[string]any, error) {
			// #7006 telemetry fix: the graph read must carry a bounded query
			// name so query.graph_read.warning can name which query hit the
			// deadline.
			if got, want := querycontract.GraphQueryNameFromContext(ctx), "code_search.fuzzy_symbol"; got != want {
				t.Fatalf("graph query name = %q, want %q", got, want)
			}
			if strings.Contains(cypher, "|") {
				t.Fatalf("cypher contains a label disjunction, which silently matches zero rows on the pinned NornicDB build:\n%s", cypher)
			}
			var label string
			for _, candidate := range EntityContextAnchorLabels {
				if strings.Contains(cypher, "MATCH (e:"+candidate+") WHERE e.id = $entity_id") {
					label = candidate
					break
				}
			}
			if label == "" {
				t.Fatalf("cypher does not open with a single-label anchor from EntityContextAnchorLabels:\n%s", cypher)
			}
			triedLabels = append(triedLabels, label)
			// Only the third candidate label (Struct) matches, so the loop
			// must keep trying past two misses instead of stopping at the
			// first empty result.
			if label != "Struct" {
				return nil, nil
			}
			return map[string]any{
				"id":            "entity-a",
				"labels":        []any{"Struct"},
				"name":          "PaymentBatch",
				"relationships": []any{},
			}, nil
		},
	}
	handler := &Handler{Neo4j: reader, Profile: querycontract.ProfileLocalAuthoritative}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/entities/entity-a/context", nil)
	req.SetPathValue("entity_id", "entity-a")
	rec := httptest.NewRecorder()

	handler.GetEntityContext(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if want := []string{"Function", "Class", "Struct"}; len(triedLabels) != len(want) {
		t.Fatalf("tried labels = %v, want exactly %v (stop at the first match)", triedLabels, want)
	} else {
		for i, label := range want {
			if triedLabels[i] != label {
				t.Fatalf("tried labels = %v, want %v", triedLabels, want)
			}
		}
	}
}

// TestEntityContextAnchorLabelDisjunctionCoversEveryGraphOnlyResolveEntityType
// pins the accuracy regression a too-narrow anchor would reintroduce: every
// label resolve_entity can hand back with no content-store fallback
// (taxonomy.GraphBackedEntityTypes, plus Workload from
// resolverOnlyGraphEntityTypes) must still resolve through GetEntityContext's
// per-label anchor loop, or a caller who resolved one of these ids would get
// a pre-fix-working request silently 404 after the anchor-order fix.
// Interface and TypeAlias are pinned too: real graph labels a caller can
// hold an id for even though resolve_entity cannot produce them by name.
func TestEntityContextAnchorLabelDisjunctionCoversEveryGraphOnlyResolveEntityType(t *testing.T) {
	t.Parallel()

	labels := strings.Split(EntityContextAnchorLabelDisjunction, "|")
	present := make(map[string]bool, len(labels))
	for _, label := range labels {
		present[label] = true
	}

	for _, graphOnlyLabel := range taxonomy.GraphBackedEntityTypes {
		if !present[graphOnlyLabel] {
			t.Errorf("EntityContextAnchorLabelDisjunction is missing %q, a graph-only resolve_entity type with no content-store fallback", graphOnlyLabel)
		}
	}
	for _, resolverOnlyLabel := range resolverOnlyGraphEntityTypes {
		if !present[resolverOnlyLabel] {
			t.Errorf("EntityContextAnchorLabelDisjunction is missing %q, a resolver-only graph entity type with no content-store fallback", resolverOnlyLabel)
		}
	}
	for _, callChainLabel := range []string{"Interface", "TypeAlias"} {
		if !present[callChainLabel] {
			t.Errorf("EntityContextAnchorLabelDisjunction is missing %q, a real graph label a caller can hold an id for", callChainLabel)
		}
	}
}

// TestEntityContextAnchorLabelsMatchesDisjunctionSet pins the two label
// sources (EntityContextAnchorLabels, the ordered iteration list the handler
// actually loops over, and EntityContextAnchorLabelDisjunction, the "|"-joined
// coverage-test reference) to the same set, so a future edit that updates one
// without the other is caught here instead of silently narrowing the live
// anchor loop.
func TestEntityContextAnchorLabelsMatchesDisjunctionSet(t *testing.T) {
	t.Parallel()

	fromDisjunction := make(map[string]bool)
	for _, label := range strings.Split(EntityContextAnchorLabelDisjunction, "|") {
		fromDisjunction[label] = true
	}
	fromSlice := make(map[string]bool)
	for _, label := range EntityContextAnchorLabels {
		fromSlice[label] = true
	}
	if len(fromSlice) != len(EntityContextAnchorLabels) {
		t.Fatalf("EntityContextAnchorLabels = %v, want no duplicate labels", EntityContextAnchorLabels)
	}
	for label := range fromDisjunction {
		if !fromSlice[label] {
			t.Errorf("EntityContextAnchorLabels is missing %q, present in EntityContextAnchorLabelDisjunction", label)
		}
	}
	for label := range fromSlice {
		if !fromDisjunction[label] {
			t.Errorf("EntityContextAnchorLabelDisjunction is missing %q, present in EntityContextAnchorLabels", label)
		}
	}
}
