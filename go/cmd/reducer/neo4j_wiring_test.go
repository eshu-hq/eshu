package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

func TestNornicDBTuningDocSemanticDefaultsMatchCode(t *testing.T) {
	t.Parallel()

	doc := readNornicDBTuningDoc(t)
	gotDefault, ok := markdownTableDefault(doc, nornicDBSemanticEntityLabelBatchEnv)
	if !ok {
		t.Fatalf("nornicdb tuning doc missing %s", nornicDBSemanticEntityLabelBatchEnv)
	}
	wantDefault := formatSemanticLabelSizes(defaultNornicDBSemanticEntityLabelBatchSizes(0))
	if gotDefault != wantDefault {
		t.Fatalf("doc default for %s = %q, want %q", nornicDBSemanticEntityLabelBatchEnv, gotDefault, wantDefault)
	}
}

// fakeNeo4jSession records cypher calls for assertion.
type fakeNeo4jSession struct {
	calls []fakeCypherCall
	err   error
	errs  []error
}

type fakeCypherCall struct {
	Cypher     string
	Parameters map[string]any
}

func (s *fakeNeo4jSession) RunCypher(ctx context.Context, cypher string, params map[string]any) error {
	s.calls = append(s.calls, fakeCypherCall{Cypher: cypher, Parameters: params})
	if len(s.errs) > 0 {
		err := s.errs[0]
		s.errs = s.errs[1:]
		return err
	}
	return s.err
}

func (s *fakeNeo4jSession) RunCypherGroup(ctx context.Context, stmts []sourcecypher.Statement) error {
	for _, stmt := range stmts {
		if err := s.RunCypher(ctx, stmt.Cypher, stmt.Parameters); err != nil {
			return err
		}
	}
	return nil
}

func readNornicDBTuningDoc(t *testing.T) string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	docPath := filepath.Join(filepath.Dir(filename), "..", "..", "..", "docs", "docs", "reference", "nornicdb-tuning.md")
	contents, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("read nornicdb tuning doc: %v", err)
	}
	return string(contents)
}

func markdownTableDefault(markdown string, envName string) (string, bool) {
	prefix := "| `" + envName + "` |"
	for _, line := range strings.Split(markdown, "\n") {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		cells := strings.Split(line, "|")
		if len(cells) < 4 {
			return "", false
		}
		return normalizeMarkdownDefault(cells[2]), true
	}
	return "", false
}

func normalizeMarkdownDefault(defaultCell string) string {
	return strings.ReplaceAll(strings.TrimSpace(defaultCell), "`", "")
}

func formatSemanticLabelSizes(labelSizes map[string]int) string {
	labels := make([]string, 0, len(labelSizes))
	for label := range labelSizes {
		labels = append(labels, label)
	}
	slices.Sort(labels)

	var builder strings.Builder
	for i, label := range labels {
		if i > 0 {
			builder.WriteByte(',')
		}
		builder.WriteString(label)
		builder.WriteByte('=')
		fmt.Fprint(&builder, labelSizes[label])
	}
	return builder.String()
}

func TestReducerNeo4jExecutorExecutesStatement(t *testing.T) {
	t.Parallel()

	session := &fakeNeo4jSession{}
	executor := reducerNeo4jExecutor{session: session}

	stmt := sourcecypher.Statement{
		Operation:  sourcecypher.OperationCanonicalUpsert,
		Cypher:     "MERGE (w:Workload {id: $workload_id})",
		Parameters: map[string]any{"workload_id": "workload:my-api"},
	}

	err := executor.Execute(context.Background(), stmt)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(session.calls) != 1 {
		t.Fatalf("session calls = %d, want 1", len(session.calls))
	}
	if session.calls[0].Cypher != stmt.Cypher {
		t.Fatalf("cypher = %q, want %q", session.calls[0].Cypher, stmt.Cypher)
	}
	if session.calls[0].Parameters["workload_id"] != "workload:my-api" {
		t.Fatalf("workload_id = %v", session.calls[0].Parameters["workload_id"])
	}
}

func TestReducerNeo4jExecutorPropagatesError(t *testing.T) {
	t.Parallel()

	session := &fakeNeo4jSession{err: errors.New("neo4j timeout")}
	executor := reducerNeo4jExecutor{session: session}

	err := executor.Execute(context.Background(), sourcecypher.Statement{
		Cypher: "MERGE (w:Workload {id: $id})",
	})
	if err == nil {
		t.Fatal("Execute() error = nil, want non-nil")
	}
}

func TestReducerCypherExecutorExecutesCypher(t *testing.T) {
	t.Parallel()

	session := &fakeNeo4jSession{}
	executor := reducerCypherExecutor{session: session}

	err := executor.ExecuteCypher(context.Background(),
		"MERGE (w:Workload {id: $workload_id})",
		map[string]any{"workload_id": "workload:my-api"},
	)
	if err != nil {
		t.Fatalf("ExecuteCypher() error = %v", err)
	}
	if len(session.calls) != 1 {
		t.Fatalf("session calls = %d, want 1", len(session.calls))
	}
	if session.calls[0].Cypher != "MERGE (w:Workload {id: $workload_id})" {
		t.Fatalf("cypher = %q", session.calls[0].Cypher)
	}
}

func TestReducerCypherExecutorPropagatesError(t *testing.T) {
	t.Parallel()

	session := &fakeNeo4jSession{err: errors.New("connection refused")}
	executor := reducerCypherExecutor{session: session}

	err := executor.ExecuteCypher(context.Background(), "MERGE (w:Workload)", nil)
	if err == nil {
		t.Fatal("ExecuteCypher() error = nil, want non-nil")
	}
}

func TestReducerCypherExecutorRetriesTransientDeadlock(t *testing.T) {
	t.Parallel()

	session := &fakeNeo4jSession{
		errs: []error{
			errors.New("Neo4jError: Neo.TransientError.Transaction.DeadlockDetected (deadlock cycle)"),
			nil,
		},
	}
	executor := reducerCypherExecutor{session: session}

	err := executor.ExecuteCypher(context.Background(), "MERGE (w:Workload {id: $id})", map[string]any{"id": "workload:retry"})
	if err != nil {
		t.Fatalf("ExecuteCypher() error = %v, want nil after retry", err)
	}
	if got, want := len(session.calls), 2; got != want {
		t.Fatalf("session calls = %d, want %d", got, want)
	}
}

type groupCapableReducerExecutor struct {
	groupCalls int
}

func (e *groupCapableReducerExecutor) Execute(context.Context, sourcecypher.Statement) error {
	return nil
}

func (e *groupCapableReducerExecutor) ExecuteGroup(context.Context, []sourcecypher.Statement) error {
	e.groupCalls++
	return nil
}

type contextBlockingReducerExecutor struct{}

func (contextBlockingReducerExecutor) Execute(ctx context.Context, _ sourcecypher.Statement) error {
	<-ctx.Done()
	return ctx.Err()
}

func (contextBlockingReducerExecutor) ExecuteGroup(ctx context.Context, _ []sourcecypher.Statement) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestSemanticEntityExecutorForGraphBackendKeepsNeo4jGroupedExecutor(t *testing.T) {
	t.Parallel()

	executor := semanticEntityExecutorForGraphBackend(&groupCapableReducerExecutor{}, runtimecfg.GraphBackendNeo4j, 0, false)
	if _, ok := executor.(sourcecypher.GroupExecutor); !ok {
		t.Fatal("Neo4j semantic entity executor does not implement GroupExecutor")
	}
}

func TestSemanticEntityExecutorForGraphBackendHidesGroupExecutorForNornicDB(t *testing.T) {
	t.Parallel()

	inner := &groupCapableReducerExecutor{}
	executor := semanticEntityExecutorForGraphBackend(inner, runtimecfg.GraphBackendNornicDB, 0, false)
	if _, ok := executor.(sourcecypher.GroupExecutor); ok {
		t.Fatal("NornicDB semantic entity executor implements GroupExecutor, want execute-only surface")
	}
}

func TestSemanticEntityExecutorForGraphBackendPreservesGroupedWritesForConformance(t *testing.T) {
	t.Parallel()

	inner := &groupCapableReducerExecutor{}
	executor := semanticEntityExecutorForGraphBackend(inner, runtimecfg.GraphBackendNornicDB, 0, true)
	ge, ok := executor.(sourcecypher.GroupExecutor)
	if !ok {
		t.Fatal("NornicDB semantic entity executor does not implement GroupExecutor when conformance grouped writes are enabled")
	}
	if err := ge.ExecuteGroup(context.Background(), []sourcecypher.Statement{{Cypher: "RETURN 1"}}); err != nil {
		t.Fatalf("ExecuteGroup() error = %v, want nil", err)
	}
	if got, want := inner.groupCalls, 1; got != want {
		t.Fatalf("inner groupCalls = %d, want %d", got, want)
	}
}

func TestSemanticEntityExecutorForGraphBackendTimesOutGroupedWrites(t *testing.T) {
	t.Parallel()

	executor := semanticEntityExecutorForGraphBackend(contextBlockingReducerExecutor{}, runtimecfg.GraphBackendNornicDB, 10*time.Millisecond, true)
	ge, ok := executor.(sourcecypher.GroupExecutor)
	if !ok {
		t.Fatal("NornicDB grouped semantic entity executor does not implement GroupExecutor")
	}
	err := ge.ExecuteGroup(context.Background(), []sourcecypher.Statement{{Cypher: "RETURN 1"}})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ExecuteGroup() error = %v, want deadline exceeded", err)
	}
}

func TestReducerTransactionTimeoutOnlyAppliesToNornicDB(t *testing.T) {
	t.Parallel()

	getenv := func(key string) string {
		if key == "ESHU_CANONICAL_WRITE_TIMEOUT" {
			return "3s"
		}
		return ""
	}
	if got := reducerTransactionTimeout(runtimecfg.GraphBackendNeo4j, getenv); got != 0 {
		t.Fatalf("reducerTransactionTimeout(neo4j) = %s, want 0", got)
	}
	if got := reducerTransactionTimeout(runtimecfg.GraphBackendNornicDB, getenv); got != 3*time.Second {
		t.Fatalf("reducerTransactionTimeout(nornicdb) = %s, want 3s", got)
	}
}

func TestReducerNeo4jSessionRunnerTransactionConfigurersSetTimeout(t *testing.T) {
	t.Parallel()

	runner := neo4jSessionRunner{TxTimeout: 4 * time.Second}
	configurers := runner.transactionConfigurers()
	if len(configurers) != 1 {
		t.Fatalf("transactionConfigurers count = %d, want 1", len(configurers))
	}
	var config neo4jdriver.TransactionConfig
	configurers[0](&config)
	if got := config.Timeout; got != 4*time.Second {
		t.Fatalf("transaction timeout = %s, want 4s", got)
	}
}

func TestNornicDBSemanticObservedExecutorLogsStatementDuration(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(previous)

	inner := &recordingReducerStatementExecutor{}
	executor := nornicDBSemanticObservedExecutor{inner: inner}

	err := executor.Execute(context.Background(), sourcecypher.Statement{
		Operation: sourcecypher.OperationCanonicalUpsert,
		Cypher:    "MERGE (n:Module {uid: $id})",
		Parameters: map[string]any{
			"rows": []map[string]any{
				{"entity_id": "module-1"},
				{"entity_id": "module-2"},
			},
			sourcecypher.StatementMetadataEntityLabelKey: "Module",
			sourcecypher.StatementMetadataSummaryKey:     "semantic label=Module rows=2",
		},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got, want := len(inner.calls), 1; got != want {
		t.Fatalf("inner calls = %d, want %d", got, want)
	}
	logText := logs.String()
	for _, want := range []string{
		"nornicdb semantic statement completed",
		"graph_backend",
		"nornicdb",
		"label",
		"Module",
		"rows",
		"2",
		"semantic label=Module rows=2",
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("semantic statement log missing %q:\n%s", want, logText)
		}
	}
}
