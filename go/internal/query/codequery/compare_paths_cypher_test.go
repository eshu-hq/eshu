// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestBuildComparePathsHopCypherParity pins the BFS hop read: anchored on
// the source entity id, repo/grant in the anchoring WHERE on both dialects,
// and identical outgoing-edge columns (id, name, repo, confidence).
func TestBuildComparePathsHopCypherParity(t *testing.T) {
	access := querycontract.RepositoryAccessFilter{AllScopes: true}
	nornic, _ := BuildComparePathsHopCypher(
		"fn-a", "repo-1", querycontract.GraphBackendNornicDB, access)
	neo, _ := BuildComparePathsHopCypher(
		"fn-a", "repo-1", querycontract.GraphBackendNeo4j, access)
	for _, cypher := range []string{nornic, neo} {
		for _, want := range []string{
			"CALLS", "edge_confidence", "repo_id",
		} {
			if !strings.Contains(cypher, want) {
				t.Errorf("compare hop cypher misses %q:\n%s", want, cypher)
			}
		}
	}
	if nornic == neo {
		t.Errorf("expected distinct dialect text, got identical queries")
	}
	if !strings.Contains(nornic, "{uid: $source_entity_id}") {
		t.Errorf("nornic compare hop anchor missing uid pattern:\n%s", nornic)
	}
}

// TestComparePathsBounds pins the shipped caps: depth and path counts
// clamp to the measured bound, never to caller arithmetic.
func TestComparePathsBounds(t *testing.T) {
	if got := normalizeCompareDepth(0); got != CompareDefaultDepth {
		t.Errorf("default depth = %d, want %d", got, CompareDefaultDepth)
	}
	if got := normalizeCompareDepth(100); got != CompareMaxDepth {
		t.Errorf("clamped depth = %d, want %d", got, CompareMaxDepth)
	}
	if got := normalizeCompareMaxPaths(0); got != CompareDefaultMaxPaths {
		t.Errorf("default max paths = %d, want %d", got, CompareDefaultMaxPaths)
	}
	if got := normalizeCompareMaxPaths(1000); got != CompareMaxPaths {
		t.Errorf("clamped max paths = %d, want %d", got, CompareMaxPaths)
	}
}
