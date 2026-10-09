// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
)

// TestSchemaStatementsForBackendUsesModernFulltextFormOnNeo4j pins #7675:
// Neo4j removed db.index.fulltext.createNodeIndex in 5.0, so the Neo4j
// statement list must carry the CREATE FULLTEXT INDEX form that actually
// runs, never the removed procedure.
func TestSchemaStatementsForBackendUsesModernFulltextFormOnNeo4j(t *testing.T) {
	t.Parallel()

	stmts, err := SchemaStatementsForBackend(SchemaBackendNeo4j)
	if err != nil {
		t.Fatalf("SchemaStatementsForBackend(neo4j) error = %v", err)
	}
	joined := strings.Join(stmts, "\n")
	for _, want := range []string{
		"CREATE FULLTEXT INDEX code_search_index",
		"CREATE FULLTEXT INDEX infra_search_index",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("Neo4j statement list missing %q", want)
		}
	}
	if strings.Contains(joined, "db.index.fulltext.createNodeIndex") {
		t.Errorf("Neo4j statement list still carries the removed procedure form")
	}
}

// TestSchemaStatementsForBackendKeepsProcedureFulltextFormOnNornicDB pins the
// other half of #7675: the NornicDB path (procedure only, no fallback) is
// unchanged.
func TestSchemaStatementsForBackendKeepsProcedureFulltextFormOnNornicDB(t *testing.T) {
	t.Parallel()

	stmts, err := SchemaStatementsForBackend(SchemaBackendNornicDB)
	if err != nil {
		t.Fatalf("SchemaStatementsForBackend(nornicdb) error = %v", err)
	}
	joined := strings.Join(stmts, "\n")
	for _, want := range []string{
		"db.index.fulltext.createNodeIndex('code_search_index'",
		"db.index.fulltext.createNodeIndex('infra_search_index'",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("NornicDB statement list missing %q", want)
		}
	}
	if strings.Contains(joined, "CREATE FULLTEXT INDEX") {
		t.Errorf("NornicDB statement list carries the modern form it skips")
	}
}

// TestEnsureSchemaNeverAttemptsTheRemovedProcedureOnNeo4j proves the #7675
// execution half: a Neo4j schema apply attempts only the modern form, so a
// clean backend sees no ProcedureNotFound failure and no fallback attempt.
func TestEnsureSchemaNeverAttemptsTheRemovedProcedureOnNeo4j(t *testing.T) {
	t.Parallel()

	executor := &schemaRecordingExecutor{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := EnsureSchemaWithBackend(context.Background(), executor, logger, SchemaBackendNeo4j); err != nil {
		t.Fatalf("EnsureSchemaWithBackend(neo4j) error = %v", err)
	}
	for _, stmt := range executor.calls {
		if strings.Contains(stmt.Cypher, "db.index.fulltext.createNodeIndex") {
			t.Fatalf("Neo4j apply attempted the removed procedure: %s", stmt.Cypher)
		}
	}
	var modern int
	for _, stmt := range executor.calls {
		if strings.Contains(stmt.Cypher, "CREATE FULLTEXT INDEX") {
			modern++
		}
	}
	if modern != len(schemaFulltextIndexes) {
		t.Fatalf("Neo4j apply attempted %d modern full-text forms, want %d", modern, len(schemaFulltextIndexes))
	}
}

// TestEnsureSchemaKeepsProcedureOnlyFulltextOnNornicDB proves the NornicDB
// execution path is unchanged: procedure form only, no modern attempt.
func TestEnsureSchemaKeepsProcedureOnlyFulltextOnNornicDB(t *testing.T) {
	t.Parallel()

	executor := &schemaRecordingExecutor{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := EnsureSchemaWithBackend(context.Background(), executor, logger, SchemaBackendNornicDB); err != nil {
		t.Fatalf("EnsureSchemaWithBackend(nornicdb) error = %v", err)
	}
	var procedure, modern int
	for _, stmt := range executor.calls {
		if strings.Contains(stmt.Cypher, "db.index.fulltext.createNodeIndex") {
			procedure++
		}
		if strings.Contains(stmt.Cypher, "CREATE FULLTEXT INDEX") {
			modern++
		}
	}
	if procedure != len(schemaFulltextIndexes) {
		t.Fatalf("NornicDB apply attempted %d procedure forms, want %d", procedure, len(schemaFulltextIndexes))
	}
	if modern != 0 {
		t.Fatalf("NornicDB apply attempted %d modern forms, want 0", modern)
	}
}
