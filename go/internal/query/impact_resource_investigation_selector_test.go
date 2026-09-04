// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

type resourceInvestigationSelectorGraph struct {
	mu        sync.Mutex
	calls     []resourceInvestigationRunCall
	active    int
	maxActive int
	fuzzyOnly bool
	duplicate bool
	failExact bool
}

func (g *resourceInvestigationSelectorGraph) Run(
	_ context.Context,
	cypher string,
	params map[string]any,
) ([]map[string]any, error) {
	g.mu.Lock()
	g.calls = append(g.calls, resourceInvestigationRunCall{cypher: cypher, params: params})
	g.active++
	if g.active > g.maxActive {
		g.maxActive = g.active
	}
	g.mu.Unlock()
	time.Sleep(5 * time.Millisecond)
	g.mu.Lock()
	g.active--
	g.mu.Unlock()

	exactName := strings.Contains(cypher, "coalesce(n.name, '') = $selector")
	legacyCombined := strings.Contains(cypher, "n.name = $selector") && strings.Contains(cypher, "n.name CONTAINS $selector")
	fuzzyName := strings.Contains(cypher, "coalesce(n.name, '') CONTAINS $selector")
	exactID := strings.Contains(cypher, "coalesce(n.id, '') = $selector")
	if g.failExact && exactID {
		return nil, errors.New("exact selector read failed")
	}
	if legacyCombined {
		if g.fuzzyOnly {
			return []map[string]any{selectorCandidateRow("resource:fuzzy", "orders-shadow", "repo-allowed")}, nil
		}
		return []map[string]any{
			selectorCandidateRow("resource:exact", "orders", "repo-allowed"),
			selectorCandidateRow("resource:prefix", "orders-shadow", "repo-allowed"),
		}, nil
	}
	if g.fuzzyOnly && fuzzyName {
		return []map[string]any{selectorCandidateRow("resource:fuzzy", "orders-shadow", "repo-allowed")}, nil
	}
	if !g.fuzzyOnly && exactName {
		return []map[string]any{selectorCandidateRow("resource:exact", "orders", "repo-allowed")}, nil
	}
	if g.duplicate && exactID {
		return []map[string]any{selectorCandidateRow("resource:exact", "orders", "repo-allowed")}, nil
	}
	return nil, nil
}

func (*resourceInvestigationSelectorGraph) RunSingle(
	context.Context,
	string,
	map[string]any,
) (map[string]any, error) {
	return nil, nil
}

func selectorCandidateRow(id, name, repoID string) map[string]any {
	return map[string]any{
		"id": id, "name": name, "labels": []any{"CloudResource"},
		"repo_id": repoID, "resource_type": "database",
	}
}

func TestResourceInvestigationSelectorPrefersExactMatchBeforeFuzzy(t *testing.T) {
	t.Parallel()

	graph := &resourceInvestigationSelectorGraph{}
	handler := &ImpactHandler{Neo4j: graph}
	req := resourceInvestigationRequest{Query: "orders", Limit: 5}
	selected, resolution, err := handler.resolveResourceInvestigationTarget(context.Background(), req)
	if err != nil {
		t.Fatalf("resolveResourceInvestigationTarget() error = %v", err)
	}
	if selected == nil || selected.ID != "resource:exact" {
		t.Fatalf("selected = %#v, want exact resource", selected)
	}
	if got, want := resolution["status"], "resolved"; got != want {
		t.Fatalf("resolution.status = %#v, want %#v", got, want)
	}
	assertResourceSelectorFanout(t, graph, len(resourceInvestigationDefaultLabels), 0)
}

func TestResourceInvestigationSelectorFallsBackToFuzzyAfterExactMiss(t *testing.T) {
	t.Parallel()

	graph := &resourceInvestigationSelectorGraph{fuzzyOnly: true}
	handler := &ImpactHandler{Neo4j: graph}
	req := resourceInvestigationRequest{Query: "orders", Limit: 5}
	selected, resolution, err := handler.resolveResourceInvestigationTarget(context.Background(), req)
	if err != nil {
		t.Fatalf("resolveResourceInvestigationTarget() error = %v", err)
	}
	if selected == nil || selected.ID != "resource:fuzzy" {
		t.Fatalf("selected = %#v, want fuzzy fallback resource", selected)
	}
	if got, want := resolution["status"], "resolved"; got != want {
		t.Fatalf("resolution.status = %#v, want %#v", got, want)
	}
	assertResourceSelectorFanout(t, graph, len(resourceInvestigationDefaultLabels), len(resourceInvestigationDefaultLabels))
}

func TestResourceInvestigationResourceIDNeverFallsBackToFuzzy(t *testing.T) {
	t.Parallel()

	graph := &resourceInvestigationSelectorGraph{fuzzyOnly: true}
	handler := &ImpactHandler{Neo4j: graph}
	req := resourceInvestigationRequest{ResourceID: "missing-resource", Limit: 5}
	selected, resolution, err := handler.resolveResourceInvestigationTarget(context.Background(), req)
	if err != nil {
		t.Fatalf("resolveResourceInvestigationTarget() error = %v", err)
	}
	if selected != nil {
		t.Fatalf("selected = %#v, want nil", selected)
	}
	if got, want := resolution["status"], "no_match"; got != want {
		t.Fatalf("resolution.status = %#v, want %#v", got, want)
	}
	assertResourceSelectorFanout(t, graph, len(resourceInvestigationDefaultLabels), 0)
}

func TestResourceInvestigationSelectorDeduplicatesLabelAndPropertyCollisions(t *testing.T) {
	t.Parallel()

	graph := &resourceInvestigationSelectorGraph{duplicate: true}
	handler := &ImpactHandler{Neo4j: graph}
	req := resourceInvestigationRequest{Query: "orders", Limit: 5}
	selected, resolution, err := handler.resolveResourceInvestigationTarget(context.Background(), req)
	if err != nil {
		t.Fatalf("resolveResourceInvestigationTarget() error = %v", err)
	}
	if selected == nil || selected.ID != "resource:exact" {
		t.Fatalf("selected = %#v, want one deduplicated exact resource", selected)
	}
	if got, want := resolution["status"], "resolved"; got != want {
		t.Fatalf("resolution.status = %#v, want %#v", got, want)
	}
	assertResourceSelectorFanout(t, graph, len(resourceInvestigationDefaultLabels), 0)
}

func assertResourceSelectorFanout(
	t *testing.T,
	graph *resourceInvestigationSelectorGraph,
	wantExact int,
	wantFuzzy int,
) {
	t.Helper()
	graph.mu.Lock()
	defer graph.mu.Unlock()
	var exact, fuzzy int
	for _, call := range graph.calls {
		if strings.Contains(call.cypher, "MATCH (n)\n") {
			t.Fatalf("selector query regressed to global MATCH (n):\n%s", call.cypher)
		}
		if !strings.HasPrefix(strings.TrimSpace(call.cypher), "MATCH (n:") {
			t.Fatalf("selector query is not directly label-anchored:\n%s", call.cypher)
		}
		limitAt := strings.LastIndex(call.cypher, "LIMIT $limit")
		if limitAt < 0 {
			t.Fatalf("selector query missing LIMIT $limit:\n%s", call.cypher)
		}
		if strings.Contains(call.cypher, "CONTAINS $selector") {
			fuzzy++
		} else {
			exact++
		}
	}
	if exact != wantExact || fuzzy != wantFuzzy {
		t.Fatalf("selector calls exact/fuzzy = %d/%d, want %d/%d", exact, fuzzy, wantExact, wantFuzzy)
	}
	if graph.maxActive < 2 {
		t.Fatalf("selector max concurrent reads = %d, want bounded fanout > 1", graph.maxActive)
	}
	if graph.maxActive > resourceInvestigationSelectorConcurrency {
		t.Fatalf(
			"selector max concurrent reads = %d, exceeds cap %d",
			graph.maxActive,
			resourceInvestigationSelectorConcurrency,
		)
	}
}

func TestResourceInvestigationSelectorReturnsExactReadFailureWithoutFuzzyFallback(t *testing.T) {
	t.Parallel()

	graph := &resourceInvestigationSelectorGraph{failExact: true}
	handler := &ImpactHandler{Neo4j: graph}
	selected, resolution, err := handler.resolveResourceInvestigationTarget(
		context.Background(),
		resourceInvestigationRequest{Query: "orders", Limit: 5},
	)
	if err == nil || !strings.Contains(err.Error(), "exact selector read failed") {
		t.Fatalf("resolveResourceInvestigationTarget() error = %v, want exact read failure", err)
	}
	if selected != nil || resolution != nil {
		t.Fatalf("selected/resolution = %#v/%#v, want nil/nil on read failure", selected, resolution)
	}
	graph.mu.Lock()
	defer graph.mu.Unlock()
	if got, want := len(graph.calls), len(resourceInvestigationDefaultLabels); got != want {
		t.Fatalf("selector calls = %d, want %d exact label calls and no fuzzy fallback", got, want)
	}
}

func TestResourceInvestigationSelectorRecordsBoundedOutcomeTelemetry(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	ctx, span := provider.Tracer("resource-selector-test").Start(context.Background(), "selector")
	candidates := []resourceInvestigationCandidate{
		{ID: "resource:one", Name: "orders"},
		{ID: "resource:two", Name: "orders"},
	}
	_, _, err := resourceInvestigationSelectorResolution(
		ctx,
		resourceInvestigationRequest{Query: "orders", Limit: 1},
		candidates,
		"exact",
		25*time.Millisecond,
	)
	if err != nil {
		span.End()
		t.Fatalf("resourceInvestigationSelectorResolution() error = %v", err)
	}
	span.End()
	spans := recorder.Ended()
	if got, want := len(spans), 1; got != want {
		t.Fatalf("ended spans = %d, want %d", got, want)
	}
	attributes := spans[0].Attributes()
	assertResourceSelectorAttribute(t, attributes, "eshu.resource_investigation.selector_phase", "exact")
	assertResourceSelectorAttribute(t, attributes, "eshu.resource_investigation.selector_candidate_count", int64(2))
	assertResourceSelectorAttribute(t, attributes, "eshu.resource_investigation.selector_ambiguous", true)
	assertResourceSelectorAttribute(t, attributes, "eshu.resource_investigation.selector_truncated", true)
	if seconds := resourceSelectorAttribute(attributes, "eshu.resource_investigation.selector_seconds"); seconds == nil || seconds.(float64) <= 0 {
		t.Fatalf("selector_seconds = %#v, want positive duration", seconds)
	}
}

func assertResourceSelectorAttribute(t *testing.T, attributes []attribute.KeyValue, key string, want any) {
	t.Helper()
	if got := resourceSelectorAttribute(attributes, key); got != want {
		t.Fatalf("%s = %#v, want %#v", key, got, want)
	}
}

func resourceSelectorAttribute(attributes []attribute.KeyValue, key string) any {
	for _, candidate := range attributes {
		if string(candidate.Key) != key {
			continue
		}
		switch candidate.Value.Type() {
		case attribute.STRING:
			return candidate.Value.AsString()
		case attribute.INT64:
			return candidate.Value.AsInt64()
		case attribute.BOOL:
			return candidate.Value.AsBool()
		case attribute.FLOAT64:
			return candidate.Value.AsFloat64()
		}
	}
	return nil
}

func TestResourceInvestigationScopedSelectorAuthorizesBeforeEveryLimit(t *testing.T) {
	t.Parallel()

	graph := &resourceInvestigationSelectorGraph{}
	handler := &ImpactHandler{Neo4j: graph}
	ctx := ContextWithAuthContext(
		context.Background(),
		scopedTestAuthContext("tenant-a", []string{"repo-allowed"}),
	)
	selected, _, err := handler.resolveResourceInvestigationTarget(
		ctx,
		resourceInvestigationRequest{Query: "orders", Limit: 5},
	)
	if err != nil {
		t.Fatalf("resolveResourceInvestigationTarget() error = %v", err)
	}
	if selected == nil || selected.ID != "resource:exact" {
		t.Fatalf("selected = %#v, want granted exact resource", selected)
	}
	graph.mu.Lock()
	defer graph.mu.Unlock()
	for _, call := range graph.calls {
		cypher := call.cypher
		grantAt := strings.Index(cypher, "n.repo_id IN $allowed_repository_ids")
		limitAt := strings.LastIndex(cypher, "LIMIT $limit")
		if grantAt < 0 || limitAt < 0 || grantAt > limitAt {
			t.Fatalf("grant predicate must precede selection limit:\n%s", cypher)
		}
	}
}
