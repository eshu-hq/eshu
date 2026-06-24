// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestSemanticEntityWriterScopesDeltaRetractToFilePaths(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewSemanticEntityWriterWithCanonicalNodeRows(executor, 100)

	_, err := writer.WriteSemanticEntities(context.Background(), reducer.SemanticEntityWrite{
		RepoIDs:         []string{"repo-1"},
		DeltaProjection: true,
		DeltaFilePaths:  []string{"/repo/src/changed.go", "/repo/src/deleted.go"},
		Rows: []reducer.SemanticEntityRow{
			semanticNornicDBFunctionRow("function-go-1"),
		},
	})
	if err != nil {
		t.Fatalf("WriteSemanticEntities() error = %v", err)
	}
	if len(executor.calls) == 0 {
		t.Fatal("executor calls = 0, want scoped retract statements before upserts")
	}

	retract := executor.calls[0]
	if retract.Operation != OperationCanonicalRetract {
		t.Fatalf("first operation = %q, want %q", retract.Operation, OperationCanonicalRetract)
	}
	if strings.Contains(retract.Cypher, "n.repo_id IN $repo_ids") {
		t.Fatalf("retract cypher = %q, want no repo-wide predicate for delta projection", retract.Cypher)
	}
	if !strings.Contains(retract.Cypher, "n.path IN $file_paths") {
		t.Fatalf("retract cypher = %q, want file-scoped predicate", retract.Cypher)
	}
	filePaths, ok := retract.Parameters["file_paths"].([]string)
	if !ok {
		t.Fatalf("file_paths type = %T, want []string", retract.Parameters["file_paths"])
	}
	if got, want := filePaths, []string{"/repo/src/changed.go", "/repo/src/deleted.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("file_paths = %#v, want %#v", got, want)
	}
	if _, ok := retract.Parameters["repo_ids"]; ok {
		t.Fatalf("repo_ids present in scoped delta retract parameters: %#v", retract.Parameters["repo_ids"])
	}
}

func TestSemanticEntityWriterRejectsDeltaRetractWithoutFilePaths(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewSemanticEntityWriter(executor, 100)

	_, err := writer.WriteSemanticEntities(context.Background(), reducer.SemanticEntityWrite{
		RepoIDs:         []string{"repo-1"},
		DeltaProjection: true,
	})
	if err == nil {
		t.Fatal("WriteSemanticEntities() error = nil, want malformed delta projection error")
	}
	if got, want := err.Error(), "semantic entity delta projection requires file paths"; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
	if got, want := len(executor.calls), 0; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
}
