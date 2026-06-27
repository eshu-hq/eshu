// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestBuildCanonicalRepoDependencyUpsertStampsSourceTool proves the DEPENDS_ON
// builder threads source_tool into both the Cypher SET and the parameters
// (#3997/#3999).
func TestBuildCanonicalRepoDependencyUpsertStampsSourceTool(t *testing.T) {
	t.Parallel()

	stmt := BuildCanonicalRepoDependencyUpsert(CanonicalRepoDependencyParams{
		RepoID:       "repo-a",
		TargetRepoID: "repo-b",
		SourceTool:   "ansible",
	}, "resolver/cross-repo")

	if !strings.Contains(stmt.Cypher, "rel.source_tool = $source_tool") {
		t.Fatalf("Cypher missing source_tool write: %s", stmt.Cypher)
	}
	if stmt.Parameters["source_tool"] != "ansible" {
		t.Fatalf("source_tool param = %v, want ansible", stmt.Parameters["source_tool"])
	}
}

// TestBuildCanonicalRepoRelationshipUpsertStampsSourceTool proves every typed
// verb (single-upsert builder) threads source_tool.
func TestBuildCanonicalRepoRelationshipUpsertStampsSourceTool(t *testing.T) {
	t.Parallel()

	for _, relType := range []string{
		"DEPLOYS_FROM", "DISCOVERS_CONFIG_IN", "PROVISIONS_DEPENDENCY_FOR", "USES_MODULE", "READS_CONFIG_FROM",
	} {
		stmt := BuildCanonicalRepoRelationshipUpsert(CanonicalRepoRelationshipParams{
			RepoID:           "repo-a",
			TargetRepoID:     "repo-b",
			RelationshipType: relType,
			SourceTool:       "kustomize",
		}, "resolver/cross-repo")
		if !strings.Contains(stmt.Cypher, "rel.source_tool = $source_tool") {
			t.Fatalf("%s Cypher missing source_tool write: %s", relType, stmt.Cypher)
		}
		if stmt.Parameters["source_tool"] != "kustomize" {
			t.Fatalf("%s source_tool param = %v, want kustomize", relType, stmt.Parameters["source_tool"])
		}
	}
}

// TestEdgeWriterThreadsSourceToolIntoRows proves the batched write path
// (buildRowMap) carries source_tool from the projection-intent payload into each
// UNWIND row, for the DEPENDS_ON, typed, and RUNS_ON branches.
func TestEdgeWriterThreadsSourceToolIntoRows(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "dep",
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"repo_id":        "repo-a",
				"target_repo_id": "repo-b",
				"evidence_type":  "ansible_role_reference",
				"source_tool":    "ansible",
			},
		},
		{
			IntentID:     "typed",
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"repo_id":           "repo-a",
				"target_repo_id":    "repo-c",
				"relationship_type": "DEPLOYS_FROM",
				"evidence_type":     "kustomize_resource_reference",
				"source_tool":       "kustomize",
			},
		},
		{
			IntentID:     "runson",
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"repo_id":           "repo-a",
				"platform_id":       "platform-x",
				"relationship_type": "RUNS_ON",
				"source_tool":       "argocd",
			},
		},
	}

	if err := writer.WriteEdges(context.Background(), reducer.DomainRepoDependency, rows, "resolver/cross-repo"); err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}

	wantTool := map[string]string{
		"MERGE (source_repo)-[rel:DEPENDS_ON]->(target_repo)":   "ansible",
		"MERGE (source_repo)-[rel:DEPLOYS_FROM]->(target_repo)": "kustomize",
		"MERGE (i)-[rel:RUNS_ON]->(p)":                          "argocd",
	}
	matched := 0
	for _, call := range executor.calls {
		rowsOut, ok := call.Parameters["rows"].([]map[string]any)
		if !ok || len(rowsOut) == 0 {
			continue
		}
		for fragment, tool := range wantTool {
			if strings.Contains(call.Cypher, fragment) {
				if !strings.Contains(call.Cypher, "rel.source_tool = row.source_tool") {
					t.Fatalf("%s Cypher missing source_tool SET: %s", fragment, call.Cypher)
				}
				if got := rowsOut[0]["source_tool"]; got != tool {
					t.Fatalf("%s row source_tool = %#v, want %q", fragment, got, tool)
				}
				matched++
			}
		}
	}
	if matched != len(wantTool) {
		t.Fatalf("matched %d source_tool-bearing edges, want %d", matched, len(wantTool))
	}
}
