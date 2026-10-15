// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nornicdb

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// captureGroupExecutor records every statement group the writer executes.
type captureGroupExecutor struct {
	groups [][]sourcecypher.Statement
}

func (e *captureGroupExecutor) Execute(_ context.Context, _ sourcecypher.Statement) error {
	return nil
}

func (e *captureGroupExecutor) ExecuteGroup(_ context.Context, stmts []sourcecypher.Statement) error {
	e.groups = append(e.groups, stmts)
	return nil
}

// TestConfigureCanonicalWriterSelectsBareRepositoryRetirement is the
// fail-closed pin for #8173: every NornicDB canonical writer must emit the
// bare path-conflict retirement shape. The attributed shape's OPTIONAL
// MATCH + WITH collect composition drops its id guard on NornicDB and
// DETACH DELETEs the live node in steady state, losing reducer edges no
// writer re-arms. If the WithBareRepositoryRetirement call below is
// removed, the cleanup statement carries OPTIONAL MATCH again and this
// test fails.
func TestConfigureCanonicalWriterSelectsBareRepositoryRetirement(t *testing.T) {
	t.Parallel()

	exec := &captureGroupExecutor{}
	writer := ConfigureCanonicalWriter(
		sourcecypher.NewCanonicalNodeWriter(exec, 500, nil),
		DefaultWriterConfig(),
	)
	mat := canonical.CanonicalMaterialization{
		ScopeID:      "scope:wiring",
		GenerationID: "gen-2",
		RepoID:       "repository:wiring",
		RepoPath:     "/wiring",
		Repository: &canonical.RepositoryRow{
			RepoID: "repository:wiring", Name: "wiring", Path: "/wiring",
			LocalPath: "/wiring",
		},
	}
	if err := writer.Write(context.Background(), mat); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	var cleanups []sourcecypher.Statement
	for _, group := range exec.groups {
		for _, stmt := range group {
			if summary, _ := stmt.Parameters[sourcecypher.StatementMetadataSummaryKey].(string); summary == "repository_cleanup lookup=path_conflict" {
				cleanups = append(cleanups, stmt)
			}
		}
	}
	if len(cleanups) != 1 {
		t.Fatalf("repository_cleanup statements = %d, want 1", len(cleanups))
	}
	if strings.Contains(cleanups[0].Cypher, "OPTIONAL MATCH") ||
		strings.Contains(cleanups[0].Cypher, "RETURN dropped") {
		t.Fatalf("NornicDB writer emits the attributed retirement shape:\n%s", cleanups[0].Cypher)
	}
}
