// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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

// fakeNeo4jSession records cypher calls for assertion. probeFound/probeErr
// script QueryCypherExists (#5998 rationale retract probe guard); probeCalls
// records every probe invocation separately from calls (RunCypher/DELETE) so
// a test can assert the probe ran without also asserting a delete happened.
type fakeNeo4jSession struct {
	calls      []fakeCypherCall
	err        error
	errs       []error
	probeFound bool
	probeErr   error
	probeCalls []fakeCypherCall
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

// QueryCypherExists implements the cypherProber interface
// (reducer_executor_adapters.go) so fakeNeo4jSession can stand in for
// neo4jSessionRunner in ExecuteProbe tests.
func (s *fakeNeo4jSession) QueryCypherExists(_ context.Context, cypher string, params map[string]any) (bool, error) {
	s.probeCalls = append(s.probeCalls, fakeCypherCall{Cypher: cypher, Parameters: params})
	return s.probeFound, s.probeErr
}

func readNornicDBTuningDoc(t *testing.T) string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	docPath := filepath.Join(filepath.Dir(filename), "..", "..", "..", "docs", "public", "reference", "nornicdb-tuning.md")
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
	executor := newReducerNeo4jExecutor(session, nil)

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
	executor := newReducerNeo4jExecutor(session, nil)

	err := executor.Execute(context.Background(), sourcecypher.Statement{
		Cypher: "MERGE (w:Workload {id: $id})",
	})
	if err == nil {
		t.Fatal("Execute() error = nil, want non-nil")
	}
}

func TestReducerNeo4jExecutorRetriesMergeGroupCommitConflict(t *testing.T) {
	t.Parallel()

	session := &fakeNeo4jSession{errs: []error{
		errors.New("Neo4jError: Neo.ClientError.Transaction.TransactionCommitFailed " +
			"(commit failed: constraint violation: Constraint violation " +
			"(UNIQUE on Environment.[name]): Node with name=production already exists)"),
		nil,
	}}
	executor := newReducerNeo4jExecutor(session, nil)
	statements := []sourcecypher.Statement{{
		Operation: sourcecypher.OperationCanonicalUpsert,
		Cypher:    "UNWIND $rows AS row MERGE (e:Environment {name: row.name}) SET e.uid = row.uid",
	}}

	if err := executor.ExecuteGroup(context.Background(), statements); err != nil {
		t.Fatalf("ExecuteGroup() error = %v, want nil after retry", err)
	}
	if got, want := len(session.calls), 2; got != want {
		t.Fatalf("session group attempts = %d, want %d", got, want)
	}
}

func TestReducerCypherExecutorExecutesCypher(t *testing.T) {
	t.Parallel()

	session := &fakeNeo4jSession{}
	executor := newReducerCypherExecutor(session, nil)

	err := executor.ExecuteCypher(
		context.Background(),
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
	executor := newReducerCypherExecutor(session, nil)

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
	executor := newReducerCypherExecutor(session, nil)

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

// TestQueryCypherExistsPassesTransactionConfigurers is a source-level
// regression for review F8: neo4jSessionRunner.QueryCypherExists' session.Run
// call previously omitted r.transactionConfigurers()..., unlike RunCypher and
// RunCypherGroup, so a probe or CanonicalNodeChecker read had no deadline on
// either backend even when ESHU_CANONICAL_WRITE_TIMEOUT bounds every other
// reducer graph write. neo4jdriver.SessionWithContext (the neo4j-go-driver/v5
// package) carries an unexported method (lastBookmark), so it cannot be
// implemented by a fake outside that package -- the same reason RunCypher and
// RunCypherGroup have no unit-level "was the configurer actually passed to
// session.Run" test either, only the live-backend suite
// (repo_dependency_*_prove_theory_live_test.go). This test proves the same
// property the only way available at the unit level: read this package's own
// source and assert QueryCypherExists' body calls r.transactionConfigurers().
func TestQueryCypherExistsPassesTransactionConfigurers(t *testing.T) {
	t.Parallel()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	sourcePath := filepath.Join(filepath.Dir(filename), "neo4j_wiring.go")
	source, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("read %s: %v", sourcePath, err)
	}

	body, ok := extractFuncBody(string(source), "func (r neo4jSessionRunner) QueryCypherExists(")
	if !ok {
		t.Fatal("QueryCypherExists function body not found in neo4j_wiring.go")
	}
	if !strings.Contains(body, "r.transactionConfigurers()") {
		t.Fatalf("QueryCypherExists does not call r.transactionConfigurers() -- probe/pre-flight reads would have no deadline:\n%s", body)
	}
}

// TestQueryCypherExistsUsesAccessModeWrite guards the correctness choice the
// in-source comment right above the session.Run call documents (#6165 review
// F2): both callers of QueryCypherExists -- the rationale retract probe guard
// and CanonicalNodeChecker -- read in order to decide whether to mutate, and
// neither session shares a BookmarkManager with the write session that
// follows it. On a routed Neo4j deployment, an AccessModeRead session can be
// served by a lagging follower and report zero rows for edges already
// committed on the leader. For the probe guard that lands on exactly the
// outcome the whole design exists to prevent: the guard skips the DELETE and
// records an ordinary `skipped`, indistinguishable from a correct skip, while
// stale edges are left behind. AccessModeWrite keeps the read on the leader.
// If a future edit ever "simplifies" this to AccessModeRead because the
// statement itself is read-only, this test goes RED.
func TestQueryCypherExistsUsesAccessModeWrite(t *testing.T) {
	t.Parallel()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	sourcePath := filepath.Join(filepath.Dir(filename), "neo4j_wiring.go")
	source, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("read %s: %v", sourcePath, err)
	}

	body, ok := extractFuncBody(string(source), "func (r neo4jSessionRunner) QueryCypherExists(")
	if !ok {
		t.Fatal("QueryCypherExists function body not found in neo4j_wiring.go")
	}
	if !strings.Contains(body, "neo4jdriver.AccessModeWrite") {
		t.Fatalf("QueryCypherExists does not specify neo4jdriver.AccessModeWrite -- a routed Neo4j read-mode session for this probe/pre-flight read can be served by a lagging follower and report zero rows for edges already committed on the leader, which for the rationale retract probe guard means a skipped DELETE recorded as an ordinary `skipped` while stale edges are left behind:\n%s", body)
	}
	if strings.Contains(body, "neo4jdriver.AccessModeRead") {
		t.Fatalf("QueryCypherExists specifies neo4jdriver.AccessModeRead, want AccessModeWrite (see the correctness-choice comment above session.Run in neo4j_wiring.go):\n%s", body)
	}
}

// extractFuncBody returns the source text from signature (a unique function
// signature prefix) up to the next top-level "\nfunc " boundary, or ok=false
// if signature is not found. It is a crude brace-agnostic slice, sufficient
// for asserting a call appears inside one specific function without pulling
// in go/ast for a single regression check.
func extractFuncBody(source, signature string) (string, bool) {
	start := strings.Index(source, signature)
	if start == -1 {
		return "", false
	}
	rest := source[start+len(signature):]
	end := strings.Index(rest, "\nfunc ")
	if end == -1 {
		return rest, true
	}
	return rest[:end], true
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
