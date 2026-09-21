// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestBuildWrapperFamilyCallersCypherParity pins the batched one-hop
// callers read: UNWIND over target ids, anchored per target, repo/grant in
// the anchoring WHERE on both dialects, and identical caller columns plus
// the target_name demux column.
func TestBuildWrapperFamilyCallersCypherParity(t *testing.T) {
	access := querycontract.RepositoryAccessFilter{}
	nornic, nornicParams := BuildWrapperFamilyCallersCypher(
		[]string{"target-a", "target-b"}, "repo-1", querycontract.GraphBackendNornicDB, access)
	neo, neoParams := BuildWrapperFamilyCallersCypher(
		[]string{"target-a", "target-b"}, "repo-1", querycontract.GraphBackendNeo4j, access)
	for _, cypher := range []string{nornic, neo} {
		for _, want := range []string{
			"UNWIND $target_ids AS tid", "CALLS", "target_id",
			"edge_method", "edge_confidence", "complexity", "target_name",
		} {
			if !strings.Contains(cypher, want) {
				t.Errorf("family callers cypher misses %q:\n%s", want, cypher)
			}
		}
	}
	if _, ok := nornicParams["target_ids"]; !ok {
		t.Errorf("nornic params miss target_ids: %v", nornicParams)
	}
	if _, ok := neoParams["target_ids"]; !ok {
		t.Errorf("neo4j params miss target_ids: %v", neoParams)
	}
	if nornic == neo {
		t.Errorf("expected distinct dialect text, got identical queries")
	}
	// Anchored shape: the UNWIND id binds the indexed entity-id node
	// pattern, never a label scan with a late filter.
	if !strings.Contains(nornic, "{uid: tid}") {
		t.Errorf("nornic family callers anchor missing uid pattern:\n%s", nornic)
	}
}

// TestBuildWrapperFamilyFanInCypherParity pins the batched fan-in count:
// one (id, fan_in) row per candidate on both dialects.
func TestBuildWrapperFamilyFanInCypherParity(t *testing.T) {
	access := querycontract.RepositoryAccessFilter{}
	nornic, _ := BuildWrapperFamilyFanInCypher(
		[]string{"fn-a", "fn-b"}, "repo-1", querycontract.GraphBackendNornicDB, access)
	neo, _ := BuildWrapperFamilyFanInCypher(
		[]string{"fn-a", "fn-b"}, "repo-1", querycontract.GraphBackendNeo4j, access)
	if !strings.Contains(nornic, "{uid: eid}") {
		t.Errorf("nornic family fan-in anchor missing uid pattern:\n%s", nornic)
	}
	for _, cypher := range []string{nornic, neo} {
		for _, want := range []string{"UNWIND $entity_ids", "CALLS", "fan_in"} {
			if !strings.Contains(cypher, want) {
				t.Errorf("family fan-in cypher misses %q:\n%s", want, cypher)
			}
		}
	}
}

// TestScanWrapperCallerRowCoercesNumbers pins driver-number tolerance:
// int64 counts and float confidences both scan.
func TestScanWrapperCallerRowCoercesNumbers(t *testing.T) {
	row := scanWrapperCallerRow(map[string]any{
		"id": "fn-w", "name": "wrap", "file_path": "pkg/wrap/w.go",
		"edge_method": "declared", "edge_confidence": 0.9, "complexity": int64(2),
	})
	if row.Package != "pkg/wrap" {
		t.Errorf("Package = %q, want pkg/wrap", row.Package)
	}
	if row.Complexity != 2 {
		t.Errorf("Complexity = %d, want 2", row.Complexity)
	}
	if row.EdgeConfidence != 0.9 {
		t.Errorf("EdgeConfidence = %v, want 0.9", row.EdgeConfidence)
	}
}

// TestBuildWrapperFamilyCalleesCypherParity pins the batched delegation
// read: UNWIND over source ids with the (source_id, id) demux pair on both
// dialects.
func TestBuildWrapperFamilyCalleesCypherParity(t *testing.T) {
	access := querycontract.RepositoryAccessFilter{}
	nornic, nornicParams := BuildWrapperFamilyCalleesCypher(
		[]string{"fn-w1", "fn-w2"}, "repo-1", querycontract.GraphBackendNornicDB, access)
	neo, neoParams := BuildWrapperFamilyCalleesCypher(
		[]string{"fn-w1", "fn-w2"}, "repo-1", querycontract.GraphBackendNeo4j, access)
	for _, cypher := range []string{nornic, neo} {
		for _, want := range []string{"UNWIND $source_ids AS sid", "CALLS", "source_id"} {
			if !strings.Contains(cypher, want) {
				t.Errorf("family callees cypher misses %q:\n%s", want, cypher)
			}
		}
	}
	if _, ok := nornicParams["source_ids"]; !ok {
		t.Errorf("nornic params miss source_ids: %v", nornicParams)
	}
	if _, ok := neoParams["source_ids"]; !ok {
		t.Errorf("neo4j params miss source_ids: %v", neoParams)
	}
	if !strings.Contains(nornic, "{uid: sid}") {
		t.Errorf("nornic family callees anchor missing uid pattern:\n%s", nornic)
	}
}

// TestChunkWrapperEvidenceKeys pins the UNWIND batch bound: chunks hold at
// most wrapperEvidenceKeyBatchSize keys, order is preserved, and empty
// input yields no chunks so the caller skips the round trip.
func TestChunkWrapperEvidenceKeys(t *testing.T) {
	if got := chunkWrapperEvidenceKeys(nil); len(got) != 0 {
		t.Fatalf("chunks(nil) = %d, want 0", len(got))
	}
	ids := make([]string, 0, 2*wrapperEvidenceKeyBatchSize+1)
	for i := 0; i < 2*wrapperEvidenceKeyBatchSize+1; i++ {
		ids = append(ids, string(rune('a'+i%26))+string(rune('0'+i/26)))
	}
	chunks := chunkWrapperEvidenceKeys(ids)
	if len(chunks) != 3 {
		t.Fatalf("chunks(%d ids) = %d chunks, want 3", len(ids), len(chunks))
	}
	if len(chunks[0]) != wrapperEvidenceKeyBatchSize || len(chunks[1]) != wrapperEvidenceKeyBatchSize || len(chunks[2]) != 1 {
		t.Fatalf("chunk sizes = %d/%d/%d, want %d/%d/1",
			len(chunks[0]), len(chunks[1]), len(chunks[2]), wrapperEvidenceKeyBatchSize, wrapperEvidenceKeyBatchSize)
	}
	flat := []string{}
	for _, chunk := range chunks {
		flat = append(flat, chunk...)
	}
	for i := range ids {
		if flat[i] != ids[i] {
			t.Fatalf("round trip order breaks at %d: %q != %q", i, flat[i], ids[i])
		}
	}
}
