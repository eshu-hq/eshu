// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossplane

import (
	"strings"
	"testing"
)

func testRows(n int) []map[string]any {
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

func TestBuildUpsertPartsEmptyRowsIsNil(t *testing.T) {
	t.Parallel()

	if parts := BuildUpsertParts(nil, "scope-1", "gen-1", EvidenceSource, 0); parts != nil {
		t.Fatalf("BuildUpsertParts(nil) = %d parts, want nil", len(parts))
	}
}

func TestBuildUpsertPartsUsesStaticRelTypeMatchMatchMerge(t *testing.T) {
	t.Parallel()

	parts := BuildUpsertParts(testRows(1), "scope-1", "gen-1", EvidenceSource, 0)
	if len(parts) != 1 {
		t.Fatalf("len(parts) = %d, want 1", len(parts))
	}
	cypher := parts[0].Cypher
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
	if parts[0].IsRetract {
		t.Fatal("upsert part must not set IsRetract")
	}
}

func TestBuildUpsertPartsBatchesRows(t *testing.T) {
	t.Parallel()

	parts := BuildUpsertParts(testRows(5), "scope-1", "gen-1", EvidenceSource, 2)
	if len(parts) != 3 {
		t.Fatalf("len(parts) = %d, want 3 batched parts", len(parts))
	}
	wantSizes := []int{2, 2, 1}
	for i, part := range parts {
		rows, ok := part.Parameters["rows"].([]map[string]any)
		if !ok {
			t.Fatalf("part[%d] rows parameter type = %T, want []map[string]any", i, part.Parameters["rows"])
		}
		if len(rows) != wantSizes[i] {
			t.Fatalf("part[%d] rows = %d, want %d", i, len(rows), wantSizes[i])
		}
	}
}

func TestBuildUpsertPartsAnnotatesScopeGenerationEvidence(t *testing.T) {
	t.Parallel()

	parts := BuildUpsertParts(testRows(1), "scope-1", "gen-1", EvidenceSource, 0)
	rows, ok := parts[0].Parameters["rows"].([]map[string]any)
	if !ok {
		t.Fatalf("rows parameter type = %T, want []map[string]any", parts[0].Parameters["rows"])
	}
	if got := rows[0]["scope_id"]; got != "scope-1" {
		t.Fatalf("scope_id = %v, want scope-1 (injected for scope-scoped retract)", got)
	}
	if got := rows[0]["generation_id"]; got != "gen-1" {
		t.Fatalf("generation_id = %v, want gen-1", got)
	}
	if got := rows[0]["evidence_source"]; got != EvidenceSource {
		t.Fatalf("evidence_source = %v, want %v", got, EvidenceSource)
	}
	if got := parts[0].Parameters[phaseKey]; got != Phase {
		t.Fatalf("phase metadata = %v, want %v", got, Phase)
	}
	if got := parts[0].Parameters[entityLabelKey]; got != EdgeLabel {
		t.Fatalf("entity label metadata = %v, want %v", got, EdgeLabel)
	}
	cypher := parts[0].Cypher
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

func TestBuildUpsertPartsDoesNotMutateInput(t *testing.T) {
	t.Parallel()

	rows := testRows(2)
	BuildUpsertParts(rows, "scope-1", "gen-1", EvidenceSource, 0)
	for i, row := range rows {
		for _, key := range []string{"scope_id", "generation_id", "evidence_source"} {
			if _, ok := row[key]; ok {
				t.Fatalf("input row[%d] gained key %q; the builder must clone", i, key)
			}
		}
	}
}

func TestBuildRetractPartScopesByEvidenceSource(t *testing.T) {
	t.Parallel()

	part := BuildRetractPart([]string{"scope-1"}, "gen-1", EvidenceSource)
	if !part.IsRetract {
		t.Fatal("retract part must set IsRetract")
	}
	cypher := part.Cypher
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
	if got := part.Parameters["scope_ids"]; !equalStrings(got, []string{"scope-1"}) {
		t.Fatalf("scope_ids = %#v, want [scope-1]", part.Parameters["scope_ids"])
	}
	if got := part.Parameters["evidence_source"]; got != EvidenceSource {
		t.Fatalf("evidence_source = %v, want %v", got, EvidenceSource)
	}
}

func equalStrings(got any, want []string) bool {
	gotSlice, ok := got.([]string)
	if !ok || len(gotSlice) != len(want) {
		return false
	}
	for i := range want {
		if gotSlice[i] != want[i] {
			return false
		}
	}
	return true
}

func TestMaterializedEdgeTypesListsSatisfiedBy(t *testing.T) {
	t.Parallel()

	got := MaterializedEdgeTypes()
	reason, ok := got["SATISFIED_BY"]
	if !ok {
		t.Fatal(`MaterializedEdgeTypes() missing "SATISFIED_BY"`)
	}
	if reason == "" {
		t.Error("SATISFIED_BY reason is empty, want a real reason")
	}
}

func TestMaterializedEdgeTypesReturnsDefensiveCopy(t *testing.T) {
	t.Parallel()

	got := MaterializedEdgeTypes()
	got["SATISFIED_BY"] = "mutated"
	if again := MaterializedEdgeTypes(); again["SATISFIED_BY"] == "mutated" {
		t.Fatal("MaterializedEdgeTypes returned shared state; callers must not mutate package state")
	}
}
