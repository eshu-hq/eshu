// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

func TestNornicDBPhaseGroupExecutorChunksPositiveRetractFilePaths(t *testing.T) {
	t.Parallel()

	inner := &recordingGroupChunkExecutor{}
	executor := nornicDBPhaseGroupExecutor{
		inner:         inner,
		maxStatements: 5,
	}
	paths := make([]string, sourcecypher.DefaultPositiveRetractStringSliceBatchSize+1)
	for i := range paths {
		paths[i] = string(rune('a'+i%26)) + ".go"
	}

	stmts := []sourcecypher.Statement{
		{
			Operation: sourcecypher.OperationCanonicalRetract,
			Cypher: `MATCH (f:File)
WHERE f.path IN $file_paths
MATCH (:Directory)-[r:CONTAINS]->(f)
DELETE r`,
			Parameters: map[string]any{"file_paths": paths},
		},
	}

	if err := executor.ExecutePhaseGroup(context.Background(), stmts); err != nil {
		t.Fatalf("ExecutePhaseGroup() error = %v, want nil", err)
	}
	if got, want := len(inner.executeStatements), 2; got != want {
		t.Fatalf("execute statement count = %d, want %d", got, want)
	}
	assertIngesterStringSliceParamLen(t, inner.executeStatements[0], "file_paths", sourcecypher.DefaultPositiveRetractStringSliceBatchSize)
	assertIngesterStringSliceParamLen(t, inner.executeStatements[1], "file_paths", 1)
}

func TestNornicDBPhaseGroupExecutorDoesNotChunkNegativeRetractFilePaths(t *testing.T) {
	t.Parallel()

	inner := &recordingGroupChunkExecutor{}
	executor := nornicDBPhaseGroupExecutor{
		inner:         inner,
		maxStatements: 5,
	}
	paths := make([]string, sourcecypher.DefaultPositiveRetractStringSliceBatchSize+1)
	for i := range paths {
		paths[i] = string(rune('a'+i%26)) + ".go"
	}

	stmts := []sourcecypher.Statement{
		{
			Operation: sourcecypher.OperationCanonicalRetract,
			Cypher: `MATCH (f:File)
WHERE f.repo_id = $repo_id AND f.generation_id <> $generation_id
  AND (f.path IS NULL OR NOT (f.path IN $file_paths))
DETACH DELETE f`,
			Parameters: map[string]any{"file_paths": paths},
		},
	}

	if err := executor.ExecutePhaseGroup(context.Background(), stmts); err != nil {
		t.Fatalf("ExecutePhaseGroup() error = %v, want nil", err)
	}
	if got, want := len(inner.executeStatements), 1; got != want {
		t.Fatalf("execute statement count = %d, want %d", got, want)
	}
	assertIngesterStringSliceParamLen(t, inner.executeStatements[0], "file_paths", len(paths))
}

func assertIngesterStringSliceParamLen(t *testing.T, stmt sourcecypher.Statement, name string, want int) {
	t.Helper()

	got, ok := stmt.Parameters[name].([]string)
	if !ok {
		t.Fatalf("%s type = %T, want []string", name, stmt.Parameters[name])
	}
	if len(got) != want {
		t.Fatalf("len(%s) = %d, want %d", name, len(got), want)
	}
}
