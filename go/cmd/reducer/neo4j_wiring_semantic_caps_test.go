package main

import (
	"context"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

func TestSemanticEntityWriterForGraphBackendAppliesDefaultNornicDBAnnotationCap(t *testing.T) {
	t.Parallel()

	executor := &recordingReducerStatementExecutor{}
	writer, err := semanticEntityWriterForGraphBackend(executor, 500, runtimecfg.GraphBackendNornicDB, func(string) string {
		return ""
	})
	if err != nil {
		t.Fatalf("semanticEntityWriterForGraphBackend() error = %v", err)
	}

	rows := make([]reducer.SemanticEntityRow, 0, 31)
	for i := 0; i < 31; i++ {
		rows = append(rows, semanticAnnotationRow(fmt.Sprintf("annotation-ts-%03d", i)))
	}
	result, err := writer.WriteSemanticEntities(context.Background(), reducer.SemanticEntityWrite{
		RepoIDs: []string{"repo-1"},
		Rows:    rows,
	})
	if err != nil {
		t.Fatalf("WriteSemanticEntities() error = %v", err)
	}
	if got, want := result.CanonicalWrites, 31; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}

	var annotationBatches []int
	for _, call := range executor.calls {
		if call.Operation != sourcecypher.OperationCanonicalUpsert {
			continue
		}
		label, _ := call.Parameters[sourcecypher.StatementMetadataEntityLabelKey].(string)
		if label != "Annotation" {
			continue
		}
		rows, _ := call.Parameters["rows"].([]map[string]any)
		annotationBatches = append(annotationBatches, len(rows))
	}
	if got, want := intsString(annotationBatches), "[5 5 5 5 5 5 1]"; got != want {
		t.Fatalf("Annotation batch sizes = %s, want %s", got, want)
	}
}

func TestSemanticEntityWriterForGraphBackendAppliesDefaultNornicDBTypeAliasCap(t *testing.T) {
	t.Parallel()

	executor := &recordingReducerStatementExecutor{}
	writer, err := semanticEntityWriterForGraphBackend(executor, 500, runtimecfg.GraphBackendNornicDB, func(string) string {
		return ""
	})
	if err != nil {
		t.Fatalf("semanticEntityWriterForGraphBackend() error = %v", err)
	}

	rows := make([]reducer.SemanticEntityRow, 0, 11)
	for i := 0; i < 11; i++ {
		rows = append(rows, semanticTypeAliasRow(fmt.Sprintf("type-alias-ts-%03d", i)))
	}
	result, err := writer.WriteSemanticEntities(context.Background(), reducer.SemanticEntityWrite{
		RepoIDs: []string{"repo-1"},
		Rows:    rows,
	})
	if err != nil {
		t.Fatalf("WriteSemanticEntities() error = %v", err)
	}
	if got, want := result.CanonicalWrites, 11; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}

	var typeAliasBatches []int
	for _, call := range executor.calls {
		if call.Operation != sourcecypher.OperationCanonicalUpsert {
			continue
		}
		label, _ := call.Parameters[sourcecypher.StatementMetadataEntityLabelKey].(string)
		if label != "TypeAlias" {
			continue
		}
		rows, _ := call.Parameters["rows"].([]map[string]any)
		typeAliasBatches = append(typeAliasBatches, len(rows))
	}
	if got, want := intsString(typeAliasBatches), "[5 5 1]"; got != want {
		t.Fatalf("TypeAlias batch sizes = %s, want %s", got, want)
	}
}

func TestSemanticEntityWriterForGraphBackendAppliesDefaultNornicDBTypeAnnotationCap(t *testing.T) {
	t.Parallel()

	executor := &recordingReducerStatementExecutor{}
	writer, err := semanticEntityWriterForGraphBackend(executor, 500, runtimecfg.GraphBackendNornicDB, func(string) string {
		return ""
	})
	if err != nil {
		t.Fatalf("semanticEntityWriterForGraphBackend() error = %v", err)
	}

	rows := make([]reducer.SemanticEntityRow, 0, 101)
	for i := 0; i < 101; i++ {
		rows = append(rows, semanticTypeAnnotationRow(fmt.Sprintf("type-annotation-py-%03d", i)))
	}
	result, err := writer.WriteSemanticEntities(context.Background(), reducer.SemanticEntityWrite{
		RepoIDs: []string{"repo-1"},
		Rows:    rows,
	})
	if err != nil {
		t.Fatalf("WriteSemanticEntities() error = %v", err)
	}
	if got, want := result.CanonicalWrites, 101; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}

	var typeAnnotationBatches []int
	for _, call := range executor.calls {
		if call.Operation != sourcecypher.OperationCanonicalUpsert {
			continue
		}
		label, _ := call.Parameters[sourcecypher.StatementMetadataEntityLabelKey].(string)
		if label != "TypeAnnotation" {
			continue
		}
		rows, _ := call.Parameters["rows"].([]map[string]any)
		typeAnnotationBatches = append(typeAnnotationBatches, len(rows))
	}
	if got, want := intsString(typeAnnotationBatches), "[50 50 1]"; got != want {
		t.Fatalf("TypeAnnotation batch sizes = %s, want %s", got, want)
	}
}

func TestSemanticEntityWriterForGraphBackendAppliesDefaultNornicDBFunctionCap(t *testing.T) {
	t.Parallel()

	executor := &recordingReducerStatementExecutor{}
	writer, err := semanticEntityWriterForGraphBackend(executor, 500, runtimecfg.GraphBackendNornicDB, func(string) string {
		return ""
	})
	if err != nil {
		t.Fatalf("semanticEntityWriterForGraphBackend() error = %v", err)
	}

	rows := make([]reducer.SemanticEntityRow, 0, 11)
	for i := 0; i < 11; i++ {
		rows = append(rows, semanticFunctionRow(fmt.Sprintf("function-go-%03d", i)))
	}
	result, err := writer.WriteSemanticEntities(context.Background(), reducer.SemanticEntityWrite{
		RepoIDs: []string{"repo-1"},
		Rows:    rows,
	})
	if err != nil {
		t.Fatalf("WriteSemanticEntities() error = %v", err)
	}
	if got, want := result.CanonicalWrites, 11; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}

	var functionBatches []int
	for _, call := range executor.calls {
		if call.Operation != sourcecypher.OperationCanonicalUpsert {
			continue
		}
		label, _ := call.Parameters[sourcecypher.StatementMetadataEntityLabelKey].(string)
		if label != "Function" {
			continue
		}
		rows, _ := call.Parameters["rows"].([]map[string]any)
		functionBatches = append(functionBatches, len(rows))
	}
	if got, want := intsString(functionBatches), "[10 1]"; got != want {
		t.Fatalf("Function batch sizes = %s, want %s", got, want)
	}
}

func semanticAnnotationRow(id string) reducer.SemanticEntityRow {
	return reducer.SemanticEntityRow{
		RepoID:       "repo-1",
		EntityID:     id,
		EntityType:   "Annotation",
		EntityName:   id,
		FilePath:     "/repo/main.ts",
		RelativePath: "main.ts",
		Language:     "typescript",
		StartLine:    1,
		EndLine:      2,
	}
}

func semanticTypeAliasRow(id string) reducer.SemanticEntityRow {
	return reducer.SemanticEntityRow{
		RepoID:       "repo-1",
		EntityID:     id,
		EntityType:   "TypeAlias",
		EntityName:   id,
		FilePath:     "/repo/types.ts",
		RelativePath: "types.ts",
		Language:     "typescript",
		StartLine:    1,
		EndLine:      1,
	}
}

func semanticTypeAnnotationRow(id string) reducer.SemanticEntityRow {
	return reducer.SemanticEntityRow{
		RepoID:       "repo-1",
		EntityID:     id,
		EntityType:   "TypeAnnotation",
		EntityName:   id,
		FilePath:     "/repo/types.py",
		RelativePath: "types.py",
		Language:     "python",
		StartLine:    1,
		EndLine:      1,
	}
}
