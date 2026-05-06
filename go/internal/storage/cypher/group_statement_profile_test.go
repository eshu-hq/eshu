package cypher

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestExecuteProfiledStatementGroupLogsStatementMetadata(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	stmts := []Statement{{
		Operation: OperationCanonicalUpsert,
		Cypher:    "UNWIND $rows AS row MERGE (n:Function {uid: row.entity_id})",
		Parameters: map[string]any{
			"rows": []map[string]any{
				{"entity_id": "function-1"},
				{"entity_id": "function-2"},
			},
			StatementMetadataPhaseKey:       CanonicalPhaseEntities,
			StatementMetadataEntityLabelKey: "Function",
			StatementMetadataSummaryKey:     "label=Function rows=2",
		},
	}}

	err := ExecuteProfiledStatementGroup(context.Background(), stmts, func(context.Context, Statement) error {
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
	stmts := []Statement{
		{Operation: OperationCanonicalUpsert, Cypher: "RETURN 1"},
		{Operation: OperationCanonicalUpsert, Cypher: "RETURN 2"},
	}

	err := ExecuteProfiledStatementGroup(context.Background(), stmts, func(context.Context, Statement) error {
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
