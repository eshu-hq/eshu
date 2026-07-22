// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// TestDispatchToolWorkloadContextReturnsHardenedEnvelope proves the workload
// context tool exposes the canonical truth envelope plus the additive
// result_limits drilldown block and explicit partial_reasons slot through MCP
// dispatch, matching the HTTP surface.
func TestDispatchToolWorkloadContextReturnsHardenedEnvelope(t *testing.T) {
	t.Parallel()

	data := dispatchContextEnvelopeData(t, "get_workload_context", map[string]any{"workload_id": "workload-1"})
	requireContextLimitsAndReasons(t, data, "get_workload_story", "/api/v0/workloads/workload-1/context")
}

// TestDispatchToolWorkloadStoryReturnsHardenedEnvelope proves the workload
// story tool exposes the same hardened envelope metadata through MCP dispatch.
func TestDispatchToolWorkloadStoryReturnsHardenedEnvelope(t *testing.T) {
	t.Parallel()

	data := dispatchContextEnvelopeData(t, "get_workload_story", map[string]any{"workload_id": "workload-1"})
	if story := query.StringVal(data, "story"); !strings.Contains(story, "Workload order-service") {
		t.Fatalf("data.story = %q, want workload narrative", story)
	}
	requireContextLimitsAndReasons(t, data, "get_workload_context", "/api/v0/workloads/workload-1/context")
}

// TestDispatchToolEntityContextReturnsHardenedEnvelope proves the entity
// context tool exposes the hardened result_limits drilldown block and explicit
// partial_reasons slot through MCP dispatch.
func TestDispatchToolEntityContextReturnsHardenedEnvelope(t *testing.T) {
	t.Parallel()

	data := dispatchContextEnvelopeData(t, "get_entity_context", map[string]any{"entity_id": "entity-1"})
	requireContextLimitsAndReasons(t, data, "get_relationship_evidence", "/api/v0/entities/entity-1/context")
}

func TestDispatchToolEntityContextDeterministicallyCapsRelationships(t *testing.T) {
	t.Parallel()

	handler := &query.EntityHandler{Neo4j: overLimitEntityContextGraphReader{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for run := 0; run < 2; run++ {
		result, err := dispatchTool(
			context.Background(),
			mux,
			"get_entity_context",
			map[string]any{"entity_id": "entity-1"},
			"",
			slog.New(slog.NewTextHandler(io.Discard, nil)),
		)
		if err != nil {
			t.Fatalf("run %d dispatchTool() error = %v, want nil", run, err)
		}
		data, ok := result.Envelope.Data.(map[string]any)
		if !ok {
			t.Fatalf("run %d data type = %T, want map[string]any", run, result.Envelope.Data)
		}
		rows, ok := data["relationships"].([]any)
		if !ok {
			t.Fatalf("run %d relationships type = %T, want []any", run, data["relationships"])
		}
		if got, want := len(rows), 50; got != want {
			t.Fatalf("run %d relationship payload len = %d, want %d", run, got, want)
		}
		first, _ := rows[0].(map[string]any)
		last, _ := rows[len(rows)-1].(map[string]any)
		if got, want := query.StringVal(first, "target_name"), "target-000"; got != want {
			t.Fatalf("run %d first target = %q, want %q", run, got, want)
		}
		if got, want := query.StringVal(last, "target_name"), "target-049"; got != want {
			t.Fatalf("run %d last target = %q, want %q", run, got, want)
		}
		limits := mcpMapValue(data, "result_limits")
		if got, want := query.IntVal(limits, "relationship_count"), 60; got != want {
			t.Fatalf("run %d relationship_count = %d, want %d", run, got, want)
		}
		if truncated, _ := limits["truncated"].(bool); !truncated {
			t.Fatalf("run %d result_limits.truncated = false, want true", run)
		}
	}
}

func dispatchContextEnvelopeData(t *testing.T, tool string, args map[string]any) map[string]any {
	t.Helper()

	handler := &query.EntityHandler{
		Neo4j:   contextEnvelopeGraphReader{},
		Profile: query.ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	result, err := dispatchTool(
		context.Background(),
		mux,
		tool,
		args,
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool(%q) error = %v, want nil", tool, err)
	}
	if result.Envelope == nil {
		t.Fatalf("dispatchTool(%q) envelope is nil, want canonical envelope", tool)
	}
	if result.Envelope.Truth == nil {
		t.Fatalf("dispatchTool(%q) envelope truth is nil, want truth envelope", tool)
	}
	data, ok := result.Envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("dispatchTool(%q) data type = %T, want map[string]any", tool, result.Envelope.Data)
	}
	return data
}

func requireContextLimitsAndReasons(t *testing.T, data map[string]any, wantTool, wantPath string) {
	t.Helper()

	limits := mcpMapValue(data, "result_limits")
	if len(limits) == 0 {
		t.Fatalf("data.result_limits missing, want bounded drilldown block; data keys = %v", mapKeys(data))
	}
	if limits["limit"] == nil {
		t.Fatal("result_limits.limit missing, want bounded limit")
	}
	if got, want := query.StringVal(limits, "ordering"), "deterministic"; got != want {
		t.Fatalf("result_limits.ordering = %q, want %q", got, want)
	}
	if _, ok := limits["truncated"].(bool); !ok {
		t.Fatalf("result_limits.truncated type = %T, want bool", limits["truncated"])
	}
	if got, want := query.StringVal(limits, "drilldown_tool"), wantTool; got != want {
		t.Fatalf("result_limits.drilldown_tool = %q, want %q", got, want)
	}
	if got, want := query.StringVal(limits, "context_path"), wantPath; got != want {
		t.Fatalf("result_limits.context_path = %q, want %q", got, want)
	}
	if _, ok := data["partial_reasons"]; !ok {
		t.Fatal("data.partial_reasons missing, want explicit partial reason slot")
	}
}

func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	return keys
}

// contextEnvelopeGraphReader returns a single workload row for workload routes
// and a single entity row for the entity context route. Enrichment fan-out
// queries return empty so the bounded context payload stays minimal.
type contextEnvelopeGraphReader struct{}

func (contextEnvelopeGraphReader) Run(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
	return nil, nil
}

type overLimitEntityContextGraphReader struct{}

func (overLimitEntityContextGraphReader) Run(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
	return nil, nil
}

func (overLimitEntityContextGraphReader) RunSingle(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
	if !strings.Contains(cypher, "WHERE e.id = $entity_id") {
		return nil, nil
	}
	relationships := make([]any, 60)
	for i := range relationships {
		index := len(relationships) - i - 1
		relationships[i] = map[string]any{
			"type":        "DEPENDS_ON",
			"target_name": fmt.Sprintf("target-%03d", index),
			"target_id":   fmt.Sprintf("repository:%03d", index),
		}
	}
	return map[string]any{
		"id":            "entity-1",
		"labels":        []any{"File"},
		"name":          "ci",
		"file_path":     ".github/workflows/ci.yml",
		"repo_id":       "repo-1",
		"relationships": relationships,
	}, nil
}

func (contextEnvelopeGraphReader) RunSingle(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
	switch {
	case strings.Contains(cypher, "WHERE e.id = $entity_id"):
		return map[string]any{
			"id":            "entity-1",
			"labels":        []any{"Function"},
			"name":          "handler",
			"file_path":     "src/handler.go",
			"repo_id":       "repo-1",
			"repo_name":     "repo-1",
			"language":      "go",
			"start_line":    int64(12),
			"end_line":      int64(24),
			"relationships": []any{},
		}, nil
	case strings.Contains(cypher, "MATCH (w:Workload)"):
		return map[string]any{
			"id":        "workload-1",
			"name":      "order-service",
			"kind":      "Deployment",
			"repo_id":   "repo-1",
			"repo_name": "order-service",
			"instances": []any{},
		}, nil
	default:
		return nil, nil
	}
}
