package graph

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"regexp"
	"strconv"
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

func TestEnsureSchemaStatementTotalIncludesFulltextFallback(t *testing.T) {
	t.Parallel()

	var output strings.Builder
	logger := slog.New(slog.NewTextHandler(io.Writer(&output), nil))
	executor := &schemaRecordingExecutor{
		failOn: func(cypher string) bool {
			return strings.Contains(cypher, "db.index.fulltext.createNodeIndex")
		},
	}

	err := EnsureSchema(context.Background(), executor, logger)
	if err != nil {
		t.Fatalf("EnsureSchema() error = %v, want nil", err)
	}

	logs := output.String()
	progressPattern := regexp.MustCompile(`statement_index=([0-9]+).*statement_total=([0-9]+)`)
	matches := progressPattern.FindAllStringSubmatch(logs, -1)
	if len(matches) == 0 {
		t.Fatalf("schema logs missing statement progress:\n%s", logs)
	}
	for _, match := range matches {
		index, err := strconv.Atoi(match[1])
		if err != nil {
			t.Fatalf("parse statement_index %q: %v", match[1], err)
		}
		total, err := strconv.Atoi(match[2])
		if err != nil {
			t.Fatalf("parse statement_total %q: %v", match[2], err)
		}
		if index > total {
			t.Fatalf("statement progress exceeded total: index=%d total=%d\n%s", index, total, logs)
		}
	}
}
