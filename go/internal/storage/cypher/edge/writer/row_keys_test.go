// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package writer

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestEdgeWriterRepoDependencyRowsCarryEveryReferencedKey is the regression
// for the B-7 Neo4j run's get_repo_context divergence (#6782).
//
// The repo-dependency routes SET `rel.source_tool = row.source_tool` (and
// `rel.evidence_type = row.evidence_type`), but buildRowMap used to add those
// keys only when the intent payload carried a value. A row map without the key
// is read as null on Neo4j, which leaves the edge unstamped as the provenance
// contract requires. The pinned NornicDB v1.3.3 instead stores the literal
// expression text "row.source_tool" on the edge, so every package-consumption
// and code-import DEPENDS_ON edge reported a bogus source_tool there. Sending
// every referenced key, nil when absent, makes both backends store the same
// thing.
func TestEdgeWriterRepoDependencyRowsCarryEveryReferencedKey(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{
			// Package consumption: evidence_type but no source_tool.
			IntentID:     "package-consumption",
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"repo_id":           "repo-a",
				"target_repo_id":    "repo-b",
				"relationship_type": "DEPENDS_ON",
				"evidence_type":     "package_consumption",
			},
		},
		{
			// A typed verb with neither optional key.
			IntentID:     "typed-bare",
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"repo_id":           "repo-a",
				"target_repo_id":    "repo-c",
				"relationship_type": "DEPLOYS_FROM",
			},
		},
		{
			// RUNS_ON with neither optional key.
			IntentID:     "runs-on-bare",
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"repo_id":           "repo-a",
				"platform_id":       "platform-x",
				"relationship_type": "RUNS_ON",
			},
		},
	}
	if _, err := writer.WriteEdges(context.Background(), reducer.DomainRepoDependency, rows, "resolver/cross-repo"); err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}

	checked := assertUnwindRowsCarryReferencedKeys(t, executor.calls)
	if checked < len(rows) {
		t.Fatalf("checked %d routed rows, want at least %d", checked, len(rows))
	}
}

// TestEdgeWriterRepoDependencyAbsentSourceToolIsNil pins the value sent for an
// absent source_tool: nil, not "". The provenance contract leaves an edge with
// no evidence kind unstamped, and readers filter `source_tool IS NOT NULL`; an
// empty string would count as stamped on both backends.
func TestEdgeWriterRepoDependencyAbsentSourceToolIsNil(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)
	rows := []reducer.SharedProjectionIntentRow{{
		IntentID:     "package-consumption",
		RepositoryID: "repo-a",
		Payload: map[string]any{
			"repo_id":        "repo-a",
			"target_repo_id": "repo-b",
			"evidence_type":  "package_consumption",
		},
	}}
	if _, err := writer.WriteEdges(context.Background(), reducer.DomainRepoDependency, rows, "resolver/cross-repo"); err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	for _, call := range executor.calls {
		rowsOut, ok := call.Parameters["rows"].([]map[string]any)
		if !ok || len(rowsOut) == 0 {
			continue
		}
		value, present := rowsOut[0]["source_tool"]
		if !present || value != nil {
			t.Fatalf("source_tool = %#v (present=%v), want an explicit nil", value, present)
		}
		return
	}
	t.Fatal("no routed row was written")
}
