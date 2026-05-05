package cypher

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestEdgeWriterRetractEdgesSQLRelationshipUsesLabelScopedGroup(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{IntentID: "i1", RepositoryID: "repo-a", Payload: map[string]any{"repo_id": "repo-a"}},
	}

	err := writer.RetractEdges(context.Background(), reducer.DomainSQLRelationships, rows, "reducer/sql-relationships")
	if err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}
	if got, want := len(executor.groupCalls), 1; got != want {
		t.Fatalf("ExecuteGroup calls = %d, want %d", got, want)
	}
	stmts := executor.groupCalls[0]
	if got, want := len(stmts), 4; got != want {
		t.Fatalf("group statement count = %d, want %d", got, want)
	}

	assertSQLRetractStatement(t, stmts[0], "SqlView", "REFERENCES_TABLE")
	assertSQLRetractStatement(t, stmts[1], "SqlFunction", "REFERENCES_TABLE")
	assertSQLRetractStatement(t, stmts[2], "SqlTable", "HAS_COLUMN")
	assertSQLRetractStatement(t, stmts[3], "SqlTrigger", "TRIGGERS")
	for _, stmt := range stmts {
		if strings.Contains(stmt.Cypher, "REFERENCES_TABLE|HAS_COLUMN|TRIGGERS") {
			t.Fatalf("cypher uses broad relationship alternation: %s", stmt.Cypher)
		}
	}
}

func TestBuildRetractSQLRelationshipEdgeStatementsUsesSharedParameters(t *testing.T) {
	t.Parallel()

	stmts := BuildRetractSQLRelationshipEdgeStatements([]string{"repo-a", "repo-b"}, "reducer/sql-relationships")
	if got, want := len(stmts), 4; got != want {
		t.Fatalf("statement count = %d, want %d", got, want)
	}

	for _, stmt := range stmts {
		if stmt.Operation != OperationCanonicalRetract {
			t.Fatalf("operation = %q, want %q", stmt.Operation, OperationCanonicalRetract)
		}
		repoIDs, ok := stmt.Parameters["repo_ids"].([]string)
		if !ok {
			t.Fatalf("repo_ids parameter type = %T, want []string", stmt.Parameters["repo_ids"])
		}
		if got, want := strings.Join(repoIDs, ","), "repo-a,repo-b"; got != want {
			t.Fatalf("repo_ids = %q, want %q", got, want)
		}
		if got, want := stmt.Parameters["evidence_source"], "reducer/sql-relationships"; got != want {
			t.Fatalf("evidence_source = %v, want %v", got, want)
		}
	}
}

func assertSQLRetractStatement(
	t *testing.T,
	stmt Statement,
	sourceLabel string,
	relationshipType string,
) {
	t.Helper()

	if !strings.Contains(stmt.Cypher, "MATCH (source:"+sourceLabel+")-[rel:"+relationshipType+"]->()") {
		t.Fatalf("cypher missing scoped match for %s/%s: %s", sourceLabel, relationshipType, stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "source.repo_id IN $repo_ids") {
		t.Fatalf("cypher missing repo_id predicate: %s", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "rel.evidence_source = $evidence_source") {
		t.Fatalf("cypher missing evidence_source predicate: %s", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "DELETE rel") {
		t.Fatalf("cypher missing DELETE: %s", stmt.Cypher)
	}
}
