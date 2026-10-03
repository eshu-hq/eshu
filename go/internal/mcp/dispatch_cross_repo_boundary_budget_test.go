// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
)

// boundaryRelationshipCount is the live ops-qa shape behind #7129: one
// producer repository with 20 incoming repository-level relationships, none of
// which names a symbol, so every candidate row falls back to the same 20
// repository-boundary evidence items.
const boundaryRelationshipCount = 20

// boundaryRelationships returns count fully populated incoming relationship
// rows, the shape the repository relationship read model carries. Each renders
// to roughly 600 bytes as a consumer_evidence item.
func boundaryRelationships(count int) []map[string]any {
	rows := make([]map[string]any, 0, count)
	for i := 0; i < count; i++ {
		rows = append(rows, map[string]any{
			"direction":         "incoming",
			"type":              "DEPENDS_ON",
			"source_id":         fmt.Sprintf("repository:r_%08x", 0x1a2b3c4d+i),
			"source_name":       fmt.Sprintf("platform-consumer-service-%02d", i),
			"target_id":         deadCodeBudgetProducerRepoID,
			"target_name":       "payments-lib",
			"resolved_id":       fmt.Sprintf("resolved:%03d:8a1d6c40-52be-47f3-bb09-6e3c0d9a4f21", i),
			"generation_id":     "generation:5b7e2f90-1c3a-4d68-a0e4-93f1c7d2b8a6",
			"confidence":        0.91,
			"evidence_count":    3,
			"evidence_type":     "go_module_import",
			"resolution_source": "repository_dependency_resolver/go_module_import",
		})
	}
	return rows
}

// TestFindCrossRepoDeadCodeBoundaryOnlyRepositoryFitsDefaultBudget drives the
// real MCP dispatch and HTTP handler over the live #7129 shape: many candidates,
// no per-entity evidence, 20 repository-boundary relationships. The boundary
// list is the same for every row, so the reply must carry it once. est2x is the
// serialized mcpToolResult with both wire copies, the metric the dispatch
// budget enforces.
func TestFindCrossRepoDeadCodeBoundaryOnlyRepositoryFitsDefaultBudget(t *testing.T) {
	t.Parallel()

	store := newDeadCodeBudgetStore(0)
	store.RelationshipReadModel.Available = true
	store.RelationshipReadModel.Relationships = boundaryRelationships(boundaryRelationshipCount)

	result, err := dispatchTool(context.Background(), deadCodeBudgetMux(store), "find_cross_repo_dead_code",
		map[string]any{"repo_id": deadCodeBudgetProducerRepoID}, "", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil || result == nil || result.Envelope == nil {
		t.Fatalf("dispatchTool() = %#v, %v; want a canonical result", result, err)
	}
	if result.IsError {
		details := result.Envelope.Error.Details
		t.Fatalf("default-args reply is an error %s (response_bytes=%v, budget %d), want a success within budget",
			result.Envelope.Error.Code, details["response_bytes"], defaultToolResponseByteBudget)
	}
	est2x := estimateResponseBytes(result)
	t.Logf("boundary-only est2x=%d budget=%d resource_only=%v", est2x, defaultToolResponseByteBudget, result.ResourceOnly)
	if result.ResourceOnly || est2x > defaultToolResponseByteBudget {
		t.Fatalf("boundary-only est2x = %d (resource_only=%v), want both wire copies within %d",
			est2x, result.ResourceOnly, defaultToolResponseByteBudget)
	}

	data, _ := result.Envelope.Data.(map[string]any)
	hoisted, _ := data["boundary_consumer_evidence"].([]any)
	if len(hoisted) != boundaryRelationshipCount {
		t.Fatalf("boundary_consumer_evidence = %d items, want %d", len(hoisted), boundaryRelationshipCount)
	}
	if got := numberValue(data["boundary_consumer_evidence_count"]); got != boundaryRelationshipCount {
		t.Fatalf("boundary_consumer_evidence_count = %v, want %d", data["boundary_consumer_evidence_count"], boundaryRelationshipCount)
	}
}
