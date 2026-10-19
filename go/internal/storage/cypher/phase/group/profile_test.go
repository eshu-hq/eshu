// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package group

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/canonical/node"

	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
)

func TestExecuteProfiledStatementGroupLogsStatementMetadata(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	stmts := []sourcecypher.Statement{{
		Operation: sourcecypher.OperationCanonicalUpsert,
		Cypher:    "UNWIND $rows AS row MERGE (n:Function {uid: row.entity_id})",
		Parameters: map[string]any{
			"rows": []map[string]any{
				{"entity_id": "function-1"},
				{"entity_id": "function-2"},
			},
			sourcecypher.StatementMetadataPhaseKey:       sourcecypher.CanonicalPhaseEntities,
			sourcecypher.StatementMetadataEntityLabelKey: "Function",
			sourcecypher.StatementMetadataSummaryKey:     "label=Function rows=2",
		},
	}}

	err := ExecuteProfiledStatementGroup(context.Background(), stmts, func(context.Context, sourcecypher.Statement) error {
		return nil
	}, true, logger)
	if err != nil {
		t.Fatalf("ExecuteProfiledStatementGroup() error = %v, want nil", err)
	}

	got := logs.String()
	for _, want := range []string{
		`"msg":"neo4j grouped statement attempt completed"`,
		`"statement_index":1`,
		`"statement_count":1`,
		`"operation":"canonical_upsert"`,
		`"write_phase":"entities"`,
		`"node_type":"Function"`,
		`"statement_summary":"label=Function rows=2"`,
		`"row_count":2`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("profile log = %s, want %s", got, want)
		}
	}
}

func TestExecuteProfiledStatementGroupStopsOnFirstError(t *testing.T) {
	t.Parallel()

	runErr := errors.New("neo4j statement failed")
	var calls int
	stmts := []sourcecypher.Statement{
		{Operation: sourcecypher.OperationCanonicalUpsert, Cypher: "RETURN 1"},
		{Operation: sourcecypher.OperationCanonicalUpsert, Cypher: "RETURN 2"},
	}

	err := ExecuteProfiledStatementGroup(context.Background(), stmts, func(context.Context, sourcecypher.Statement) error {
		calls++
		return runErr
	}, false, nil)
	if !errors.Is(err, runErr) {
		t.Fatalf("ExecuteProfiledStatementGroup() error = %v, want %v", err, runErr)
	}
	if calls != 1 {
		t.Fatalf("runner calls = %d, want 1", calls)
	}
}

func TestCanonicalFileStatementProfileUsesClosedTemplateIDs(t *testing.T) {
	t.Parallel()
	cases := []struct {
		cypher string
		want   string
	}{
		{node.FileUpdateExistingCypher, "file.nested.update_existing"},
		{node.FileCreateMissingCypher, "file.nested.create_missing"},
		{node.FileFirstGenerationMergeCypher, "file.nested.first_generation"},
		{node.RootFileUpdateExistingCypher, "file.root.update_existing"},
		{node.RootFileCreateMissingCypher, "file.root.create_missing"},
		{node.RootFileFirstGenerationMergeCypher, "file.root.first_generation"},
	}
	for _, tc := range cases {
		stmt := sourcecypher.Statement{Cypher: tc.cypher, Parameters: map[string]any{
			"rows": []map[string]any{{"path": "/sensitive/source.go"}},
		}}
		id, rows, ok := CanonicalFileStatementProfile(stmt)
		if !ok || id != tc.want || rows != 1 {
			t.Fatalf("profile = (%q, %d, %t), want (%q, 1, true)", id, rows, ok, tc.want)
		}
	}
	if id, rows, ok := CanonicalFileStatementProfile(sourcecypher.Statement{Cypher: "MATCH (f:File) RETURN f"}); ok || id != "" || rows != 0 {
		t.Fatalf("unknown query classified: (%q, %d, %t)", id, rows, ok)
	}
}

// TestCanonicalFileWriterStatementsAllHaveProfiles protects the diagnostic
// allowlist against new File templates emitted by the production builder:
// every files-phase statement the writer emits for a file materialization
// must classify. It runs the public Write path and filters to the files
// phase (the unexported buildFileStatements helper is no longer reachable
// across the package split).
func TestCanonicalFileWriterStatementsAllHaveProfiles(t *testing.T) {
	t.Parallel()
	mat := canonical.CanonicalMaterialization{
		ScopeID: "scope", GenerationID: "generation", RepoID: "repo",
		Repository: &canonical.RepositoryRow{RepoID: "repo", Name: "repo", Path: "/repo", LocalPath: "/repo"},
		Files: []canonical.FileRow{
			{RepoID: "repo", Path: "/repo/root.go", RelativePath: "root.go", Name: "root.go"},
			{RepoID: "repo", Path: "/repo/dir/nested.go", RelativePath: "dir/nested.go", DirPath: "/repo/dir", Name: "nested.go"},
		},
	}
	for _, tc := range []struct {
		name            string
		firstGeneration bool
		wantStatements  int
	}{
		{name: "first generation", firstGeneration: true, wantStatements: 2},
		{name: "refresh", firstGeneration: false, wantStatements: 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mat.FirstGeneration = tc.firstGeneration
			exec := &profileRecordingExecutor{}
			writer := sourcecypher.NewCanonicalNodeWriter(exec, 100, nil)
			if err := writer.Write(context.Background(), mat); err != nil {
				t.Fatalf("Write() error = %v", err)
			}
			var fileStmts []sourcecypher.Statement
			for _, stmt := range exec.statements {
				phase, _ := stmt.Parameters[sourcecypher.StatementMetadataPhaseKey].(string)
				if phase == sourcecypher.CanonicalPhaseFiles {
					fileStmts = append(fileStmts, stmt)
				}
			}
			if len(fileStmts) != tc.wantStatements {
				t.Fatalf("File statements = %d, want %d", len(fileStmts), tc.wantStatements)
			}
			for _, stmt := range fileStmts {
				if id, rows, ok := CanonicalFileStatementProfile(stmt); !ok || id == "" || rows != 1 {
					t.Fatalf("File statement lacks diagnostic profile: id=%q rows=%d ok=%t", id, rows, ok)
				}
			}
		})
	}
}

// profileRecordingExecutor records every statement the writer executes so
// profile tests can filter the public Write path down to one phase.
type profileRecordingExecutor struct {
	statements []sourcecypher.Statement
}

func (e *profileRecordingExecutor) Execute(_ context.Context, stmt sourcecypher.Statement) error {
	e.statements = append(e.statements, stmt)
	return nil
}
