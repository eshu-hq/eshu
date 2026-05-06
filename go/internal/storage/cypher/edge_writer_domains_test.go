package cypher

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestBatchedWriteEdgesUsesUNWINDCypher(t *testing.T) {
	t.Parallel()

	domains := []struct {
		domain   string
		payload  map[string]any
		contains string
	}{
		{
			domain:   reducer.DomainPlatformInfra,
			payload:  map[string]any{"repo_id": "r1", "platform_id": "p1"},
			contains: "UNWIND $rows AS row",
		},
		{
			domain:   reducer.DomainRepoDependency,
			payload:  map[string]any{"repo_id": "r1", "target_repo_id": "r2"},
			contains: "UNWIND $rows AS row",
		},
		{
			domain:   reducer.DomainWorkloadDependency,
			payload:  map[string]any{"workload_id": "w1", "target_workload_id": "w2"},
			contains: "UNWIND $rows AS row",
		},
		{
			domain:   reducer.DomainCodeCalls,
			payload:  map[string]any{"caller_entity_id": "c1", "callee_entity_id": "c2"},
			contains: "UNWIND $rows AS row",
		},
	}

	for _, tc := range domains {
		t.Run(tc.domain, func(t *testing.T) {
			t.Parallel()
			executor := &recordingExecutor{}
			writer := NewEdgeWriter(executor, 0)

			rows := []reducer.SharedProjectionIntentRow{
				{IntentID: "i1", RepositoryID: "r1", Payload: tc.payload},
			}
			err := writer.WriteEdges(context.Background(), tc.domain, rows, "test")
			if err != nil {
				t.Fatalf("WriteEdges(%s) error = %v", tc.domain, err)
			}
			if !strings.Contains(executor.calls[0].Cypher, tc.contains) {
				t.Fatalf("cypher missing %q: %s", tc.contains, executor.calls[0].Cypher)
			}
		})
	}
}

func TestEdgeWriterWriteEdgesInheritanceDispatch(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "i1",
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"child_entity_id":   "entity:class:child",
				"parent_entity_id":  "entity:class:parent",
				"repo_id":           "repo-a",
				"relationship_type": "INHERITS",
			},
		},
	}

	err := writer.WriteEdges(context.Background(), reducer.DomainInheritanceEdges, rows, "reducer/inheritance")
	if err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	if !strings.Contains(executor.calls[0].Cypher, "INHERITS") {
		t.Fatalf("cypher missing INHERITS: %s", executor.calls[0].Cypher)
	}
	if !strings.Contains(executor.calls[0].Cypher, "MATCH (child:Function|Class|Interface|Trait|Struct|Enum|Protocol {uid: row.child_entity_id})") {
		t.Fatalf("cypher missing labeled child match: %s", executor.calls[0].Cypher)
	}
	if !strings.Contains(executor.calls[0].Cypher, "MATCH (parent:Function|Class|Interface|Trait|Struct|Enum|Protocol {uid: row.parent_entity_id})") {
		t.Fatalf("cypher missing labeled parent match: %s", executor.calls[0].Cypher)
	}
	if !strings.Contains(executor.calls[0].Cypher, "UNWIND") {
		t.Fatalf("cypher missing UNWIND: %s", executor.calls[0].Cypher)
	}
	batchRows, ok := executor.calls[0].Parameters["rows"].([]map[string]any)
	if !ok || len(batchRows) != 1 {
		t.Fatalf("expected 1 row in batch, got %v", executor.calls[0].Parameters["rows"])
	}
	if got, want := batchRows[0]["child_entity_id"], "entity:class:child"; got != want {
		t.Fatalf("child_entity_id = %v, want %v", got, want)
	}
	if got, want := batchRows[0]["parent_entity_id"], "entity:class:parent"; got != want {
		t.Fatalf("parent_entity_id = %v, want %v", got, want)
	}
	if got, want := batchRows[0]["evidence_source"], "reducer/inheritance"; got != want {
		t.Fatalf("evidence_source = %v, want %v", got, want)
	}
}

func TestEdgeWriterWriteEdgesInheritanceSkipsEmptyFields(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{IntentID: "i1", RepositoryID: "r1", Payload: map[string]any{"child_entity_id": "", "parent_entity_id": "p1"}},
		{IntentID: "i2", RepositoryID: "r1", Payload: map[string]any{"child_entity_id": "c1", "parent_entity_id": ""}},
	}

	err := writer.WriteEdges(context.Background(), reducer.DomainInheritanceEdges, rows, "reducer/inheritance")
	if err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	if got := len(executor.calls); got != 0 {
		t.Fatalf("executor calls = %d, want 0 (all rows filtered)", got)
	}
}

func TestEdgeWriterWriteEdgesSQLRelationshipDispatch(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "i1",
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"source_entity_id":   "entity:sql_view:my_view",
				"target_entity_id":   "entity:sql_table:users",
				"source_entity_type": "SqlView",
				"target_entity_type": "SqlTable",
				"repo_id":            "repo-a",
				"relationship_type":  "REFERENCES_TABLE",
			},
		},
	}

	err := writer.WriteEdges(context.Background(), reducer.DomainSQLRelationships, rows, "reducer/sql-relationships")
	if err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	if !strings.Contains(executor.calls[0].Cypher, "REFERENCES_TABLE") {
		t.Fatalf("cypher missing REFERENCES_TABLE: %s", executor.calls[0].Cypher)
	}
	if !strings.Contains(executor.calls[0].Cypher, "MATCH (source:SqlView {uid: row.source_entity_id})") {
		t.Fatalf("cypher missing exact source label match: %s", executor.calls[0].Cypher)
	}
	if !strings.Contains(executor.calls[0].Cypher, "MATCH (target:SqlTable {uid: row.target_entity_id})") {
		t.Fatalf("cypher missing exact target label match: %s", executor.calls[0].Cypher)
	}
	if strings.Contains(executor.calls[0].Cypher, "HAS_COLUMN") {
		t.Fatalf("cypher unexpectedly included HAS_COLUMN edge: %s", executor.calls[0].Cypher)
	}
	if strings.Contains(executor.calls[0].Cypher, "TRIGGERS") {
		t.Fatalf("cypher unexpectedly included TRIGGERS edge: %s", executor.calls[0].Cypher)
	}
	batchRows, ok := executor.calls[0].Parameters["rows"].([]map[string]any)
	if !ok || len(batchRows) != 1 {
		t.Fatalf("expected 1 row in batch, got %v", executor.calls[0].Parameters["rows"])
	}
	if got, want := batchRows[0]["source_entity_id"], "entity:sql_view:my_view"; got != want {
		t.Fatalf("source_entity_id = %v, want %v", got, want)
	}
	if got, want := batchRows[0]["relationship_type"], "REFERENCES_TABLE"; got != want {
		t.Fatalf("relationship_type = %v, want %v", got, want)
	}
}

func TestEdgeWriterWriteEdgesSQLRelationshipFallsBackForRowsWithoutEntityTypes(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "i1",
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"source_entity_id":  "entity:sql_view:my_view",
				"target_entity_id":  "entity:sql_table:users",
				"repo_id":           "repo-a",
				"relationship_type": "REFERENCES_TABLE",
			},
		},
	}

	if err := writer.WriteEdges(context.Background(), reducer.DomainSQLRelationships, rows, "reducer/sql-relationships"); err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	if !strings.Contains(executor.calls[0].Cypher, "MATCH (source:SqlTable|SqlView|SqlFunction|SqlTrigger|SqlIndex|SqlColumn {uid: row.source_entity_id})") {
		t.Fatalf("cypher missing compatibility source match: %s", executor.calls[0].Cypher)
	}
}

func TestEdgeWriterWriteEdgesSQLRelationshipDispatchesRelationshipTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		relationshipType string
		expectedEdge     string
	}{
		{name: "has column", relationshipType: "HAS_COLUMN", expectedEdge: "HAS_COLUMN"},
		{name: "triggers", relationshipType: "TRIGGERS", expectedEdge: "TRIGGERS"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			executor := &recordingExecutor{}
			writer := NewEdgeWriter(executor, 0)

			rows := []reducer.SharedProjectionIntentRow{
				{
					IntentID:     "i1",
					RepositoryID: "repo-a",
					Payload: map[string]any{
						"source_entity_id":  "entity:sql_view:my_view",
						"target_entity_id":  "entity:sql_table:users",
						"repo_id":           "repo-a",
						"relationship_type": tt.relationshipType,
					},
				},
			}

			if err := writer.WriteEdges(context.Background(), reducer.DomainSQLRelationships, rows, "reducer/sql-relationships"); err != nil {
				t.Fatalf("WriteEdges() error = %v", err)
			}
			if got, want := len(executor.calls), 1; got != want {
				t.Fatalf("executor calls = %d, want %d", got, want)
			}
			if !strings.Contains(executor.calls[0].Cypher, tt.expectedEdge) {
				t.Fatalf("cypher missing %s edge: %s", tt.expectedEdge, executor.calls[0].Cypher)
			}
			if rowsOut, ok := executor.calls[0].Parameters["rows"].([]map[string]any); !ok || len(rowsOut) != 1 {
				t.Fatalf("expected 1 row in batch, got %v", executor.calls[0].Parameters["rows"])
			}
		})
	}
}

func TestEdgeWriterWriteEdgesSQLRelationshipSkipsEmptyFields(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{IntentID: "i1", RepositoryID: "r1", Payload: map[string]any{"source_entity_id": "", "target_entity_id": "t1"}},
		{IntentID: "i2", RepositoryID: "r1", Payload: map[string]any{"source_entity_id": "s1", "target_entity_id": ""}},
	}

	err := writer.WriteEdges(context.Background(), reducer.DomainSQLRelationships, rows, "reducer/sql-relationships")
	if err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	if got := len(executor.calls); got != 0 {
		t.Fatalf("executor calls = %d, want 0 (all rows filtered)", got)
	}
}

func TestEdgeWriterRetractEdgesInheritanceDispatch(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{IntentID: "i1", RepositoryID: "repo-a", Payload: map[string]any{"repo_id": "repo-a"}},
	}

	err := writer.RetractEdges(context.Background(), reducer.DomainInheritanceEdges, rows, "reducer/inheritance")
	if err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	if !strings.Contains(executor.calls[0].Cypher, "INHERITS") {
		t.Fatalf("cypher missing INHERITS: %s", executor.calls[0].Cypher)
	}
	if !strings.Contains(executor.calls[0].Cypher, "DELETE rel") {
		t.Fatalf("cypher missing DELETE: %s", executor.calls[0].Cypher)
	}
}

func TestEdgeWriterRetractEdgesSQLRelationshipDispatch(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{IntentID: "i1", RepositoryID: "repo-a", Payload: map[string]any{"repo_id": "repo-a"}},
	}

	err := writer.RetractEdges(context.Background(), reducer.DomainSQLRelationships, rows, "reducer/sql-relationships")
	if err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	if !strings.Contains(executor.calls[0].Cypher, "REFERENCES_TABLE") {
		t.Fatalf("cypher missing REFERENCES_TABLE: %s", executor.calls[0].Cypher)
	}
	if !strings.Contains(executor.calls[0].Cypher, "HAS_COLUMN") {
		t.Fatalf("cypher missing HAS_COLUMN: %s", executor.calls[0].Cypher)
	}
	if !strings.Contains(executor.calls[0].Cypher, "TRIGGERS") {
		t.Fatalf("cypher missing TRIGGERS: %s", executor.calls[0].Cypher)
	}
	if !strings.Contains(executor.calls[0].Cypher, "DELETE rel") {
		t.Fatalf("cypher missing DELETE: %s", executor.calls[0].Cypher)
	}
}

func TestBatchedWriteEdgesUsesUNWINDCypherIncludesNewDomains(t *testing.T) {
	t.Parallel()

	domains := []struct {
		domain   string
		payload  map[string]any
		contains string
	}{
		{
			domain:   reducer.DomainInheritanceEdges,
			payload:  map[string]any{"child_entity_id": "c1", "parent_entity_id": "p1"},
			contains: "UNWIND $rows AS row",
		},
		{
			domain:   reducer.DomainSQLRelationships,
			payload:  map[string]any{"source_entity_id": "s1", "target_entity_id": "t1", "relationship_type": "REFERENCES_TABLE"},
			contains: "UNWIND $rows AS row",
		},
	}

	for _, tc := range domains {
		t.Run(tc.domain, func(t *testing.T) {
			t.Parallel()
			executor := &recordingExecutor{}
			writer := NewEdgeWriter(executor, 0)

			rows := []reducer.SharedProjectionIntentRow{
				{IntentID: "i1", RepositoryID: "r1", Payload: tc.payload},
			}
			err := writer.WriteEdges(context.Background(), tc.domain, rows, "test")
			if err != nil {
				t.Fatalf("WriteEdges(%s) error = %v", tc.domain, err)
			}
			if !strings.Contains(executor.calls[0].Cypher, tc.contains) {
				t.Fatalf("cypher missing %q: %s", tc.contains, executor.calls[0].Cypher)
			}
		})
	}
}

type recordingGroupExecutor struct {
	groupCalls [][]Statement
}

func (r *recordingGroupExecutor) Execute(context.Context, Statement) error {
	return nil
}

func (r *recordingGroupExecutor) ExecuteGroup(_ context.Context, stmts []Statement) error {
	cloned := make([]Statement, len(stmts))
	copy(cloned, stmts)
	r.groupCalls = append(r.groupCalls, cloned)
	return nil
}

// Suppress unused import for time package used only by SharedProjectionIntentRow.
var _ = time.Now
