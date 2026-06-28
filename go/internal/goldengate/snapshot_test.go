// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package goldengate

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadSnapshotReadsContract proves the loader parses a minimal well-formed
// snapshot and exposes the tolerance/correlation fields the Evaluate* functions
// consume. It uses a temp file so the test is self-contained (the gate's own
// tests cover the committed B-12 golden file relative to the command).
func TestLoadSnapshotReadsContract(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snap.json")
	const body = `{
      "schema_version": "1",
      "corpus_id": "unit",
      "graph": {
        "node_counts": {"Repository": {"min": 1, "max": 1}},
        "edge_counts": {"CONTAINS": {"min": 1, "max": 9}},
        "required_correlations": [
          {"id": "rc-x", "relationship": "CONTAINS", "from_label": "Repository", "to_label": "Directory", "minimum_count": 1}
        ],
        "required_nodes": [
          {"id": "rn-dir", "label": "Directory", "minimum_count": 1}
        ]
      },
      "drain_assertions": {
        "fact_work_items": {"residual_max": 0},
        "shared_projection_intents": {"nonterminal_max": 0}
      }
    }`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}

	snap, err := LoadSnapshot(path)
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	if snap.SchemaVersion != "1" {
		t.Errorf("schema_version = %q, want 1", snap.SchemaVersion)
	}
	if got := snap.Graph.NodeCounts["Repository"]; !got.Contains(1) {
		t.Errorf("Repository range %+v does not contain 1", got)
	}
	if len(snap.Graph.RequiredCorrelations) != 1 || snap.Graph.RequiredCorrelations[0].ID != "rc-x" {
		t.Errorf("required_correlations = %+v, want one rc-x", snap.Graph.RequiredCorrelations)
	}
	if len(snap.Graph.RequiredNodes) != 1 || snap.Graph.RequiredNodes[0].Label != "Directory" {
		t.Errorf("required_nodes = %+v, want one Directory", snap.Graph.RequiredNodes)
	}
	if snap.DrainAssertions.FactWorkItems.Limit() != 0 {
		t.Errorf("fact_work_items limit = %d, want 0", snap.DrainAssertions.FactWorkItems.Limit())
	}
}

// TestLoadSnapshotRejectsMissingSchemaVersion proves a snapshot without a
// schema_version is a loud error, not a silently-empty contract that would make
// every Evaluate* call pass vacuously.
func TestLoadSnapshotRejectsMissingSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte(`{"corpus_id": "x"}`), 0o600); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}
	if _, err := LoadSnapshot(path); err == nil {
		t.Fatal("LoadSnapshot accepted a snapshot with no schema_version")
	}
}
