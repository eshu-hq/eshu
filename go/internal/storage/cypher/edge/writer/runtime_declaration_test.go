// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package writer

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

func runtimeDeclarationRow(repoID, relativePath string, line, order int) map[string]any {
	return map[string]any{
		"repo_id":       repoID,
		"relative_path": relativePath,
		"line_number":   line,
		"runtime":       "node",
		"order_index":   order,
		"scope_dir":     ".",
		"source_kind":   "nvmrc",
		"raw_spec":      "20.11.1",
		"spec_kind":     "exact",
		"version_major": 20,
	}
}

func TestRuntimeDeclarationWriterEmptyRowsIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	w := NewRuntimeDeclarationWriter(executor, 0)
	if err := w.WriteRuntimeDeclarations(context.Background(), nil, "gen-1", "reducer/runtime-declaration"); err != nil {
		t.Fatalf("WriteRuntimeDeclarations(nil) error = %v", err)
	}
	if err := w.RetractRuntimeDeclarations(context.Background(), nil, "reducer/runtime-declaration"); err != nil {
		t.Fatalf("RetractRuntimeDeclarations(nil) error = %v", err)
	}
	if err := w.RetractRuntimeDeclarationsByPath(context.Background(), map[string][]string{"r": nil}, "reducer/runtime-declaration"); err != nil {
		t.Fatalf("RetractRuntimeDeclarationsByPath(empty paths) error = %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0", len(executor.calls))
	}
}

// TestRuntimeDeclarationUpsertCypherMergesOnTheFivePartKey pins the MERGE
// identity to the graph key the #8815 arbiter ruling fixed. A MERGE on a wider
// map (raw_spec, spec_kind, version parts) would mint a second node when a
// file's pinned version changes in place on the same line.
func TestRuntimeDeclarationUpsertCypherMergesOnTheFivePartKey(t *testing.T) {
	t.Parallel()

	want := "MERGE (d:RuntimeDeclaration {repo_id: row.repo_id, relative_path: row.relative_path, " +
		"line_number: row.line_number, runtime: row.runtime, order_index: row.order_index})"
	if !strings.Contains(RuntimeDeclarationUpsertCypher, want) {
		t.Fatalf("upsert Cypher missing five-part MERGE identity %q:\n%s", want, RuntimeDeclarationUpsertCypher)
	}
	for _, fragment := range []string{
		"MERGE (repo:Repository {id: row.repo_id})",
		"MERGE (repo)-[rel:DECLARES_RUNTIME]->(d)",
		"d.version_major = row.version_major",
		"d.range_upper_major = row.range_upper_major",
		"d.evidence_source = row.evidence_source",
		"rel.evidence_source = row.evidence_source",
	} {
		if !strings.Contains(RuntimeDeclarationUpsertCypher, fragment) {
			t.Errorf("upsert Cypher missing %q", fragment)
		}
	}
}

func TestRuntimeDeclarationWriterStampsGenerationAndEvidenceSource(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	w := NewRuntimeDeclarationWriter(executor, 0)
	row := runtimeDeclarationRow("repository:r1", ".nvmrc", 1, 0)
	if err := w.WriteRuntimeDeclarations(context.Background(), []map[string]any{row}, "gen-1", "reducer/runtime-declaration"); err != nil {
		t.Fatalf("WriteRuntimeDeclarations error = %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	call := executor.calls[0]
	if call.Cypher != RuntimeDeclarationUpsertCypher {
		t.Fatalf("Cypher = %q, want the upsert template", call.Cypher)
	}
	rows, ok := call.Parameters["rows"].([]map[string]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("rows param = %#v, want one row", call.Parameters["rows"])
	}
	if got := rows[0]["generation_id"]; got != "gen-1" {
		t.Errorf("generation_id = %v, want gen-1", got)
	}
	if got := rows[0]["evidence_source"]; got != "reducer/runtime-declaration" {
		t.Errorf("evidence_source = %v, want reducer/runtime-declaration", got)
	}
	if _, mutated := row["generation_id"]; mutated {
		t.Errorf("caller row was mutated: %#v", row)
	}
	if got := call.Parameters[sourcecypher.StatementMetadataEntityLabelKey]; got != "RuntimeDeclaration" {
		t.Errorf("entity label metadata = %v, want RuntimeDeclaration", got)
	}
}

func TestRuntimeDeclarationWriterBatchesRows(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	w := NewRuntimeDeclarationWriter(executor, 2)
	rows := []map[string]any{
		runtimeDeclarationRow("repository:r1", ".nvmrc", 1, 0),
		runtimeDeclarationRow("repository:r1", ".tool-versions", 1, 0),
		runtimeDeclarationRow("repository:r1", ".tool-versions", 1, 1),
	}
	if err := w.WriteRuntimeDeclarations(context.Background(), rows, "gen-1", "src"); err != nil {
		t.Fatalf("WriteRuntimeDeclarations error = %v", err)
	}
	if len(executor.calls) != 2 {
		t.Fatalf("len(calls) = %d, want 2 batches of at most 2 rows", len(executor.calls))
	}
}

// TestRuntimeDeclarationRetractIsRepoAnchoredNodeDetach proves the
// whole-repository retract seeks the owning Repository by id (never scans the
// RuntimeDeclaration label) and DETACH DELETEs only the nodes this evidence
// source owns, so the DECLARES_RUNTIME edges go with them.
func TestRuntimeDeclarationRetractIsRepoAnchoredNodeDetach(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	w := NewRuntimeDeclarationWriter(executor, 0)
	if err := w.RetractRuntimeDeclarations(context.Background(), []string{"repository:r1", "repository:r2"}, "src"); err != nil {
		t.Fatalf("RetractRuntimeDeclarations error = %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	call := executor.calls[0]
	if call.Cypher != RetractRuntimeDeclarationsCypher {
		t.Fatalf("Cypher = %q, want the whole-repository retract", call.Cypher)
	}
	if call.Operation != sourcecypher.OperationCanonicalRetract {
		t.Errorf("Operation = %q, want %q", call.Operation, sourcecypher.OperationCanonicalRetract)
	}
	for _, fragment := range []string{
		"MATCH (repo:Repository)-[:DECLARES_RUNTIME]->(d:RuntimeDeclaration)",
		"repo.id IN $repo_ids",
		"d.evidence_source = $evidence_source",
		"DETACH DELETE d",
	} {
		if !strings.Contains(call.Cypher, fragment) {
			t.Errorf("retract Cypher missing %q", fragment)
		}
	}
	if got := call.Parameters["repo_ids"]; !reflect.DeepEqual(got, []string{"repository:r1", "repository:r2"}) {
		t.Errorf("repo_ids = %#v", got)
	}
}

// TestRuntimeDeclarationPathRetractPairsEachRepoWithItsOwnPaths proves the
// delta retract never crosses repositories: relative paths such as ".nvmrc"
// are identical across every repository, so each path list is bound to its
// own repository row (the #5419 P1 lesson), not flattened into one IN list.
func TestRuntimeDeclarationPathRetractPairsEachRepoWithItsOwnPaths(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	w := NewRuntimeDeclarationWriter(executor, 0)
	err := w.RetractRuntimeDeclarationsByPath(context.Background(), map[string][]string{
		"repository:r2": {"svc/.nvmrc"},
		"repository:r1": {".nvmrc", "README.md"},
		"repository:r3": nil,
	}, "src")
	if err != nil {
		t.Fatalf("RetractRuntimeDeclarationsByPath error = %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	call := executor.calls[0]
	if call.Cypher != RetractRuntimeDeclarationsByPathCypher {
		t.Fatalf("Cypher = %q, want the path-scoped retract", call.Cypher)
	}
	for _, fragment := range []string{
		"UNWIND $scopes AS row",
		"MATCH (repo:Repository {id: row.repo_id})-[:DECLARES_RUNTIME]->(d:RuntimeDeclaration)",
		"d.relative_path IN row.relative_paths",
		"d.evidence_source = $evidence_source",
		"DETACH DELETE d",
	} {
		if !strings.Contains(call.Cypher, fragment) {
			t.Errorf("path retract Cypher missing %q", fragment)
		}
	}
	want := []map[string]any{
		{"repo_id": "repository:r1", "relative_paths": []string{".nvmrc", "README.md"}},
		{"repo_id": "repository:r2", "relative_paths": []string{"svc/.nvmrc"}},
	}
	if got := call.Parameters["scopes"]; !reflect.DeepEqual(got, want) {
		t.Errorf("scopes = %#v, want %#v (sorted, empty path lists dropped)", got, want)
	}
}

func TestRuntimeDeclarationWriterRequiresExecutor(t *testing.T) {
	t.Parallel()

	w := NewRuntimeDeclarationWriter(nil, 0)
	row := runtimeDeclarationRow("repository:r1", ".nvmrc", 1, 0)
	if err := w.WriteRuntimeDeclarations(context.Background(), []map[string]any{row}, "g", "s"); err == nil {
		t.Fatal("WriteRuntimeDeclarations with nil executor = nil error, want error")
	}
	if err := w.RetractRuntimeDeclarations(context.Background(), []string{"r"}, "s"); err == nil {
		t.Fatal("RetractRuntimeDeclarations with nil executor = nil error, want error")
	}
}

func TestRuntimeDeclarationWriterPropagatesExecutorError(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	executor := &recordingExecutor{errAtCall: boom}
	w := NewRuntimeDeclarationWriter(executor, 0)
	row := runtimeDeclarationRow("repository:r1", ".nvmrc", 1, 0)
	if err := w.WriteRuntimeDeclarations(context.Background(), []map[string]any{row}, "g", "s"); !errors.Is(err, boom) {
		t.Fatalf("WriteRuntimeDeclarations error = %v, want wrapping %v", err, boom)
	}
	if err := w.RetractRuntimeDeclarations(context.Background(), []string{"r"}, "s"); !errors.Is(err, boom) {
		t.Fatalf("RetractRuntimeDeclarations error = %v, want wrapping %v", err, boom)
	}
}
