package main

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

type recordingReducerStatementExecutor struct {
	calls []sourcecypher.Statement
}

func (e *recordingReducerStatementExecutor) Execute(_ context.Context, stmt sourcecypher.Statement) error {
	e.calls = append(e.calls, stmt)
	return nil
}

func TestSemanticEntityWriterForGraphBackendUsesCanonicalNodeRowsForNornicDB(t *testing.T) {
	t.Parallel()

	executor := &recordingReducerStatementExecutor{}
	writer, err := semanticEntityWriterForGraphBackend(executor, 100, runtimecfg.GraphBackendNornicDB, func(string) string { return "" })
	if err != nil {
		t.Fatalf("semanticEntityWriterForGraphBackend() error = %v", err)
	}

	const docstring = "buildCallChainCypher uses shortestPath((start)-[*]->(end)) for graph traversal."
	result, err := writer.WriteSemanticEntities(context.Background(), reducer.SemanticEntityWrite{
		RepoIDs: []string{"repo-1"},
		Rows: []reducer.SemanticEntityRow{
			{
				RepoID:       "repo-1",
				EntityID:     "function-go-1",
				EntityType:   "Function",
				EntityName:   "buildCallChainCypher",
				FilePath:     "/repo/go/internal/query/code_call_chain.go",
				RelativePath: "go/internal/query/code_call_chain.go",
				Language:     "go",
				StartLine:    22,
				EndLine:      178,
				Metadata: map[string]any{
					"docstring":     docstring,
					"class_context": "CodeHandler",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("WriteSemanticEntities() error = %v", err)
	}
	if got, want := result.CanonicalWrites, 1; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}
	stmt := firstReducerStatementWithOperation(t, executor.calls, sourcecypher.OperationCanonicalUpsert)
	if !strings.Contains(stmt.Cypher, "UNWIND $rows AS row") {
		t.Fatalf("upsert cypher = %q, want batched UNWIND rows", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "MATCH (n:Function {uid: row.entity_id})") {
		t.Fatalf("upsert cypher = %q, want source-local owned semantic node anchor", stmt.Cypher)
	}
	if strings.Contains(stmt.Cypher, "MERGE (n:Function {uid: row.entity_id})") {
		t.Fatalf("upsert cypher = %q, want no duplicate MERGE for source-local owned Function", stmt.Cypher)
	}
	if strings.Contains(stmt.Cypher, "SET n += row.properties") {
		t.Fatalf("upsert cypher = %q, want explicit SET fields for NornicDB hot path", stmt.Cypher)
	}
	if strings.Contains(stmt.Cypher, "shortestPath") {
		t.Fatalf("upsert cypher inlined docstring metadata: %s", stmt.Cypher)
	}
	if strings.Contains(stmt.Cypher, "MATCH (f:File {path: row.file_path})") {
		t.Fatalf("upsert cypher = %q, want source-local to own file containment", stmt.Cypher)
	}
	if strings.Contains(stmt.Cypher, "MERGE (f)-[:CONTAINS]->(n)") {
		t.Fatalf("upsert cypher = %q, want no repeated containment MERGE", stmt.Cypher)
	}
	if strings.Contains(stmt.Cypher, "n.evidence_source = row.evidence_source") {
		t.Fatalf("upsert cypher = %q, want canonical evidence_source preserved", stmt.Cypher)
	}
	rows, ok := stmt.Parameters["rows"].([]map[string]any)
	if !ok {
		t.Fatalf("rows parameter type = %T, want []map[string]any", stmt.Parameters["rows"])
	}
	if _, ok := rows[0]["properties"]; ok {
		t.Fatalf("rows[0] unexpectedly contains properties map: %#v", rows[0])
	}
	if got, want := rows[0]["docstring"], docstring; got != want {
		t.Fatalf("rows[0][docstring] = %#v, want %#v", got, want)
	}
}

func TestSemanticEntityWriterForGraphBackendUsesOwnershipScopedRetractForNornicDB(t *testing.T) {
	t.Parallel()

	executor := &recordingReducerStatementExecutor{}
	writer, err := semanticEntityWriterForGraphBackend(executor, 100, runtimecfg.GraphBackendNornicDB, func(string) string {
		return ""
	})
	if err != nil {
		t.Fatalf("semanticEntityWriterForGraphBackend() error = %v", err)
	}

	_, err = writer.WriteSemanticEntities(context.Background(), reducer.SemanticEntityWrite{
		RepoIDs: []string{"repo-1"},
		Rows: []reducer.SemanticEntityRow{
			semanticFunctionRow("function-go-1"),
		},
	})
	if err != nil {
		t.Fatalf("WriteSemanticEntities() error = %v", err)
	}

	var retracts int
	var functionRetract, moduleRetract sourcecypher.Statement
	for _, call := range executor.calls {
		if call.Operation != sourcecypher.OperationCanonicalRetract {
			continue
		}
		retracts++
		switch call.Parameters[sourcecypher.StatementMetadataEntityLabelKey] {
		case "Function":
			functionRetract = call
		case "Module":
			moduleRetract = call
		}
		if strings.Contains(call.Cypher, "|") {
			t.Fatalf("NornicDB retract cypher = %q, want one label per statement", call.Cypher)
		}
		if !strings.Contains(call.Cypher, "MATCH (n:") {
			t.Fatalf("NornicDB retract cypher = %q, want label-scoped MATCH", call.Cypher)
		}
	}
	if got, want := retracts, 11; got != want {
		t.Fatalf("retract statement count = %d, want %d", got, want)
	}
	if functionRetract.Cypher == "" {
		t.Fatal("missing Function retract statement")
	}
	if strings.Contains(functionRetract.Cypher, "DETACH DELETE") {
		t.Fatalf("Function retract cypher = %q, want semantic property clear", functionRetract.Cypher)
	}
	if !strings.Contains(functionRetract.Cypher, "REMOVE n.impl_context") {
		t.Fatalf("Function retract cypher = %q, want semantic property REMOVE", functionRetract.Cypher)
	}
	if moduleRetract.Cypher == "" {
		t.Fatal("missing Module retract statement")
	}
	if !strings.Contains(moduleRetract.Cypher, "DETACH DELETE n") {
		t.Fatalf("Module retract cypher = %q, want semantic-owned Module delete", moduleRetract.Cypher)
	}
}

func TestSemanticEntityWriterForGraphBackendKeepsBroadRetractForNeo4j(t *testing.T) {
	t.Parallel()

	executor := &recordingReducerStatementExecutor{}
	writer, err := semanticEntityWriterForGraphBackend(executor, 100, runtimecfg.GraphBackendNeo4j, func(string) string {
		return ""
	})
	if err != nil {
		t.Fatalf("semanticEntityWriterForGraphBackend() error = %v", err)
	}

	_, err = writer.WriteSemanticEntities(context.Background(), reducer.SemanticEntityWrite{
		RepoIDs: []string{"repo-1"},
		Rows: []reducer.SemanticEntityRow{
			semanticFunctionRow("function-go-1"),
		},
	})
	if err != nil {
		t.Fatalf("WriteSemanticEntities() error = %v", err)
	}

	var retracts int
	for _, call := range executor.calls {
		if call.Operation != sourcecypher.OperationCanonicalRetract {
			continue
		}
		retracts++
		if !strings.Contains(call.Cypher, ":Annotation|Typedef|TypeAlias") {
			t.Fatalf("Neo4j retract cypher = %q, want broad pipe-label retract", call.Cypher)
		}
	}
	if got, want := retracts, 1; got != want {
		t.Fatalf("retract statement count = %d, want %d", got, want)
	}
}

func TestSemanticEntityWriterForGraphBackendAppliesNornicDBLabelBatchCaps(t *testing.T) {
	t.Parallel()

	executor := &recordingReducerStatementExecutor{}
	writer, err := semanticEntityWriterForGraphBackend(executor, 100, runtimecfg.GraphBackendNornicDB, func(key string) string {
		if key == nornicDBSemanticEntityLabelBatchEnv {
			return "Function=2,Variable=3"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("semanticEntityWriterForGraphBackend() error = %v", err)
	}

	result, err := writer.WriteSemanticEntities(context.Background(), reducer.SemanticEntityWrite{
		RepoIDs: []string{"repo-1"},
		Rows: []reducer.SemanticEntityRow{
			semanticFunctionRow("function-go-1"),
			semanticFunctionRow("function-go-2"),
			semanticFunctionRow("function-go-3"),
			semanticVariableRow("variable-go-1"),
			semanticVariableRow("variable-go-2"),
			semanticVariableRow("variable-go-3"),
			semanticVariableRow("variable-go-4"),
		},
	})
	if err != nil {
		t.Fatalf("WriteSemanticEntities() error = %v", err)
	}
	if got, want := result.CanonicalWrites, 7; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}

	var functionBatches, variableBatches []int
	for _, call := range executor.calls {
		if call.Operation != sourcecypher.OperationCanonicalUpsert {
			continue
		}
		label, _ := call.Parameters[sourcecypher.StatementMetadataEntityLabelKey].(string)
		rows, _ := call.Parameters["rows"].([]map[string]any)
		switch label {
		case "Function":
			functionBatches = append(functionBatches, len(rows))
		case "Variable":
			variableBatches = append(variableBatches, len(rows))
		}
	}
	if got, want := intsString(functionBatches), "[2 1]"; got != want {
		t.Fatalf("Function batch sizes = %s, want %s", got, want)
	}
	if got, want := intsString(variableBatches), "[3 1]"; got != want {
		t.Fatalf("Variable batch sizes = %s, want %s", got, want)
	}
}

func TestSemanticEntityWriterForGraphBackendAppliesDefaultNornicDBLabelCaps(t *testing.T) {
	t.Parallel()

	executor := &recordingReducerStatementExecutor{}
	writer, err := semanticEntityWriterForGraphBackend(executor, 100, runtimecfg.GraphBackendNornicDB, func(string) string {
		return ""
	})
	if err != nil {
		t.Fatalf("semanticEntityWriterForGraphBackend() error = %v", err)
	}

	rows := make([]reducer.SemanticEntityRow, 0, 22)
	for i := 0; i < 11; i++ {
		rows = append(rows, semanticModuleRow(fmt.Sprintf("module-ts-%02d", i)))
		rows = append(rows, semanticImplBlockRow(fmt.Sprintf("impl-rs-%02d", i)))
	}
	result, err := writer.WriteSemanticEntities(context.Background(), reducer.SemanticEntityWrite{
		RepoIDs: []string{"repo-1"},
		Rows:    rows,
	})
	if err != nil {
		t.Fatalf("WriteSemanticEntities() error = %v", err)
	}
	if got, want := result.CanonicalWrites, 22; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}

	var moduleBatches, implBlockBatches []int
	for _, call := range executor.calls {
		if call.Operation != sourcecypher.OperationCanonicalUpsert {
			continue
		}
		label, _ := call.Parameters[sourcecypher.StatementMetadataEntityLabelKey].(string)
		rows, _ := call.Parameters["rows"].([]map[string]any)
		switch label {
		case "Module":
			moduleBatches = append(moduleBatches, len(rows))
		case "ImplBlock":
			implBlockBatches = append(implBlockBatches, len(rows))
		}
	}
	if got, want := intsString(moduleBatches), "[10 1]"; got != want {
		t.Fatalf("Module batch sizes = %s, want %s", got, want)
	}
	if got, want := intsString(implBlockBatches), "[10 1]"; got != want {
		t.Fatalf("ImplBlock batch sizes = %s, want %s", got, want)
	}
}

func TestSemanticEntityWriterForGraphBackendRejectsInvalidNornicDBLabelBatchCaps(t *testing.T) {
	t.Parallel()

	_, err := semanticEntityWriterForGraphBackend(&recordingReducerStatementExecutor{}, 100, runtimecfg.GraphBackendNornicDB, func(key string) string {
		if key == nornicDBSemanticEntityLabelBatchEnv {
			return "Function=0"
		}
		return ""
	})
	if err == nil {
		t.Fatal("semanticEntityWriterForGraphBackend() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), nornicDBSemanticEntityLabelBatchEnv) {
		t.Fatalf("semanticEntityWriterForGraphBackend() error = %q, want env name", err)
	}
}

func firstReducerStatementWithOperation(
	t *testing.T,
	calls []sourcecypher.Statement,
	operation sourcecypher.Operation,
) sourcecypher.Statement {
	t.Helper()
	for _, call := range calls {
		if call.Operation == operation {
			return call
		}
	}
	t.Fatalf("missing statement with operation %q", operation)
	return sourcecypher.Statement{}
}

func semanticFunctionRow(id string) reducer.SemanticEntityRow {
	return reducer.SemanticEntityRow{
		RepoID:       "repo-1",
		EntityID:     id,
		EntityType:   "Function",
		EntityName:   id,
		FilePath:     "/repo/main.go",
		RelativePath: "main.go",
		Language:     "go",
		StartLine:    1,
		EndLine:      2,
	}
}

func semanticVariableRow(id string) reducer.SemanticEntityRow {
	return reducer.SemanticEntityRow{
		RepoID:       "repo-1",
		EntityID:     id,
		EntityType:   "Variable",
		EntityName:   id,
		FilePath:     "/repo/main.go",
		RelativePath: "main.go",
		Language:     "go",
		StartLine:    1,
		EndLine:      1,
	}
}

func semanticModuleRow(id string) reducer.SemanticEntityRow {
	return reducer.SemanticEntityRow{
		RepoID:       "repo-1",
		EntityID:     id,
		EntityType:   "Module",
		EntityName:   id,
		FilePath:     "/repo/main.ts",
		RelativePath: "main.ts",
		Language:     "typescript",
		StartLine:    1,
		EndLine:      1,
	}
}

func semanticImplBlockRow(id string) reducer.SemanticEntityRow {
	return reducer.SemanticEntityRow{
		RepoID:       "repo-1",
		EntityID:     id,
		EntityType:   "ImplBlock",
		EntityName:   id,
		FilePath:     "/repo/lib.rs",
		RelativePath: "lib.rs",
		Language:     "rust",
		StartLine:    1,
		EndLine:      1,
	}
}

func intsString(values []int) string {
	return fmt.Sprint(values)
}
