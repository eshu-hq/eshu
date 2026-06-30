// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// drainCountReader is a fake drainReader that returns a configurable sequence
// of __drained counts. After the sequence is exhausted it returns 0.
type drainCountReader struct {
	counts  []int64
	callIdx int
	lastErr error
	failAt  int // 1-based; 0 means never fail
}

func (r *drainCountReader) RunWrite(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
	r.callIdx++
	if r.failAt > 0 && r.callIdx == r.failAt {
		return nil, r.lastErr
	}
	if r.callIdx > len(r.counts) {
		return []map[string]any{{"__drained": int64(0)}}, nil
	}
	return []map[string]any{{"__drained": r.counts[r.callIdx-1]}}, nil
}

// TestNornicDBPhaseGroupExecutorDrainLoopIteratesUntilZero verifies that a
// statement with Drain=true drives the drain loop until __drained == 0.
func TestNornicDBPhaseGroupExecutorDrainLoopIteratesUntilZero(t *testing.T) {
	t.Parallel()

	reader := &drainCountReader{counts: []int64{2000, 2000, 500}}
	inner := &recordingGroupChunkExecutor{}
	executor := nornicDBPhaseGroupExecutor{
		inner:            inner,
		maxStatements:    100,
		retractBatchSize: 2000,
		drainReader:      reader,
	}

	stmts := []sourcecypher.Statement{
		{
			Operation: sourcecypher.OperationCanonicalRetract,
			Cypher: `MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)
WHERE f.repo_id = $repo_id AND f.evidence_source = 'projector/canonical' AND f.generation_id <> $generation_id
DETACH DELETE f`,
			Parameters: map[string]any{
				"repo_id":       "repo-1",
				"generation_id": "gen-2",
			},
			Drain:    true,
			DrainVar: "f",
		},
	}

	if err := executor.ExecutePhaseGroup(context.Background(), stmts); err != nil {
		t.Fatalf("ExecutePhaseGroup() error = %v, want nil", err)
	}

	// Reader was called 3 times returning 2000, 2000, 500; then a 4th call
	// returns 0 and the loop stops.
	if reader.callIdx != 4 {
		t.Fatalf("drain loop iterations = %d, want 4 (2000, 2000, 500, 0)", reader.callIdx)
	}
}

// TestNornicDBPhaseGroupExecutorDrainLoopStopsImmediatelyOnZero verifies that
// when the first drain call returns 0 the loop runs exactly once.
func TestNornicDBPhaseGroupExecutorDrainLoopStopsImmediatelyOnZero(t *testing.T) {
	t.Parallel()

	reader := &drainCountReader{counts: []int64{}} // always returns 0
	inner := &recordingGroupChunkExecutor{}
	executor := nornicDBPhaseGroupExecutor{
		inner:            inner,
		maxStatements:    100,
		retractBatchSize: 2000,
		drainReader:      reader,
	}

	stmts := []sourcecypher.Statement{
		{
			Operation: sourcecypher.OperationCanonicalRetract,
			Cypher: `MATCH (d:Directory)
WHERE d.repo_id = $repo_id AND d.generation_id <> $generation_id
  AND (d.path IS NULL OR NOT (d.path IN $directory_paths))
DETACH DELETE d`,
			Parameters: map[string]any{
				"repo_id":         "repo-1",
				"generation_id":   "gen-2",
				"directory_paths": []string{"/repo/src"},
			},
			Drain:    true,
			DrainVar: "d",
		},
	}

	if err := executor.ExecutePhaseGroup(context.Background(), stmts); err != nil {
		t.Fatalf("ExecutePhaseGroup() error = %v, want nil", err)
	}
	if reader.callIdx != 1 {
		t.Fatalf("drain loop iterations = %d, want 1 (zero on first call)", reader.callIdx)
	}
}

// TestNornicDBPhaseGroupExecutorDrainLoopEnforcesSafetyCap verifies that the
// safety cap is enforced: if __drained never reaches 0, the loop returns an
// error instead of looping forever.
func TestNornicDBPhaseGroupExecutorDrainLoopEnforcesSafetyCap(t *testing.T) {
	t.Parallel()

	// Always returns a non-zero count — infinite loop without a cap.
	reader := &drainCountReader{counts: make([]int64, 10000)}
	for i := range reader.counts {
		reader.counts[i] = 1
	}
	inner := &recordingGroupChunkExecutor{}
	executor := nornicDBPhaseGroupExecutor{
		inner:            inner,
		maxStatements:    100,
		retractBatchSize: 2000,
		drainReader:      reader,
	}

	stmts := []sourcecypher.Statement{
		{
			Operation: sourcecypher.OperationCanonicalRetract,
			Cypher: `MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)
WHERE f.repo_id = $repo_id AND f.evidence_source = 'projector/canonical' AND f.generation_id <> $generation_id
DETACH DELETE f`,
			Parameters: map[string]any{
				"repo_id":       "repo-1",
				"generation_id": "gen-2",
			},
			Drain:    true,
			DrainVar: "f",
		},
	}

	err := executor.ExecutePhaseGroup(context.Background(), stmts)
	if err == nil {
		t.Fatal("ExecutePhaseGroup() error = nil, want non-nil (safety cap exceeded)")
	}
	if !strings.Contains(err.Error(), "safety cap") && !strings.Contains(err.Error(), "exceeded") {
		t.Fatalf("error = %v, want message mentioning safety cap or exceeded", err)
	}
}

// TestNornicDBPhaseGroupExecutorDrainLoopPropagatesReaderError verifies that
// a RunWrite error during the drain loop is wrapped and returned.
func TestNornicDBPhaseGroupExecutorDrainLoopPropagatesReaderError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("bolt session error")
	reader := &drainCountReader{
		counts:  []int64{2000},
		failAt:  2, // fail on second call
		lastErr: sentinel,
	}
	inner := &recordingGroupChunkExecutor{}
	executor := nornicDBPhaseGroupExecutor{
		inner:            inner,
		maxStatements:    100,
		retractBatchSize: 2000,
		drainReader:      reader,
	}

	stmts := []sourcecypher.Statement{
		{
			Operation: sourcecypher.OperationCanonicalRetract,
			Cypher: `MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)
WHERE f.repo_id = $repo_id AND f.evidence_source = 'projector/canonical' AND f.generation_id <> $generation_id
DETACH DELETE f`,
			Parameters: map[string]any{
				"repo_id":       "repo-1",
				"generation_id": "gen-2",
			},
			Drain:    true,
			DrainVar: "f",
		},
	}

	err := executor.ExecutePhaseGroup(context.Background(), stmts)
	if err == nil {
		t.Fatal("ExecutePhaseGroup() error = nil, want bolt session error")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want wrapping %v", err, sentinel)
	}
}

// TestNornicDBPhaseGroupExecutorNoDrainFallsBackToExistingPath verifies that a
// retract statement without Drain=true still goes through the existing
// ChunkPositiveStringSliceRetractStatement path and does NOT use the drain reader.
func TestNornicDBPhaseGroupExecutorNoDrainFallsBackToExistingPath(t *testing.T) {
	t.Parallel()

	reader := &drainCountReader{}
	inner := &recordingGroupChunkExecutor{}
	executor := nornicDBPhaseGroupExecutor{
		inner:            inner,
		maxStatements:    100,
		retractBatchSize: 2000,
		drainReader:      reader,
	}

	paths := []string{"/repo/a.go", "/repo/b.go"}
	stmts := []sourcecypher.Statement{
		{
			Operation: sourcecypher.OperationCanonicalRetract,
			Cypher: `UNWIND $file_paths AS file_path
MATCH (f:File {path: file_path})
WHERE f.repo_id = $repo_id
DETACH DELETE f`,
			Parameters: map[string]any{
				"file_paths": paths,
				"repo_id":    "repo-1",
			},
			Drain: false, // NOT a drain statement
		},
	}

	if err := executor.ExecutePhaseGroup(context.Background(), stmts); err != nil {
		t.Fatalf("ExecutePhaseGroup() error = %v, want nil", err)
	}

	// Drain reader must not have been called.
	if reader.callIdx != 0 {
		t.Fatalf("drain reader call count = %d, want 0 (non-drain statement)", reader.callIdx)
	}
	// Inner Execute must have been called (existing retract path).
	if len(inner.executeStatements) == 0 {
		t.Fatal("inner Execute not called; expected existing retract path to execute the statement")
	}
}

// TestNornicDBRetractBatchSizeEnvDefault verifies the default retract batch
// size is used when the env var is unset.
func TestNornicDBRetractBatchSizeEnvDefault(t *testing.T) {
	t.Parallel()

	got, err := nornicDBCanonicalRetractBatchSize(func(string) string { return "" })
	if err != nil {
		t.Fatalf("nornicDBCanonicalRetractBatchSize() error = %v, want nil", err)
	}
	if got != defaultNornicDBCanonicalRetractBatchSize {
		t.Fatalf("retract batch size = %d, want %d", got, defaultNornicDBCanonicalRetractBatchSize)
	}
}

// TestNornicDBRetractBatchSizeEnvCustom verifies a valid env override is
// parsed correctly.
func TestNornicDBRetractBatchSizeEnvCustom(t *testing.T) {
	t.Parallel()

	got, err := nornicDBCanonicalRetractBatchSize(func(key string) string {
		if key == nornicDBCanonicalRetractBatchSizeEnv {
			return "500"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("nornicDBCanonicalRetractBatchSize() error = %v, want nil", err)
	}
	if got != 500 {
		t.Fatalf("retract batch size = %d, want 500", got)
	}
}

// TestNornicDBRetractBatchSizeEnvInvalid verifies that invalid values return
// an error.
func TestNornicDBRetractBatchSizeEnvInvalid(t *testing.T) {
	t.Parallel()

	_, err := nornicDBCanonicalRetractBatchSize(func(key string) string {
		if key == nornicDBCanonicalRetractBatchSizeEnv {
			return "not-a-number"
		}
		return ""
	})
	if err == nil {
		t.Fatal("nornicDBCanonicalRetractBatchSize() error = nil, want non-nil for invalid value")
	}
}
