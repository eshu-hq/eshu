// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"
)

func crossplaneSatisfiedByEdgeRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		suffix := string(rune('a' + i))
		rows = append(rows, map[string]any{
			"claim_uid":       "content-entity:claim-" + suffix,
			"xrd_uid":         "content-entity:xrd-" + suffix,
			"rel_type":        "SATISFIED_BY",
			"resolution_mode": "group_claim_kind",
			"claim_group":     "database.example.org",
			"claim_kind":      "PostgreSQLInstance",
		})
	}
	return rows
}

func TestCrossplaneSatisfiedByEdgeWriterEmptyRowsIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCrossplaneSatisfiedByEdgeWriter(executor, 0)

	if err := writer.WriteCrossplaneSatisfiedByEdges(context.Background(), nil, "scope-1", "gen-1", crossplaneSatisfiedByEvidenceSource); err != nil {
		t.Fatalf("WriteCrossplaneSatisfiedByEdges returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty rows", len(executor.calls))
	}
}

func TestCrossplaneSatisfiedByEdgeWriterUsesStaticRelTypeMatchMatchMerge(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCrossplaneSatisfiedByEdgeWriter(executor, 0)

	if err := writer.WriteCrossplaneSatisfiedByEdges(context.Background(), crossplaneSatisfiedByEdgeRows(1), "scope-1", "gen-1", crossplaneSatisfiedByEvidenceSource); err != nil {
		t.Fatalf("WriteCrossplaneSatisfiedByEdges returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	if !strings.Contains(cypher, "UNWIND $rows AS row") {
		t.Fatalf("cypher missing UNWIND batch shape:\n%s", cypher)
	}
	// Two MATCHes before the MERGE guarantee a missing endpoint is a no-op,
	// never a fabricated node.
	if !strings.Contains(cypher, "MATCH (claim:K8sResource {uid: row.claim_uid})") {
		t.Fatalf("cypher must MATCH the claim by uid:\n%s", cypher)
	}
	if !strings.Contains(cypher, "MATCH (xrd:CrossplaneXRD {uid: row.xrd_uid})") {
		t.Fatalf("cypher must MATCH the XRD by uid:\n%s", cypher)
	}
	if strings.Contains(cypher, "MERGE (claim:K8sResource") || strings.Contains(cypher, "MERGE (xrd:CrossplaneXRD") {
		t.Fatalf("cypher must not MERGE (fabricate) endpoint nodes:\n%s", cypher)
	}
	if !strings.Contains(cypher, "MERGE (claim)-[rel:SATISFIED_BY]->(xrd)") {
		t.Fatalf("edge MERGE must use the static SATISFIED_BY relationship type:\n%s", cypher)
	}
	if strings.Contains(cypher, "{rel_type: row.rel_type}") {
		t.Fatalf("rel_type must not live inside MERGE identity:\n%s", cypher)
	}
}

func TestCrossplaneSatisfiedByEdgeWriterBatchesRows(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCrossplaneSatisfiedByEdgeWriter(executor, 2)

	if err := writer.WriteCrossplaneSatisfiedByEdges(context.Background(), crossplaneSatisfiedByEdgeRows(5), "scope-1", "gen-1", crossplaneSatisfiedByEvidenceSource); err != nil {
		t.Fatalf("WriteCrossplaneSatisfiedByEdges returned error: %v", err)
	}
	if len(executor.calls) != 3 {
		t.Fatalf("len(calls) = %d, want 3 batched statements", len(executor.calls))
	}
}

func TestCrossplaneSatisfiedByEdgeWriterUsesGroupExecutorAtomically(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewCrossplaneSatisfiedByEdgeWriter(executor, 2)

	if err := writer.WriteCrossplaneSatisfiedByEdges(context.Background(), crossplaneSatisfiedByEdgeRows(5), "scope-1", "gen-1", crossplaneSatisfiedByEvidenceSource); err != nil {
		t.Fatalf("WriteCrossplaneSatisfiedByEdges returned error: %v", err)
	}
	if len(executor.groupCalls) != 1 {
		t.Fatalf("len(groupCalls) = %d, want 1 atomic group", len(executor.groupCalls))
	}
	if len(executor.groupCalls[0]) != 3 {
		t.Fatalf("group statement count = %d, want 3", len(executor.groupCalls[0]))
	}
}

func TestCrossplaneSatisfiedByEdgeWriterAnnotatesScopeGenerationEvidence(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCrossplaneSatisfiedByEdgeWriter(executor, 0)

	if err := writer.WriteCrossplaneSatisfiedByEdges(context.Background(), crossplaneSatisfiedByEdgeRows(1), "scope-1", "gen-1", crossplaneSatisfiedByEvidenceSource); err != nil {
		t.Fatalf("WriteCrossplaneSatisfiedByEdges returned error: %v", err)
	}
	rows, ok := executor.calls[0].Parameters["rows"].([]map[string]any)
	if !ok {
		t.Fatalf("rows parameter type = %T, want []map[string]any", executor.calls[0].Parameters["rows"])
	}
	if got := rows[0]["scope_id"]; got != "scope-1" {
		t.Fatalf("scope_id = %v, want scope-1 (injected for scope-scoped retract)", got)
	}
	if got := rows[0]["generation_id"]; got != "gen-1" {
		t.Fatalf("generation_id = %v, want gen-1", got)
	}
	if got := rows[0]["evidence_source"]; got != crossplaneSatisfiedByEvidenceSource {
		t.Fatalf("evidence_source = %v, want %v", got, crossplaneSatisfiedByEvidenceSource)
	}
	cypher := executor.calls[0].Cypher
	for _, want := range []string{
		"rel.scope_id = row.scope_id",
		"rel.generation_id = row.generation_id",
		"rel.evidence_source = row.evidence_source",
		"rel.resolution_mode = row.resolution_mode",
		"rel.claim_group = row.claim_group",
		"rel.claim_kind = row.claim_kind",
	} {
		if !strings.Contains(cypher, want) {
			t.Fatalf("cypher must persist %q:\n%s", want, cypher)
		}
	}
}

func TestCrossplaneSatisfiedByEdgeWriterRetractScopesByEvidenceSource(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCrossplaneSatisfiedByEdgeWriter(executor, 0)

	if err := writer.RetractCrossplaneSatisfiedByEdges(
		context.Background(),
		[]string{"scope-1"},
		"gen-1",
		crossplaneSatisfiedByEvidenceSource,
	); err != nil {
		t.Fatalf("RetractCrossplaneSatisfiedByEdges returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1 retract statement", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	if !strings.Contains(cypher, "MATCH (:K8sResource)-[rel:SATISFIED_BY]->(:CrossplaneXRD)") {
		t.Fatalf("retract must target reducer-owned SATISFIED_BY edges:\n%s", cypher)
	}
	// The retract MUST filter on the edge's own scope_id. K8sResource and
	// CrossplaneXRD nodes are cross-generation canonical and carry no
	// reducer scope_id, so a node-scoped predicate would be a silent no-op
	// that leaks stale edges.
	if !strings.Contains(cypher, "rel.scope_id IN $scope_ids") {
		t.Fatalf("retract must filter by the edge scope_id:\n%s", cypher)
	}
	if !strings.Contains(cypher, "rel.evidence_source = $evidence_source") {
		t.Fatalf("retract must be scoped to this writer's evidence_source:\n%s", cypher)
	}
	if !strings.Contains(cypher, "DELETE rel") {
		t.Fatalf("retract must DELETE only the edge:\n%s", cypher)
	}
	if strings.Contains(cypher, "DETACH DELETE") {
		t.Fatalf("retract must not delete endpoint nodes:\n%s", cypher)
	}
}

func TestCrossplaneSatisfiedByEdgeWriterRetractEmptyScopesIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCrossplaneSatisfiedByEdgeWriter(executor, 0)

	if err := writer.RetractCrossplaneSatisfiedByEdges(context.Background(), nil, "gen-1", crossplaneSatisfiedByEvidenceSource); err != nil {
		t.Fatalf("RetractCrossplaneSatisfiedByEdges returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty scope set", len(executor.calls))
	}
}

func TestCrossplaneRelationshipMaterializedEdgeTypesListsSatisfiedBy(t *testing.T) {
	t.Parallel()

	got := CrossplaneRelationshipMaterializedEdgeTypes()
	reason, ok := got["SATISFIED_BY"]
	if !ok {
		t.Fatal(`CrossplaneRelationshipMaterializedEdgeTypes() missing "SATISFIED_BY"`)
	}
	if reason == "" {
		t.Error("SATISFIED_BY reason is empty, want a real reason")
	}
}
