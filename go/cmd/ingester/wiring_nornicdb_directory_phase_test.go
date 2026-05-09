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
		inner:                  inner,
		maxStatements:          10,
		directoryMaxStatements: 3,
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
