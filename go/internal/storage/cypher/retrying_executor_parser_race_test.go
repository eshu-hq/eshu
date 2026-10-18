// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/storage/cypher/canonical/node"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// parserRaceError builds the Neo4j wire error for a false "Variable not
// defined" report at character offset off of cypher, with name as the
// (possibly corrupted) token text. Line and column are derived from the
// statement the way Neo4j derives them, so a fixture never hand-copies a
// position. The statements under test are ASCII, so bytes equal characters.
func parserRaceError(cypher string, off int, name string) error {
	before := cypher[:off]
	line := 1 + strings.Count(before, "\n")
	lineStart := strings.LastIndex(before, "\n") + 1
	col := off - lineStart + 1
	lineEnd := len(cypher)
	if i := strings.Index(cypher[off:], "\n"); i >= 0 {
		lineEnd = off + i
	}
	return &neo4jdriver.Neo4jError{
		Code: nornicDBStatementSyntaxErrorCode,
		Msg: fmt.Sprintf("Variable `%s` not defined (line %d, column %d (offset: %d))\n\"%s\"\n%s^",
			name, line, col, off, cypher[lineStart:lineEnd], strings.Repeat(" ", col)),
	}
}

func fileStatement(cypher string) Statement {
	return Statement{
		Operation: OperationCanonicalUpsert,
		Cypher:    cypher,
		Parameters: map[string]any{
			"rows":                      []map[string]any{},
			StatementMetadataSummaryKey: "files rows=0",
		},
	}
}

// TestParserIntervalRaceMatchesIssue8190Shapes pins the two production
// failures reported in #8190 and the corruption variants the upstream ANTLR
// Interval.of cache race (antlr/antlr4#4901) can produce.
func TestParserIntervalRaceMatchesIssue8190Shapes(t *testing.T) {
	t.Parallel()

	root := node.RootFileCreateMissingCypher
	nested := node.FileCreateMissingCypher
	if root[266] != 'f' || nested[245] != 'f' {
		t.Fatalf("fixture drift: root[266]=%q nested[245]=%q, want the reported 'f' tokens", root[266], nested[245])
	}
	tests := []struct {
		name   string
		cypher string
		off    int
		report string
	}{
		{"issue root offset 266 char0", root, 266, "U"},
		{"issue nested offset 245 char0", nested, 245, "U"},
		{"defining site f", root, 266, "f"},
		{"defining site f nested", nested, 245, "f"},
		{"prefix of query", root, 266, root[:266]},
		{"short prefix of query", root, 266, root[:40]},
		{"empty name from a=N,b=0", root, 266, ""},
		{"leading space shifts char0", " " + root, 267, " "},
		{"leading comment shifts char0", "// x\n" + root, 271, "/"},
		{"leading space defining site", " " + root, 267, "f"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := parserRaceError(tc.cypher, tc.off, tc.report)
			stmt := fileStatement(tc.cypher)
			if got := classifyRetryableGraphWriteError(err, stmt); got != "neo4j_parser_interval_race" {
				t.Errorf("single reason = %q, want neo4j_parser_interval_race", got)
			}
			if got := classifyRetryableGraphWriteGroupError(err, []Statement{stmt}); got != "neo4j_parser_interval_race" {
				t.Errorf("group reason = %q, want neo4j_parser_interval_race", got)
			}
		})
	}
}

// TestParserIntervalRaceGroupMatchesAnyStatement proves the offset is matched
// against the statement it belongs to, not only the first of a group.
func TestParserIntervalRaceGroupMatchesAnyStatement(t *testing.T) {
	t.Parallel()

	other := fileStatement(node.FileFirstGenerationMergeCypher)
	target := fileStatement(node.RootFileCreateMissingCypher)
	err := parserRaceError(target.Cypher, 266, "U")
	if got := classifyRetryableGraphWriteGroupError(err, []Statement{other, target}); got != "neo4j_parser_interval_race" {
		t.Fatalf("group reason = %q, want neo4j_parser_interval_race", got)
	}
}

// TestParserIntervalRaceStaysNarrow keeps a real undefined-variable bug and
// every non-matching shape terminal.
func TestParserIntervalRaceStaysNarrow(t *testing.T) {
	t.Parallel()

	root := node.RootFileCreateMissingCypher
	rowOff := strings.Index(root, "row.path") // 'r' of a three-char identifier
	farCypher := "UNWIND $rows AS row\n" + strings.Repeat(" ", 1100) + "MERGE (f:File {path: row.path})\nSET f.x = 1"
	farOff := strings.Index(farCypher, "f:File")

	tests := []struct {
		name  string
		err   error
		stmts []Statement
	}{
		{"multi-char name", parserRaceError(root, 266, "foo"), []Statement{fileStatement(root)}},
		{"offset on a multi-char identifier", parserRaceError(root, rowOff, "U"), []Statement{fileStatement(root)}},
		{"name not a prefix and not the identifier", parserRaceError(root, 266, "g"), []Statement{fileStatement(root)}},
		{"name is a non-prefix substring", parserRaceError(root, 266, root[1:40]), []Statement{fileStatement(root)}},
		{"offset past the 1000 cache bound", parserRaceError(farCypher, farOff, "U"), []Statement{fileStatement(farCypher)}},
		{"offset past statement end", &neo4jdriver.Neo4jError{
			Code: nornicDBStatementSyntaxErrorCode,
			Msg:  "Variable `U` not defined (line 9, column 5 (offset: 99999))",
		}, []Statement{fileStatement(root)}},
		{"line inconsistent with offset", &neo4jdriver.Neo4jError{
			Code: nornicDBStatementSyntaxErrorCode,
			Msg:  "Variable `U` not defined (line 2, column 5 (offset: 266))",
		}, []Statement{fileStatement(root)}},
		{"column inconsistent with offset", &neo4jdriver.Neo4jError{
			Code: nornicDBStatementSyntaxErrorCode,
			Msg:  "Variable `U` not defined (line 9, column 6 (offset: 266))",
		}, []Statement{fileStatement(root)}},
		{
			"offset matches no statement of the group",
			parserRaceError(root, 266, "U"),
			[]Statement{fileStatement(node.FileFirstGenerationMergeCypher)},
		},
		{"other error code", &neo4jdriver.Neo4jError{
			Code: "Neo.ClientError.Statement.SemanticError",
			Msg:  parserRaceError(root, 266, "U").(*neo4jdriver.Neo4jError).Msg,
		}, []Statement{fileStatement(root)}},
		{"different syntax message", &neo4jdriver.Neo4jError{
			Code: nornicDBStatementSyntaxErrorCode,
			Msg:  "Invalid input 'BROKEN': expected a graph pattern (line 9, column 5 (offset: 266))",
		}, []Statement{fileStatement(root)}},
		{
			"plain error without a Neo4jError", errors.New("Variable `U` not defined (line 9, column 5 (offset: 266))"),
			[]Statement{fileStatement(root)},
		},
		{"overflowing offset", &neo4jdriver.Neo4jError{
			Code: nornicDBStatementSyntaxErrorCode,
			Msg:  "Variable `U` not defined (line 9, column 5 (offset: 99999999999999999999999))",
		}, []Statement{fileStatement(root)}},
		{"nil error", nil, []Statement{fileStatement(root)}},
		{"empty group", parserRaceError(root, 266, "U"), nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if len(tc.stmts) == 1 {
				if got := classifyRetryableGraphWriteError(tc.err, tc.stmts[0]); got != "" {
					t.Errorf("single reason = %q, want terminal", got)
				}
			}
			if got := classifyRetryableGraphWriteGroupError(tc.err, tc.stmts); got != "" {
				t.Errorf("group reason = %q, want terminal", got)
			}
		})
	}
}

// TestParserIntervalRaceLeavesNornicDBClassificationUnchanged proves the
// existing NornicDB SyntaxError exceptions keep their reasons.
func TestParserIntervalRaceLeavesNornicDBClassificationUnchanged(t *testing.T) {
	t.Parallel()

	merge := Statement{Operation: OperationCanonicalUpsert, Cypher: "MERGE (a)-[r:R]->(b)"}
	snapshot := &neo4jdriver.Neo4jError{
		Code: nornicDBStatementSyntaxErrorCode,
		Msg:  nornicDBRelationshipSnapshotConflictMessage,
	}
	if got := classifyRetryableGraphWriteError(snapshot, merge); got != graphWriteRetryReasonWriteConflict {
		t.Errorf("snapshot single reason = %q, want %q", got, graphWriteRetryReasonWriteConflict)
	}
	if got := classifyRetryableGraphWriteGroupError(snapshot, []Statement{merge}); got != graphWriteRetryReasonWriteConflict {
		t.Errorf("snapshot group reason = %q, want %q", got, graphWriteRetryReasonWriteConflict)
	}
	unique := &neo4jdriver.Neo4jError{
		Code: nornicDBStatementSyntaxErrorCode,
		Msg:  nornicDBPlatformCommitUniqueConflictMessage,
	}
	if got := classifyRetryableGraphWriteGroupError(unique, []Statement{merge}); got != graphWriteRetryReasonUniqueConflict {
		t.Errorf("unique group reason = %q, want %q", got, graphWriteRetryReasonUniqueConflict)
	}
	plain := &neo4jdriver.Neo4jError{Code: nornicDBStatementSyntaxErrorCode, Msg: "Invalid input 'X'"}
	if got := classifyRetryableGraphWriteGroupError(plain, []Statement{merge}); got != "" {
		t.Errorf("plain syntax group reason = %q, want terminal", got)
	}
}

type parserRaceGroupExecutor struct {
	calls   atomic.Int32
	failFor int32
	err     error
}

func (e *parserRaceGroupExecutor) Execute(context.Context, Statement) error {
	if e.calls.Add(1) <= e.failFor {
		return e.err
	}
	return nil
}

func (e *parserRaceGroupExecutor) ExecuteGroup(context.Context, []Statement) error {
	if e.calls.Add(1) <= e.failFor {
		return e.err
	}
	return nil
}

func newParserRaceInstruments(t *testing.T) (*telemetry.Instruments, *metric.ManualReader) {
	t.Helper()
	reader := metric.NewManualReader()
	inst, err := telemetry.NewInstruments(metric.NewMeterProvider(metric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	return inst, reader
}

func retryCounterReasons(t *testing.T, reader metric.Reader) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("reader.Collect() error = %v", err)
	}
	reasons := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok || m.Name != "eshu_dp_neo4j_deadlock_retries_total" {
				continue
			}
			for _, dp := range sum.DataPoints {
				v, _ := dp.Attributes.Value(telemetry.MetricDimensionReason)
				reasons[v.AsString()] += dp.Value
			}
		}
	}
	return reasons
}

// TestRetryingExecutorRecoversFromParserIntervalRace drives the production
// retry loop: the first attempt fails with the #8190 error, the replay
// succeeds, and the retry counter carries the new bounded reason.
func TestRetryingExecutorRecoversFromParserIntervalRace(t *testing.T) {
	t.Parallel()

	stmt := fileStatement(node.RootFileCreateMissingCypher)
	inner := &parserRaceGroupExecutor{failFor: 1, err: parserRaceError(stmt.Cypher, 266, "U")}
	inst, reader := newParserRaceInstruments(t)
	retrying := &RetryingExecutor{Inner: inner, MaxRetries: 3, BaseDelay: time.Millisecond, Instruments: inst}

	if err := retrying.ExecuteGroup(context.Background(), []Statement{stmt}); err != nil {
		t.Fatalf("ExecuteGroup() error = %v, want nil after replay", err)
	}
	if got := inner.calls.Load(); got != 2 {
		t.Fatalf("calls = %d, want 2", got)
	}
	if got := retryCounterReasons(t, reader)["neo4j_parser_interval_race"]; got != 1 {
		t.Fatalf("retry counter neo4j_parser_interval_race = %d, want 1 (all=%v)", got, retryCounterReasons(t, reader))
	}

	single := &parserRaceGroupExecutor{failFor: 1, err: parserRaceError(stmt.Cypher, 266, "U")}
	if err := (&RetryingExecutor{Inner: single, BaseDelay: time.Millisecond}).Execute(context.Background(), stmt); err != nil {
		t.Fatalf("Execute() error = %v, want nil after replay", err)
	}
	if got := single.calls.Load(); got != 2 {
		t.Fatalf("Execute calls = %d, want 2", got)
	}
}

// TestRetryingExecutorParserIntervalRaceIsBounded proves a persistent match
// stops at the retry budget and a real undefined variable never retries.
func TestRetryingExecutorParserIntervalRaceIsBounded(t *testing.T) {
	t.Parallel()

	stmt := fileStatement(node.RootFileCreateMissingCypher)
	persistent := &parserRaceGroupExecutor{failFor: 1 << 20, err: parserRaceError(stmt.Cypher, 266, "U")}
	err := (&RetryingExecutor{Inner: persistent, MaxRetries: 2, BaseDelay: time.Millisecond}).
		ExecuteGroup(context.Background(), []Statement{stmt})
	if err == nil {
		t.Fatal("ExecuteGroup() error = nil, want exhausted retry error")
	}
	if got := persistent.calls.Load(); got != 3 {
		t.Fatalf("calls = %d, want 3 (1 attempt + MaxRetries 2)", got)
	}
	assertParserRaceExhaustedErrorIsTerminal(t, err)

	real := &parserRaceGroupExecutor{failFor: 1 << 20, err: parserRaceError(stmt.Cypher, 266, "foo")}
	err = (&RetryingExecutor{Inner: real, MaxRetries: 3, BaseDelay: time.Millisecond}).
		ExecuteGroup(context.Background(), []Statement{stmt})
	if err == nil {
		t.Fatal("ExecuteGroup() error = nil, want terminal error")
	}
	if got := real.calls.Load(); got != 1 {
		t.Fatalf("real undefined variable calls = %d, want 1 (terminal)", got)
	}
}

// TestRetryingExecutorParserIntervalRaceLogsWarn proves the operator log
// carries phase, summary, offset, and a bounded reported name, and no row
// data. It swaps the process default logger, so it must not run in parallel.
func TestRetryingExecutorParserIntervalRaceLogsWarn(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	stmt := fileStatement(node.RootFileCreateMissingCypher)
	stmt.Parameters["rows"] = []map[string]any{{"path": "/secret/row/path.go"}}
	inner := &parserRaceGroupExecutor{failFor: 1, err: parserRaceError(stmt.Cypher, 266, stmt.Cypher[:200])}
	if err := (&RetryingExecutor{Inner: inner, BaseDelay: time.Millisecond}).
		ExecuteGroup(context.Background(), []Statement{stmt}); err != nil {
		t.Fatalf("ExecuteGroup() error = %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"neo4j_parser_interval_race", "offset=266", "statement_summary=\"files rows=0\"", "write_phase=canonical_upsert",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("log missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "/secret/row/path.go") {
		t.Errorf("log leaked row data:\n%s", out)
	}
}
