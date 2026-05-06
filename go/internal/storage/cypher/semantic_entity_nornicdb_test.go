package cypher

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestSemanticEntityWriterConstructorsSetExclusiveWriteModes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		got  *SemanticEntityWriter
		want semanticEntityWriteMode
	}{
		{
			name: "legacy row templates",
			got:  NewSemanticEntityWriter(&recordingExecutor{}, 0),
			want: semanticEntityWriteModeLegacyRows,
		},
		{
			name: "single-row parameterized properties",
			got:  NewSemanticEntityWriterWithParameterizedRows(&recordingExecutor{}, 0),
			want: semanticEntityWriteModeParameterizedRows,
		},
		{
			name: "batched property maps",
			got:  NewSemanticEntityWriterWithBatchedProperties(&recordingExecutor{}, 0),
			want: semanticEntityWriteModeBatchedProperties,
		},
		{
			name: "merge-first rows",
			got:  NewSemanticEntityWriterWithMergeFirstRows(&recordingExecutor{}, 0),
			want: semanticEntityWriteModeMergeFirstRows,
		},
		{
			name: "canonical node rows",
			got:  NewSemanticEntityWriterWithCanonicalNodeRows(&recordingExecutor{}, 0),
			want: semanticEntityWriteModeCanonicalNodeRows,
		},
	}

	for _, tt := range tests {
		if tt.got.writeMode != tt.want {
			t.Fatalf("%s writeMode = %v, want %v", tt.name, tt.got.writeMode, tt.want)
		}
	}
}

func TestSemanticEntityWriterWithCanonicalNodeRowsSkipsFileContainmentForCanonicalOwnedLabels(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewSemanticEntityWriterWithCanonicalNodeRows(executor, 100)

	result, err := writer.WriteSemanticEntities(context.Background(), reducer.SemanticEntityWrite{
		RepoIDs:     []string{"repo-1"},
		SkipRetract: true,
		Rows: []reducer.SemanticEntityRow{
			semanticNornicDBFunctionRow("function-go-1"),
		},
	})
	if err != nil {
		t.Fatalf("WriteSemanticEntities() error = %v", err)
	}
	if got, want := result.CanonicalWrites, 1; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}

	upsert := executor.calls[0]
	if !strings.Contains(upsert.Cypher, "UNWIND $rows AS row\nMATCH (n:Function {uid: row.entity_id})") {
		t.Fatalf("upsert cypher = %q, want canonical Function match shape", upsert.Cypher)
	}
	if strings.Contains(upsert.Cypher, "MERGE (n:Function {uid: row.entity_id})") {
		t.Fatalf("upsert cypher = %q, want no duplicate MERGE for source-local owned Function", upsert.Cypher)
	}
	if strings.Contains(upsert.Cypher, "MATCH (f:File") {
		t.Fatalf("upsert cypher = %q, want no file match in canonical-node mode", upsert.Cypher)
	}
	if strings.Contains(upsert.Cypher, "MERGE (f)-[:CONTAINS]->(n)") {
		t.Fatalf("upsert cypher = %q, want source-local to own File CONTAINS edges", upsert.Cypher)
	}
	if strings.Contains(upsert.Cypher, "n.evidence_source = row.evidence_source") {
		t.Fatalf("upsert cypher = %q, want canonical evidence_source preserved", upsert.Cypher)
	}
	if strings.Contains(upsert.Cypher, "SET n += row.properties") {
		t.Fatalf("upsert cypher = %q, want explicit SET fields for NornicDB hot path", upsert.Cypher)
	}
	if got, want := upsert.Parameters[StatementMetadataEntityLabelKey], "Function"; got != want {
		t.Fatalf("label metadata = %#v, want %#v", got, want)
	}
}

func TestSemanticEntityCanonicalNodeRowsKeepsSemanticOwnedModuleContainment(t *testing.T) {
	t.Parallel()

	cypher := semanticEntityCanonicalNodeRowsUpsertCypher("Module", semanticModuleUpsertCypher)
	if !strings.Contains(cypher, "MERGE (n:Module {uid: row.entity_id})") {
		t.Fatalf("cypher = %q, want Module uid merge", cypher)
	}
	if !strings.Contains(cypher, "MATCH (f:File {path: row.file_path})") {
		t.Fatalf("cypher = %q, want Module file match because canonical Module uses name key", cypher)
	}
	if !strings.Contains(cypher, "MERGE (f)-[:CONTAINS]->(n)") {
		t.Fatalf("cypher = %q, want Module containment to remain semantic-owned", cypher)
	}
}

func TestSemanticEntityCanonicalNodeRowsRewritesOnlyCanonicalOwnedLabels(t *testing.T) {
	t.Parallel()

	for _, plan := range semanticEntityPlans() {
		t.Run(plan.label, func(t *testing.T) {
			t.Parallel()

			cypher := semanticEntityCanonicalNodeRowsUpsertCypher(plan.label, plan.cypher)
			if semanticEntityCanonicalNodeOwnedLabel(plan.label) {
				if strings.Contains(cypher, "MATCH (f:File") ||
					strings.Contains(cypher, "MERGE (f)-[:CONTAINS]->(n)") {
					t.Fatalf("cypher = %q, want no file containment for canonical-owned %s", cypher, plan.label)
				}
				if strings.Contains(cypher, "n.evidence_source = row.evidence_source") {
					t.Fatalf("cypher = %q, want canonical evidence_source preserved for %s", cypher, plan.label)
				}
				return
			}
			if !strings.Contains(cypher, "MERGE (f)-[:CONTAINS]->(n)") {
				t.Fatalf("cypher = %q, want containment retained for semantic-owned %s", cypher, plan.label)
			}
		})
	}
}

func TestSemanticEntityCanonicalNodeRowsClearPropertiesWithoutDeletingCanonicalNodes(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewSemanticEntityWriterWithCanonicalNodeRows(executor, 100)

	_, err := writer.WriteSemanticEntities(context.Background(), reducer.SemanticEntityWrite{
		RepoIDs: []string{"repo-1"},
		Rows: []reducer.SemanticEntityRow{
			semanticNornicDBFunctionRow("function-go-1"),
		},
	})
	if err != nil {
		t.Fatalf("WriteSemanticEntities() error = %v", err)
	}

	functionRetractIndex := -1
	moduleRetractIndex := -1
	for i, call := range executor.calls {
		switch call.Parameters[StatementMetadataEntityLabelKey] {
		case "Function":
			if call.Operation == OperationCanonicalRetract {
				functionRetractIndex = i
			}
		case "Module":
			if call.Operation == OperationCanonicalRetract {
				moduleRetractIndex = i
			}
		}
	}
	if functionRetractIndex < 0 {
		t.Fatal("missing Function retract/clear statement")
	}
	functionRetract := executor.calls[functionRetractIndex]
	if strings.Contains(functionRetract.Cypher, "DETACH DELETE") {
		t.Fatalf("function retract cypher = %q, want property clear not node delete", functionRetract.Cypher)
	}
	if !strings.Contains(functionRetract.Cypher, "REMOVE n.impl_context") ||
		!strings.Contains(functionRetract.Cypher, "n.docstring") {
		t.Fatalf("function retract cypher = %q, want semantic property REMOVE list", functionRetract.Cypher)
	}
	if moduleRetractIndex < 0 {
		t.Fatal("missing Module retract statement")
	}
	moduleRetract := executor.calls[moduleRetractIndex]
	if !strings.Contains(moduleRetract.Cypher, "DETACH DELETE n") {
		t.Fatalf("module retract cypher = %q, want semantic-owned Module delete", moduleRetract.Cypher)
	}
}

func TestSemanticEntityWriterWithMergeFirstRowsUsesNornicDBHotPathShape(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewSemanticEntityWriterWithMergeFirstRows(executor, 100).WithLabelScopedRetract()

	result, err := writer.WriteSemanticEntities(context.Background(), reducer.SemanticEntityWrite{
		RepoIDs: []string{"repo-1"},
		Rows: []reducer.SemanticEntityRow{
			semanticNornicDBFunctionRow("function-go-1"),
		},
	})
	if err != nil {
		t.Fatalf("WriteSemanticEntities() error = %v", err)
	}
	if got, want := result.CanonicalWrites, 1; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}

	upsert := executor.calls[len(semanticEntityPlans())]
	if !strings.Contains(upsert.Cypher, "UNWIND $rows AS row\nMERGE (n:Function {uid: row.entity_id})") {
		t.Fatalf("upsert cypher = %q, want merge-first function shape", upsert.Cypher)
	}
	if strings.Contains(upsert.Cypher, "SET n += row.properties") {
		t.Fatalf("upsert cypher = %q, want explicit SET fields for NornicDB hot path", upsert.Cypher)
	}
	mergeIndex := strings.Index(upsert.Cypher, "MERGE (n:Function {uid: row.entity_id})")
	fileMatchIndex := strings.Index(upsert.Cypher, "MATCH (f:File {path: row.file_path})")
	if mergeIndex < 0 || fileMatchIndex < 0 || mergeIndex > fileMatchIndex {
		t.Fatalf("upsert cypher = %q, want node MERGE before file MATCH", upsert.Cypher)
	}
	rows, ok := upsert.Parameters["rows"].([]map[string]any)
	if !ok {
		t.Fatalf("rows parameter type = %T, want []map[string]any", upsert.Parameters["rows"])
	}
	if _, ok := rows[0]["properties"]; ok {
		t.Fatalf("rows[0] unexpectedly contains properties map: %#v", rows[0])
	}
}

func TestSemanticEntityMergeFirstRowsRewritesEverySemanticPlan(t *testing.T) {
	t.Parallel()

	for _, plan := range semanticEntityPlans() {
		t.Run(plan.label, func(t *testing.T) {
			t.Parallel()

			cypher := semanticEntityMergeFirstRowsUpsertCypher(plan.cypher)
			expectedMerge := "UNWIND $rows AS row\nMERGE (n:" + plan.label + " {uid: row.entity_id})"
			if !strings.Contains(cypher, expectedMerge) {
				t.Fatalf("cypher = %q, want %q", cypher, expectedMerge)
			}
			mergeIndex := strings.Index(cypher, "MERGE (n:"+plan.label+" {uid: row.entity_id})")
			fileMatchIndex := strings.Index(cypher, "MATCH (f:File {path: row.file_path})")
			if mergeIndex < 0 || fileMatchIndex < 0 || mergeIndex > fileMatchIndex {
				t.Fatalf("cypher = %q, want node MERGE before file MATCH", cypher)
			}
			if strings.Contains(cypher, "SET n += row.properties") {
				t.Fatalf("cypher = %q, want explicit SET fields", cypher)
			}
		})
	}
}

func TestSemanticEntityWriterWithParameterizedRowsAvoidsInlineSemanticMetadata(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewSemanticEntityWriterWithParameterizedRows(executor, 0)

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
	if got, want := len(executor.calls), 2; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}

	stmt := executor.calls[1]
	if strings.Contains(stmt.Cypher, "shortestPath") {
		t.Fatalf("upsert cypher inlined shortestPath metadata: %s", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "SET n += $properties") {
		t.Fatalf("upsert cypher = %q, want parameterized properties merge", stmt.Cypher)
	}
	if got, want := stmt.Parameters["entity_id"], "function-go-1"; got != want {
		t.Fatalf("entity_id = %#v, want %#v", got, want)
	}
	properties, ok := stmt.Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties type = %T, want map[string]any", stmt.Parameters["properties"])
	}
	if got, want := properties["docstring"], docstring; got != want {
		t.Fatalf("properties[docstring] = %#v, want %#v", got, want)
	}
	if got, want := properties["class_context"], "CodeHandler"; got != want {
		t.Fatalf("properties[class_context] = %#v, want %#v", got, want)
	}
}

func TestSemanticEntityWriterWithLabelScopedRetractSplitsBroadRetractByLabel(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewSemanticEntityWriterWithBatchedProperties(executor, 100).WithLabelScopedRetract()

	result, err := writer.WriteSemanticEntities(context.Background(), reducer.SemanticEntityWrite{
		RepoIDs: []string{"repo-1"},
		Rows: []reducer.SemanticEntityRow{
			semanticNornicDBFunctionRow("function-go-1"),
		},
	})
	if err != nil {
		t.Fatalf("WriteSemanticEntities() error = %v", err)
	}
	if got, want := result.CanonicalWrites, 1; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}

	plans := semanticEntityPlans()
	if got, want := len(executor.calls), len(plans)+1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	for i, plan := range plans {
		stmt := executor.calls[i]
		if stmt.Operation != OperationCanonicalRetract {
			t.Fatalf("call[%d].Operation = %q, want %q", i, stmt.Operation, OperationCanonicalRetract)
		}
		if strings.Contains(stmt.Cypher, "|") {
			t.Fatalf("call[%d].Cypher = %q, want label-scoped retract without pipe labels", i, stmt.Cypher)
		}
		if !strings.Contains(stmt.Cypher, "MATCH (n:"+plan.label+")") {
			t.Fatalf("call[%d].Cypher = %q, want label %q", i, stmt.Cypher, plan.label)
		}
		if got, want := stmt.Parameters[StatementMetadataEntityLabelKey], plan.label; got != want {
			t.Fatalf("call[%d] label metadata = %#v, want %#v", i, got, want)
		}
	}

	upsert := executor.calls[len(plans)]
	if upsert.Operation != OperationCanonicalUpsert {
		t.Fatalf("last call Operation = %q, want %q", upsert.Operation, OperationCanonicalUpsert)
	}
	if got, want := upsert.Parameters[StatementMetadataEntityLabelKey], "Function"; got != want {
		t.Fatalf("upsert label metadata = %#v, want %#v", got, want)
	}
}

func TestSemanticEntityWriterSkipsRetractWhenRequested(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewSemanticEntityWriterWithBatchedProperties(executor, 100).WithLabelScopedRetract()

	result, err := writer.WriteSemanticEntities(context.Background(), reducer.SemanticEntityWrite{
		RepoIDs:     []string{"repo-1"},
		SkipRetract: true,
		Rows: []reducer.SemanticEntityRow{
			semanticNornicDBFunctionRow("function-go-1"),
		},
	})
	if err != nil {
		t.Fatalf("WriteSemanticEntities() error = %v", err)
	}
	if got, want := result.CanonicalWrites, 1; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	if got, want := executor.calls[0].Operation, OperationCanonicalUpsert; got != want {
		t.Fatalf("call[0].Operation = %q, want %q", got, want)
	}
}

func semanticNornicDBFunctionRow(id string) reducer.SemanticEntityRow {
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
