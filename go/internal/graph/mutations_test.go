package graph

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestDeleteFileFromGraphExecutesCascade(t *testing.T) {
	t.Parallel()

	executor := &mutationRecordingExecutor{}
	err := DeleteFileFromGraph(context.Background(), executor, "/repo/src/main.go")
	if err != nil {
		t.Fatalf("DeleteFileFromGraph() error = %v, want nil", err)
	}

	if got, want := len(executor.calls), 2; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}

	// First call: delete file and elements
	if !strings.Contains(executor.calls[0].Cypher, "DETACH DELETE f, element") {
		t.Fatalf("first call missing DETACH DELETE: %s", executor.calls[0].Cypher)
	}
	if executor.calls[0].Parameters["file_path"] != "/repo/src/main.go" {
		t.Fatalf("file_path = %v, want /repo/src/main.go", executor.calls[0].Parameters["file_path"])
	}

	// Second call: prune orphaned directories
	if !strings.Contains(executor.calls[1].Cypher, "Directory") {
		t.Fatalf("second call missing Directory prune: %s", executor.calls[1].Cypher)
	}
}

func TestDeleteFileFromGraphRejectsEmptyPath(t *testing.T) {
	t.Parallel()

	executor := &mutationRecordingExecutor{}
	err := DeleteFileFromGraph(context.Background(), executor, "  ")
	if err == nil {
		t.Fatal("error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "file path must not be empty") {
		t.Fatalf("error = %q, want 'file path must not be empty'", err.Error())
	}
}

func TestDeleteFileFromGraphRequiresExecutor(t *testing.T) {
	t.Parallel()

	err := DeleteFileFromGraph(context.Background(), nil, "/repo/main.go")
	if err == nil {
		t.Fatal("error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "executor is required") {
		t.Fatalf("error = %q, want 'executor is required'", err.Error())
	}
}

func TestDeleteFileFromGraphPropagatesError(t *testing.T) {
	t.Parallel()

	executor := &mutationRecordingExecutor{errAtCall: errors.New("connection lost")}
	err := DeleteFileFromGraph(context.Background(), executor, "/repo/main.go")
	if err == nil {
		t.Fatal("error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "connection lost") {
		t.Fatalf("error = %q, want propagated error", err.Error())
	}
}

func TestDeleteRepositoryFromGraphExecutesCascade(t *testing.T) {
	t.Parallel()

	executor := &mutationRecordingExecutor{}
	deleted, err := DeleteRepositoryFromGraph(context.Background(), executor, "repository:org/my-repo")
	if err != nil {
		t.Fatalf("DeleteRepositoryFromGraph() error = %v, want nil", err)
	}
	if !deleted {
		t.Fatal("deleted = false, want true")
	}

	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	if !strings.Contains(executor.calls[0].Cypher, "DETACH DELETE r, e") {
		t.Fatalf("cypher missing cascade delete: %s", executor.calls[0].Cypher)
	}

	lookupValues, ok := executor.calls[0].Parameters["lookup_values"].([]string)
	if !ok {
		t.Fatalf("lookup_values type = %T, want []string", executor.calls[0].Parameters["lookup_values"])
	}
	if len(lookupValues) != 1 || lookupValues[0] != "repository:org/my-repo" {
		t.Fatalf("lookup_values = %v, want [repository:org/my-repo]", lookupValues)
	}
}

func TestDeleteRepositoryFromGraphRejectsEmptyIdentifier(t *testing.T) {
	t.Parallel()

	executor := &mutationRecordingExecutor{}
	_, err := DeleteRepositoryFromGraph(context.Background(), executor, "")
	if err == nil {
		t.Fatal("error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "repository identifier must not be empty") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestDeleteRepositoryFromGraphRequiresExecutor(t *testing.T) {
	t.Parallel()

	_, err := DeleteRepositoryFromGraph(context.Background(), nil, "repository:org/repo")
	if err == nil {
		t.Fatal("error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "executor is required") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestResetRepositorySubtreeInGraphExecutesTwoPhase(t *testing.T) {
	t.Parallel()

	executor := &mutationRecordingExecutor{}
	reset, err := ResetRepositorySubtreeInGraph(context.Background(), executor, "/path/to/repo")
	if err != nil {
		t.Fatalf("ResetRepositorySubtreeInGraph() error = %v, want nil", err)
	}
	if !reset {
		t.Fatal("reset = false, want true")
	}

	if got, want := len(executor.calls), 2; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}

	// First call: delete owned subtree nodes
	if !strings.Contains(executor.calls[0].Cypher, "DETACH DELETE owned") {
		t.Fatalf("first call missing owned subtree delete: %s", executor.calls[0].Cypher)
	}

	// Second call: delete remaining relationships
	if !strings.Contains(executor.calls[1].Cypher, "DELETE rel") {
		t.Fatalf("second call missing relationship delete: %s", executor.calls[1].Cypher)
	}
}

func TestResetRepositorySubtreeInGraphRejectsEmptyIdentifier(t *testing.T) {
	t.Parallel()

	executor := &mutationRecordingExecutor{}
	_, err := ResetRepositorySubtreeInGraph(context.Background(), executor, "  ")
	if err == nil {
		t.Fatal("error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "repository identifier must not be empty") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestResetRepositorySubtreeInGraphRequiresExecutor(t *testing.T) {
	t.Parallel()

	_, err := ResetRepositorySubtreeInGraph(context.Background(), nil, "repository:org/repo")
	if err == nil {
		t.Fatal("error = nil, want non-nil")
	}
}

func TestResetRepositorySubtreeInGraphPropagatesError(t *testing.T) {
	t.Parallel()

	executor := &mutationRecordingExecutor{errAtCall: errors.New("timeout")}
	_, err := ResetRepositorySubtreeInGraph(context.Background(), executor, "/path/to/repo")
	if err == nil {
		t.Fatal("error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("error = %q, want timeout propagated", err.Error())
	}
}

func TestRepositoryLookupValuesCanonicalID(t *testing.T) {
	t.Parallel()

	values := repositoryLookupValues("repository:org/my-repo")
	if len(values) != 1 {
		t.Fatalf("len = %d, want 1", len(values))
	}
	if values[0] != "repository:org/my-repo" {
		t.Fatalf("values[0] = %q", values[0])
	}
}

func TestRepositoryLookupValuesRawPath(t *testing.T) {
	t.Parallel()

	values := repositoryLookupValues("/home/user/repos/my-repo")
	if len(values) != 1 {
		t.Fatalf("len = %d, want 1", len(values))
	}
	if values[0] != "/home/user/repos/my-repo" {
		t.Fatalf("values[0] = %q", values[0])
	}
}

func TestRepositoryLookupValuesEmpty(t *testing.T) {
	t.Parallel()

	values := repositoryLookupValues("")
	if len(values) != 0 {
		t.Fatalf("len = %d, want 0", len(values))
	}
}

type mutationRecordingExecutor struct {
	calls     []CypherStatement
	errAtCall error
}

func (r *mutationRecordingExecutor) ExecuteCypher(_ context.Context, stmt CypherStatement) error {
	r.calls = append(r.calls, stmt)
	if r.errAtCall != nil {
		return r.errAtCall
	}
	return nil
}
