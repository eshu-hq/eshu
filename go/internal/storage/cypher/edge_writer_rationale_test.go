// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestBuildRationaleRowMapRoutesExplainsEdge(t *testing.T) {
	payload := map[string]any{
		"rationale_uid":    "rationale:uid:func:HACK:abc",
		"target_entity_id": "uid:func",
		"repo_id":          "repo-1",
		"comment_kind":     "HACK",
		"excerpt_hash":     "abc",
	}
	cypher, rowMap, ok := buildRationaleRowMap(payload, "reducer/rationale")
	if !ok {
		t.Fatal("buildRationaleRowMap ok = false, want true")
	}
	if cypher != batchCanonicalRationaleExplainsEdgeCypher {
		t.Errorf("expected EXPLAINS template, got %q", cypher)
	}
	if !strings.Contains(cypher, "rel:EXPLAINS") || !strings.Contains(cypher, "MERGE (rationale:Rationale") {
		t.Errorf("template missing EXPLAINS edge / Rationale node: %q", cypher)
	}
	if rowMap["comment_kind"] != "HACK" || rowMap["repo_id"] != "repo-1" {
		t.Errorf("rowMap fields not carried: %#v", rowMap)
	}
}

func TestBuildRationaleRowMapRequiresRationaleAndTarget(t *testing.T) {
	if _, _, ok := buildRationaleRowMap(map[string]any{"target_entity_id": "uid:func"}, "src"); ok {
		t.Error("missing rationale_uid should be rejected")
	}
	if _, _, ok := buildRationaleRowMap(map[string]any{"rationale_uid": "r"}, "src"); ok {
		t.Error("missing target_entity_id should be rejected")
	}
}

func TestRetractRationaleEdgesIsRepoScoped(t *testing.T) {
	stmt := BuildRetractRationaleEdges([]string{"repo-1"}, "reducer/rationale")
	if !strings.Contains(stmt.Cypher, "rel:EXPLAINS") {
		t.Errorf("retract does not target EXPLAINS: %q", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "rationale.repo_id IN $repo_ids") {
		t.Errorf("retract is not repo-scoped: %q", stmt.Cypher)
	}
}

func TestBuildRetractRationaleEdgesByFilePath(t *testing.T) {
	t.Parallel()

	stmt := BuildRetractRationaleEdgesByFilePath([]string{"/repo/src/handler.go"}, "reducer/rationale")
	if !strings.Contains(stmt.Cypher, "rel:EXPLAINS") {
		t.Fatalf("cypher = %q, want EXPLAINS cleanup", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "target.path IN $file_paths") {
		t.Fatalf("cypher = %q, want target.path file-scope filter", stmt.Cypher)
	}
	if strings.Contains(stmt.Cypher, "rationale.repo_id IN $repo_ids") {
		t.Fatalf("cypher = %q, want no repo-wide rationale filter", stmt.Cypher)
	}
	if got, want := stmt.Parameters["evidence_source"], "reducer/rationale"; got != want {
		t.Fatalf("evidence_source = %#v, want %#v", got, want)
	}
	gotPaths, ok := stmt.Parameters["file_paths"].([]string)
	if !ok {
		t.Fatalf("file_paths parameter type = %T, want []string", stmt.Parameters["file_paths"])
	}
	wantPaths := []string{"/repo/src/handler.go"}
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatalf("file_paths = %#v, want %#v", gotPaths, wantPaths)
	}
}

func TestEdgeWriterRetractEdgesRationaleDeltaUsesFileScope(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "i1",
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"repo_id":          "repo-a",
				"delta_projection": true,
				"delta_file_paths": []string{"/repo/src/handler.go"},
			},
		},
	}

	err := writer.RetractEdges(context.Background(), reducer.DomainRationaleEdges, rows, "reducer/rationale")
	if err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	stmt := executor.calls[0]
	if strings.Contains(stmt.Cypher, "rationale.repo_id IN $repo_ids") {
		t.Fatalf("delta retract cypher = %q, want no repo-wide rationale filter", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "target.path IN $file_paths") {
		t.Fatalf("delta retract cypher = %q, want target.path file-scope filter", stmt.Cypher)
	}
	if _, ok := stmt.Parameters["repo_ids"]; ok {
		t.Fatalf("repo_ids unexpectedly present in delta retract parameters: %#v", stmt.Parameters)
	}
	filePaths, ok := stmt.Parameters["file_paths"].([]string)
	if !ok {
		t.Fatalf("file_paths parameter type = %T, want []string", stmt.Parameters["file_paths"])
	}
	if got, want := strings.Join(filePaths, ","), "/repo/src/handler.go"; got != want {
		t.Fatalf("file_paths = %q, want %q", got, want)
	}
}

func TestEdgeWriterRetractEdgesRationaleRejectsDeltaWithoutFilePaths(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "i1",
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"repo_id":          "repo-a",
				"delta_projection": true,
			},
		},
	}

	err := writer.RetractEdges(context.Background(), reducer.DomainRationaleEdges, rows, "reducer/rationale")
	if err == nil {
		t.Fatal("RetractEdges() error = nil, want malformed delta scope error")
	}
	if got := len(executor.calls); got != 0 {
		t.Fatalf("executor calls = %d, want 0 for malformed delta scope", got)
	}
}
