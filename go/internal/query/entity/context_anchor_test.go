// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/taxonomy"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

func TestEntityContextProjectsLegacyKeyTypesWithoutLabelPredicates(t *testing.T) {
	t.Parallel()

	cypher := entityContextStatement("MATCH (e:Workload {id: $entity_id})", querycontract.RepositoryAccessFilter{AllScopes: true})
	for _, want := range []string{
		"labels(e) as labels",
		"e.resource_type as resource_type",
		"e.data_type as data_type",
		"target_labels: labels(target)",
		"target_resource_type: target.resource_type",
		"target_data_type: target.data_type",
	} {
		if !strings.Contains(cypher, want) {
			t.Fatalf("entity-context query does not project %q for Go-side disclosure filtering: %s", want, cypher)
		}
	}
	if strings.Contains(cypher, "IN labels(") || strings.Contains(cypher, "all(n IN nodes(") {
		t.Fatalf("entity-context query uses a label predicate that pinned NornicDB may ignore: %s", cypher)
	}
}

func TestIsLegacyAppSyncKeyNodeClassifiesEitherTypeOnlyForCloudResources(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		node map[string]any
		want bool
	}{
		{name: "resource type only", node: map[string]any{"labels": []any{"CloudResource"}, "resource_type": "aws_appsync_api_key"}, want: true},
		{name: "data type only", node: map[string]any{"labels": []any{"CloudResource"}, "data_type": "aws_appsync_api_key"}, want: true},
		{name: "conflicting type values", node: map[string]any{"labels": []any{"CloudResource"}, "resource_type": "aws_lambda", "data_type": "aws_appsync_api_key"}, want: true},
		{name: "null type values", node: map[string]any{"labels": []any{"CloudResource"}, "resource_type": nil, "data_type": nil}},
		{name: "healthy resource", node: map[string]any{"labels": []any{"CloudResource"}, "resource_type": "aws_appsync_graphql_api"}},
		{name: "same type without cloud label", node: map[string]any{"labels": []any{"Workload"}, "resource_type": "aws_appsync_api_key"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isLegacyAppSyncKeyNode(tt.node); got != tt.want {
				t.Fatalf("isLegacyAppSyncKeyNode(%#v) = %v, want %v", tt.node, got, tt.want)
			}
		})
	}
}

func TestEntityContextFiltersLegacyAppSyncKeyRelationshipTargets(t *testing.T) {
	t.Parallel()

	row := map[string]any{"relationships": []any{
		map[string]any{
			"type": "USES", "target_name": "secret-key", "target_id": "secret-key-id",
			"target_labels":        []any{"CloudResource"},
			"target_resource_type": "aws_appsync_api_key",
		},
		map[string]any{
			"type": "OWNS", "target_name": "healthy-resource", "target_id": "resource-1",
			"target_labels":        []any{"CloudResource"},
			"target_resource_type": "aws_lambda_function",
		},
		map[string]any{
			"type": "OWNS", "target_name": "healthy-resource", "target_id": "resource-1",
			"target_labels": []any{"CloudResource", "LegacyLabel"}, "target_data_type": "aws_lambda_function",
		},
	}}

	got := extractRelationships(row)
	if len(got) != 1 || got[0]["target_id"] != "resource-1" {
		t.Fatalf("relationships = %#v, want one publicly distinct healthy target", got)
	}
}

func TestGetEntityContextSuppressesLegacyAppSyncKeyAnchor(t *testing.T) {
	t.Parallel()

	const secretID = "synthetic-secret-key-id"
	reader := graph.FakeGraphReader{RunSingleFn: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
		if !strings.Contains(cypher, "MATCH (e) WHERE e.id = $entity_id") {
			return nil, nil
		}
		return map[string]any{
			"id": secretID, "labels": []any{"CloudResource"},
			"resource_type": "aws_appsync_api_key", "relationships": []any{},
		}, nil
	}}
	contentSpy := &legacyKeyFallbackContentSpy{}
	relationshipSpy := &legacyKeyFallbackRelationshipSpy{}
	handler := &Handler{
		Neo4j: reader, Profile: querycontract.ProfileLocalAuthoritative,
		Content: contentSpy, ContentRelationships: relationshipSpy,
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/entities/"+secretID+"/context", nil)
	req.SetPathValue("entity_id", secretID)
	rec := httptest.NewRecorder()

	handler.GetEntityContext(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), secretID) {
		t.Fatalf("entity-context response exposed legacy key identity: %s", rec.Body.String())
	}
	if contentSpy.getCalls != 0 || relationshipSpy.calls != 0 {
		t.Fatalf("legacy key anchor reached content fallback: GetEntityContent=%d BuildContentRelationships=%d", contentSpy.getCalls, relationshipSpy.calls)
	}
}

type legacyKeyFallbackContentSpy struct {
	content.FakePortContentStore
	getCalls int
}

func (s *legacyKeyFallbackContentSpy) GetEntityContent(context.Context, string) (*querycontract.EntityContent, error) {
	s.getCalls++
	return nil, nil
}

type legacyKeyFallbackRelationshipSpy struct {
	calls int
}

func (s *legacyKeyFallbackRelationshipSpy) BuildContentRelationships(context.Context, querycontract.ContentStore, querycontract.EntityContent, *slog.Logger) (querycontract.ContentRelationshipSet, error) {
	s.calls++
	return querycontract.ContentRelationshipSet{}, nil
}

// TestGetEntityContextAnchorsOneLabelPerMatch pins the #7006 fix's final
// shape: the bare `MATCH (e) WHERE e.id = $entity_id` anchor scanned every
// node in the graph on every call -- proven live on QA to blow the 10s
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
			// deadline. #7006 review F6: "entity.context", not the reused
			// "code_search.fuzzy_symbol" capability string, so an
			// entity-context deadline is distinguishable from a fuzzy-symbol
			// search one in telemetry.
			if got, want := querycontract.GraphQueryNameFromContext(ctx), "entity.context"; got != want {
				t.Fatalf("graph query name = %q, want %q", got, want)
			}
			if strings.Contains(cypher, "|") {
				t.Fatalf("cypher contains a label disjunction, which silently matches zero rows on the pinned NornicDB build:\n%s", cypher)
			}
			// A uid-constrained label anchors on
			// `e.uid = $entity_id AND e.id = $entity_id` (#7089); either
			// form opens with the single-label pattern.
			var label string
			for _, candidate := range EntityContextAnchorLabels {
				if strings.Contains(cypher, "MATCH (e:"+candidate+") WHERE e.id = $entity_id\n") ||
					strings.Contains(cypher, "MATCH (e:"+candidate+") WHERE e.uid = $entity_id AND e.id = $entity_id\n") {
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
