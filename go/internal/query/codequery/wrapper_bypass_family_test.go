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
