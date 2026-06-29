// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"path/filepath"
	"testing"
)

// goldenSnapshotPath returns the repo-relative path to the committed B-12
// snapshot from this package's working directory (go/cmd/golden-corpus-gate).
func goldenSnapshotPath() string {
	return filepath.Join("..", "..", "..", "testdata", "golden", "e2e-20repo-snapshot.json")
}

func TestLoadSnapshotParsesGoldenContract(t *testing.T) {
	snap, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}

	if snap.SchemaVersion != "1" {
		t.Errorf("schema_version = %q, want %q", snap.SchemaVersion, "1")
	}
	if snap.CorpusID != "supply-chain-demo-20repo" {
		t.Errorf("corpus_id = %q, want supply-chain-demo-20repo", snap.CorpusID)
	}

	// Drain bounds must parse from the distinct JSON keys.
	if got := snap.DrainAssertions.FactWorkItems.Limit(); got != 0 {
		t.Errorf("fact_work_items residual limit = %d, want 0", got)
	}
	if got := snap.DrainAssertions.SharedProjectionIntents.Limit(); got != 0 {
		t.Errorf("shared_projection_intents nonterminal limit = %d, want 0", got)
	}

	// The minimal gate depends on rc-1 (deployable-unit) and rc-3 (DEPENDS_ON).
	wantRC := map[string]RequiredCorrelation{
		"rc-1": {Relationship: "CORRELATES_DEPLOYABLE_UNIT", FromLabel: "Repository", ToLabel: "Repository"},
		"rc-3": {Relationship: "DEPENDS_ON", FromLabel: "Repository", ToLabel: "Repository"},
	}
	got := map[string]RequiredCorrelation{}
	for _, rc := range snap.Graph.RequiredCorrelations {
		got[rc.ID] = rc
	}
	for id, want := range wantRC {
		rc, ok := got[id]
		if !ok {
			t.Fatalf("required_correlations missing %s", id)
		}
		if rc.Relationship != want.Relationship || rc.FromLabel != want.FromLabel || rc.ToLabel != want.ToLabel {
			t.Errorf("%s = %+v, want relationship=%s %s->%s", id, rc, want.Relationship, want.FromLabel, want.ToLabel)
		}
		if rc.MinimumCount < 1 {
			t.Errorf("%s minimum_count = %d, want >= 1", id, rc.MinimumCount)
		}
	}

	// A representative node range and edge range must parse.
	if r, ok := snap.Graph.NodeCounts["Repository"]; !ok || r.Min < 1 {
		t.Errorf("node_counts[Repository] = %+v, ok=%v", r, ok)
	}
	if r, ok := snap.Graph.EdgeCounts["DEPENDS_ON"]; !ok || r.Min < 1 {
		t.Errorf("edge_counts[DEPENDS_ON] = %+v, ok=%v", r, ok)
	}

	// Query shapes for the canonical surfaces must parse.
	if _, ok := snap.QueryShapes.MCP["list_indexed_repositories"]; !ok {
		t.Error("query_shapes.mcp missing list_indexed_repositories")
	}
	httpRepos, ok := snap.QueryShapes.HTTP["GET /api/v0/repositories"]
	if !ok {
		t.Fatal("query_shapes.http missing GET /api/v0/repositories")
	}
	if len(httpRepos.RequiredResponseFields) == 0 {
		t.Error("GET /api/v0/repositories has no required_response_fields")
	}
}

func TestCountRangeContains(t *testing.T) {
	r := CountRange{Min: 1, Max: 20}
	cases := []struct {
		n    int64
		want bool
	}{{0, false}, {1, true}, {10, true}, {20, true}, {21, false}}
	for _, c := range cases {
		if got := r.Contains(c.n); got != c.want {
			t.Errorf("Contains(%d) = %v, want %v", c.n, got, c.want)
		}
	}
}

func TestEvidenceNarrowedCorrelationsRequireSourceTool(t *testing.T) {
	snap, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}

	for _, rc := range snap.Graph.RequiredCorrelations {
		if len(rc.EvidenceKinds) == 0 {
			continue
		}
		if !containsString(rc.RequiredEdgeProperties, "source_tool") {
			t.Errorf("%s evidence-filtered correlation must require source_tool", rc.ID)
			continue
		}
		if len(rc.AllowedEdgePropertyValues["source_tool"]) == 0 {
			t.Errorf("%s source_tool assertion must pin allowed values", rc.ID)
		}
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestLoadSnapshotMissingFile(t *testing.T) {
	if _, err := LoadSnapshot(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Fatal("expected error for missing snapshot file")
	}
}
