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

// TestBuildRetractRationaleEdgeStatementsByFilePath guards the rationale
// sibling of the #5116 fix: the delta EXPLAINS retract must emit one statement
// per target label with a single-label target anchor. On NornicDB v1.1.11 a
// bare MATCH whose target carries a node-label disjunction matches zero rows
// (probed), so a single combined statement silently retracted nothing. The
// live proof is TestReducerRationaleEdgeRetractGraphTruth in
// internal/replay/offlinetier.
func TestBuildRetractRationaleEdgeStatementsByFilePath(t *testing.T) {
	t.Parallel()

	stmts := BuildRetractRationaleEdgeStatementsByFilePath([]string{"/repo/src/handler.go"}, "reducer/rationale")
	if got, want := len(stmts), len(rationaleExplainsTargetLabels); got != want {
		t.Fatalf("statement count = %d, want %d (one per target label)", got, want)
	}
	for _, label := range rationaleExplainsTargetLabels {
		want := "MATCH (rationale:Rationale)-[rel:EXPLAINS]->(target:" + label + ")"
		found := false
		for _, stmt := range stmts {
			if strings.Contains(stmt.Cypher, want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no per-label retract statement for target label %q (want %q)", label, want)
		}
	}
	for _, stmt := range stmts {
		if strings.Contains(stmt.Cypher, "->(target:Function|") || strings.Contains(stmt.Cypher, "->(target)") {
			t.Fatalf("target disjunction or unlabeled target reintroduced (#5116 sibling): %q", stmt.Cypher)
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
}

// TestRationaleRetractCoversEveryWriteTargetLabel links the delta retract's
// per-label fan-out to the write template's target disjunction. Both are built
// from rationaleExplainsTargetLabels, so this asserts the write template
// actually contains every label the retract covers — a regression that
// hardcodes one side and drops a label from the other fails here.
func TestRationaleRetractCoversEveryWriteTargetLabel(t *testing.T) {
	t.Parallel()

	wantDisjunction := "MATCH (target:" + strings.Join(rationaleExplainsTargetLabels, "|") + " {uid: row.target_entity_id})"
	if !strings.Contains(batchCanonicalRationaleExplainsEdgeCypher, wantDisjunction) {
		t.Fatalf("write template target disjunction diverged from rationaleExplainsTargetLabels:\nwant %q\nin %q",
			wantDisjunction, batchCanonicalRationaleExplainsEdgeCypher)
	}
	stmts := BuildRetractRationaleEdgeStatementsByFilePath([]string{"p"}, "reducer/rationale")
	if got, want := len(stmts), len(rationaleExplainsTargetLabels); got != want {
		t.Fatalf("retract statements = %d, want %d (one per write-capable target label)", got, want)
	}
}

func TestEdgeWriterRetractEdgesRationaleDeltaRunsPerLabelStatementsSequentially(t *testing.T) {
	t.Parallel()

	executor := &sqlSequentialRecordingExecutor{}
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
	// The #5116 managed-transaction under-apply also forbids grouping: the
	// per-label statements must run as separate transactions even when the
	// executor supports grouping.
	if got := len(executor.groupCalls); got != 0 {
		t.Fatalf("ExecuteGroup calls = %d, want 0 (grouped DELETEs under-apply on NornicDB v1.1.11)", got)
	}
	if got, want := len(executor.calls), len(rationaleExplainsTargetLabels); got != want {
		t.Fatalf("Execute calls = %d, want %d (one per target label)", got, want)
	}
	for _, stmt := range executor.calls {
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
