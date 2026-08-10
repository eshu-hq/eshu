// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// codeCallExpectedEdgeFile mirrors the committed assertion fixture's shape.
type codeCallExpectedEdgeFile struct {
	Odu   string `json:"odu"`
	Edges []struct {
		RelationshipType string `json:"relationship_type"`
		SourceEntityID   string `json:"source_entity_id"`
		TargetEntityID   string `json:"target_entity_id"`
	} `json:"edges"`
}

// codeCallEdgeKey renders one edge as a comparable triple.
func codeCallEdgeKey(relType, source, target string) string {
	// The extractor leaves relationship_type empty for a plain call; the writer's
	// default arm turns that into CALLS (edge_writer_code_call_labels.go). The
	// expected set names the graph type, so normalize here rather than teaching
	// the fixture the reducer's internal blank convention.
	if strings.TrimSpace(relType) == "" {
		relType = "CALLS"
	}
	return relType + "|" + source + "|" + target
}

// TestCodeCallFamilyCassetteDerivesTheExpectedEdgeSet is the offline vacuity
// guard for #5991: the production extractor, over the committed cassette,
// reproduces the hand-derived expected set EXACTLY.
//
// Exact in both directions. MISSING means the extractor stopped producing an
// edge the graph is supposed to carry; EXTRA means it started producing one
// nobody derived — the direction a "contains all expected" check misses, and the
// one that silently grows the graph.
//
// This is deliberately NOT called coverage. It proves the extractor, not the
// gate: the live edge write is a MATCH-MATCH-MERGE on endpoint uid, so a missing
// endpoint node makes the write a silent no-op that this test cannot see. The
// live ifa-determinism assertion is what closes that half, and the family stays
// waived until it runs.
func TestCodeCallFamilyCassetteDerivesTheExpectedEdgeSet(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)

	odu, err := loadCodeCallFamilyOdu(codeCallFamilyCassetteFullPath(repoRoot))
	if err != nil {
		t.Fatalf("loadCodeCallFamilyOdu: %v", err)
	}

	raw, err := os.ReadFile(codeCallFamilyExpectedEdgesPath(repoRoot))
	if err != nil {
		t.Fatalf("read expected edges: %v", err)
	}
	var expectedFile codeCallExpectedEdgeFile
	if err := json.Unmarshal(raw, &expectedFile); err != nil {
		t.Fatalf("parse expected edges: %v", err)
	}
	if len(expectedFile.Edges) == 0 {
		t.Fatal("expected-edge set is empty; an empty expectation makes this guard vacuous")
	}

	_, codeCallRows, _, metaclassRows := reducer.ExtractAllCodeRelationshipRows(odu.Facts)

	actual := map[string]int{}
	for _, row := range codeCallRows {
		actual[codeCallEdgeKey(
			anyToStringValue(row["relationship_type"]),
			anyToStringValue(row["caller_entity_id"]),
			anyToStringValue(row["callee_entity_id"]),
		)]++
	}
	for _, row := range metaclassRows {
		// Metaclass rows carry source/target rather than caller/callee.
		actual[codeCallEdgeKey(
			anyToStringValue(row["relationship_type"]),
			anyToStringValue(row["source_entity_id"]),
			anyToStringValue(row["target_entity_id"]),
		)]++
	}

	expected := map[string]int{}
	for _, e := range expectedFile.Edges {
		expected[codeCallEdgeKey(e.RelationshipType, e.SourceEntityID, e.TargetEntityID)]++
	}

	var missing, extra []string
	for key, want := range expected {
		if actual[key] < want {
			missing = append(missing, key)
		}
	}
	for key, got := range actual {
		if got > expected[key] {
			extra = append(extra, key)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)

	if len(missing) > 0 {
		t.Errorf("MISSING %d edge(s) the extractor no longer produces: %s", len(missing), strings.Join(missing, ", "))
	}
	if len(extra) > 0 {
		t.Errorf("EXTRA %d edge(s) nobody derived: %s", len(extra), strings.Join(extra, ", "))
	}
	if len(missing) == 0 && len(extra) == 0 {
		t.Logf("code_calls: %d rows reproduce the expected %d-edge set exactly", len(codeCallRows)+len(metaclassRows), len(expectedFile.Edges))
	}
}

// TestCodeCallFamilyCoversAllFourEdgeTypes stops the fixture degrading into a
// one-type proof.
//
// The family owns four types and the whole point of #5991 is exhaustiveness. An
// expected set that quietly lost INSTANTIATES or USES_METACLASS would still pass
// the exact-set test above — it would simply be exact about less.
func TestCodeCallFamilyCoversAllFourEdgeTypes(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)

	raw, err := os.ReadFile(codeCallFamilyExpectedEdgesPath(repoRoot))
	if err != nil {
		t.Fatalf("read expected edges: %v", err)
	}
	var expectedFile codeCallExpectedEdgeFile
	if err := json.Unmarshal(raw, &expectedFile); err != nil {
		t.Fatalf("parse expected edges: %v", err)
	}

	present := map[string]struct{}{}
	for _, e := range expectedFile.Edges {
		present[e.RelationshipType] = struct{}{}
	}
	registered, err := MaterializedEdgeDomainEdgeTypes("code_calls")
	if err != nil {
		t.Fatalf("MaterializedEdgeDomainEdgeTypes(code_calls): %v", err)
	}
	var uncovered []string
	for edgeType := range registered {
		if _, ok := present[edgeType]; !ok {
			uncovered = append(uncovered, edgeType)
		}
	}
	sort.Strings(uncovered)
	if len(uncovered) > 0 {
		t.Errorf("the expected-edge set exercises no %v edge; the family registers them, so the fixture proves exhaustiveness over less than the family owns", uncovered)
	}
}
