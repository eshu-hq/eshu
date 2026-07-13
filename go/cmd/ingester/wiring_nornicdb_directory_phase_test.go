// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

func TestNornicDBPhaseGroupExecutorUsesDirectorySpecificStatementLimit(t *testing.T) {
	t.Parallel()

	inner := &recordingGroupChunkExecutor{}
	executor := nornicDBPhaseGroupExecutor{
		Inner:                  inner,
		MaxStatements:          10,
		DirectoryMaxStatements: 3,
	}

	stmts := []sourcecypher.Statement{
		{Cypher: "RETURN 1", Parameters: map[string]any{"_eshu_phase": sourcecypher.CanonicalPhaseDirectories}},
		{Cypher: "RETURN 2", Parameters: map[string]any{"_eshu_phase": sourcecypher.CanonicalPhaseDirectories}},
		{Cypher: "RETURN 3", Parameters: map[string]any{"_eshu_phase": sourcecypher.CanonicalPhaseDirectories}},
		{Cypher: "RETURN 4", Parameters: map[string]any{"_eshu_phase": sourcecypher.CanonicalPhaseDirectories}},
		{Cypher: "RETURN 5", Parameters: map[string]any{"_eshu_phase": sourcecypher.CanonicalPhaseDirectories}},
		{Cypher: "RETURN 6", Parameters: map[string]any{"_eshu_phase": sourcecypher.CanonicalPhaseDirectories}},
		{Cypher: "RETURN 7", Parameters: map[string]any{"_eshu_phase": sourcecypher.CanonicalPhaseDirectories}},
	}

	if err := executor.ExecutePhaseGroup(context.Background(), stmts); err != nil {
		t.Fatalf("ExecutePhaseGroup() error = %v, want nil", err)
	}
	if got, want := inner.groupSizes, []int{3, 3, 1}; !equalIntSlices(got, want) {
		t.Fatalf("directory group sizes = %v, want %v", got, want)
	}
}

// TestNornicDBPhaseGroupExecutorAppliesDirectoryLimitToDirectoryEdges proves the
// directory_edges phase shares the directory request-size budget (it carries the
// same directory row maps), rather than falling back to the broad phase-group
// default. Regression for #4019 review (codex P2).
func TestNornicDBPhaseGroupExecutorAppliesDirectoryLimitToDirectoryEdges(t *testing.T) {
	t.Parallel()

	inner := &recordingGroupChunkExecutor{}
	executor := nornicDBPhaseGroupExecutor{
		Inner:                  inner,
		MaxStatements:          10,
		DirectoryMaxStatements: 3,
	}

	stmts := []sourcecypher.Statement{
		{Cypher: "RETURN 1", Parameters: map[string]any{"_eshu_phase": sourcecypher.CanonicalPhaseDirectoryEdges}},
		{Cypher: "RETURN 2", Parameters: map[string]any{"_eshu_phase": sourcecypher.CanonicalPhaseDirectoryEdges}},
		{Cypher: "RETURN 3", Parameters: map[string]any{"_eshu_phase": sourcecypher.CanonicalPhaseDirectoryEdges}},
		{Cypher: "RETURN 4", Parameters: map[string]any{"_eshu_phase": sourcecypher.CanonicalPhaseDirectoryEdges}},
		{Cypher: "RETURN 5", Parameters: map[string]any{"_eshu_phase": sourcecypher.CanonicalPhaseDirectoryEdges}},
	}

	if err := executor.ExecutePhaseGroup(context.Background(), stmts); err != nil {
		t.Fatalf("ExecutePhaseGroup() error = %v, want nil", err)
	}
	// directoryMaxStatements=3 must apply (chunks of 3,2), NOT the maxStatements=10 default.
	if got, want := inner.groupSizes, []int{3, 2}; !equalIntSlices(got, want) {
		t.Fatalf("directory_edges group sizes = %v, want %v (directory cap, not the broad default)", got, want)
	}
}
