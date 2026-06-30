// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import "testing"

func TestChunkPositiveStringSliceRetractStatementSplitsPositiveInList(t *testing.T) {
	t.Parallel()

	stmt := Statement{
		Operation: OperationCanonicalRetract,
		Cypher: `MATCH (f:File)
WHERE f.path IN $file_paths
MATCH (:Directory)-[r:CONTAINS]->(f)
DELETE r`,
		Parameters: map[string]any{
			"file_paths":    []string{"a.go", "b.go", "c.go", "d.go", "e.go"},
			"generation_id": "gen-2",
		},
	}

	chunks := ChunkPositiveStringSliceRetractStatement(stmt, 2)
	if got, want := len(chunks), 3; got != want {
		t.Fatalf("len(chunks) = %d, want %d", got, want)
	}
	assertStringSliceParam(t, chunks[0], "file_paths", []string{"a.go", "b.go"})
	assertStringSliceParam(t, chunks[1], "file_paths", []string{"c.go", "d.go"})
	assertStringSliceParam(t, chunks[2], "file_paths", []string{"e.go"})
	for _, chunk := range chunks {
		if got, want := chunk.Parameters["generation_id"], "gen-2"; got != want {
			t.Fatalf("generation_id = %#v, want %#v", got, want)
		}
	}
}

func TestChunkPositiveStringSliceRetractStatementSplitsPositiveUnwindList(t *testing.T) {
	t.Parallel()

	stmt := Statement{
		Operation: OperationCanonicalRetract,
		Cypher: `UNWIND $file_paths AS file_path
MATCH (f:File {path: file_path})
WHERE f.repo_id = $repo_id AND f.evidence_source = 'projector/canonical'
DETACH DELETE f`,
		Parameters: map[string]any{
			"file_paths": []string{"a.go", "b.go", "c.go", "d.go", "e.go"},
			"repo_id":    "repo-1",
		},
	}

	chunks := ChunkPositiveStringSliceRetractStatement(stmt, 2)
	if got, want := len(chunks), 3; got != want {
		t.Fatalf("len(chunks) = %d, want %d", got, want)
	}
	assertStringSliceParam(t, chunks[0], "file_paths", []string{"a.go", "b.go"})
	assertStringSliceParam(t, chunks[1], "file_paths", []string{"c.go", "d.go"})
	assertStringSliceParam(t, chunks[2], "file_paths", []string{"e.go"})
	for _, chunk := range chunks {
		if got, want := chunk.Parameters["repo_id"], "repo-1"; got != want {
			t.Fatalf("repo_id = %#v, want %#v", got, want)
		}
	}
}

func TestChunkPositiveStringSliceRetractStatementDoesNotSplitNegativeInList(t *testing.T) {
	t.Parallel()

	stmt := Statement{
		Operation: OperationCanonicalRetract,
		Cypher: `MATCH (f:File)
WHERE f.repo_id = $repo_id AND f.generation_id <> $generation_id
  AND (f.path IS NULL OR NOT (f.path IN $file_paths))
DETACH DELETE f`,
		Parameters: map[string]any{
			"repo_id":       "repo-1",
			"generation_id": "gen-2",
			"file_paths":    []string{"a.go", "b.go", "c.go"},
		},
	}

	chunks := ChunkPositiveStringSliceRetractStatement(stmt, 1)
	if got, want := len(chunks), 1; got != want {
		t.Fatalf("len(chunks) = %d, want %d", got, want)
	}
	assertStringSliceParam(t, chunks[0], "file_paths", []string{"a.go", "b.go", "c.go"})
}

func TestChunkPositiveStringSliceRetractStatementLeavesNonRetractStatementIntact(t *testing.T) {
	t.Parallel()

	stmt := Statement{
		Operation: OperationCanonicalUpsert,
		Cypher:    `UNWIND $rows AS row RETURN row`,
		Parameters: map[string]any{
			"rows": []map[string]any{{"id": "one"}, {"id": "two"}},
		},
	}

	chunks := ChunkPositiveStringSliceRetractStatement(stmt, 1)
	if got, want := len(chunks), 1; got != want {
		t.Fatalf("len(chunks) = %d, want %d", got, want)
	}
	if got := chunks[0].Parameters["rows"]; got == nil {
		t.Fatal("rows parameter was dropped")
	}
}

func assertStringSliceParam(t *testing.T, stmt Statement, name string, want []string) {
	t.Helper()

	got, ok := stmt.Parameters[name].([]string)
	if !ok {
		t.Fatalf("%s type = %T, want []string", name, stmt.Parameters[name])
	}
	if len(got) != len(want) {
		t.Fatalf("%s = %v, want %v", name, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s = %v, want %v", name, got, want)
		}
	}
}
