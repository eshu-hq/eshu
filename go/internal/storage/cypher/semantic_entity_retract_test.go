package cypher

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestSemanticEntityWriterRetractsWithoutUpserts(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewSemanticEntityWriter(executor, 0)

	result, err := writer.WriteSemanticEntities(context.Background(), reducer.SemanticEntityWrite{
		RepoIDs: []string{"repo-1", "repo-2"},
	})
	if err != nil {
		t.Fatalf("WriteSemanticEntities() error = %v", err)
	}
	if got, want := result.CanonicalWrites, 0; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	if executor.calls[0].Operation != OperationCanonicalRetract {
		t.Fatalf("call[0].Operation = %q, want %q", executor.calls[0].Operation, OperationCanonicalRetract)
	}
	repoIDs, ok := executor.calls[0].Parameters["repo_ids"].([]string)
	if !ok {
		t.Fatalf("repo_ids type = %T, want []string", executor.calls[0].Parameters["repo_ids"])
	}
	if got, want := len(repoIDs), 2; got != want {
		t.Fatalf("repo_ids length = %d, want %d", got, want)
	}
}
