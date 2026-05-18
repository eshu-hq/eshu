package graph

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
)

func TestEnsureSchemaReturnsContextDeadline(t *testing.T) {
	t.Parallel()

	executor := &schemaRecordingExecutor{
		failErr: context.DeadlineExceeded,
		failOn:  func(_ string) bool { return true },
	}

	err := EnsureSchema(context.Background(), executor, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("EnsureSchema() error = %v, want context deadline exceeded", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("executor calls = %d, want fail-fast after first deadline", len(executor.calls))
	}
}

func TestEnsureSchemaLogsStatementProgress(t *testing.T) {
	t.Parallel()

	var output strings.Builder
	logger := slog.New(slog.NewTextHandler(io.Writer(&output), nil))
	executor := &schemaRecordingExecutor{}

	err := EnsureSchemaWithBackend(context.Background(), executor, logger, SchemaBackendNornicDB)
	if err != nil {
		t.Fatalf("EnsureSchemaWithBackend() error = %v, want nil", err)
	}

	logs := output.String()
	for _, want := range []string{
		"graph schema statement applying",
		"graph schema statement applied",
		"statement_index=1",
		"statement_total=",
		"graph_backend=nornicdb",
		"schema_statement=",
	} {
		if !strings.Contains(logs, want) {
			t.Fatalf("schema logs missing %q:\n%s", want, logs)
		}
	}
}
