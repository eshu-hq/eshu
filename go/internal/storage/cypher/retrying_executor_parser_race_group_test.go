// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	"github.com/eshu-hq/eshu/go/internal/projector/failure"
)

// realNonFirstGenerationGroup returns the atomic main group CanonicalNodeWriter
// sends for a repository that already has a previous generation. That is the
// only path that emits the two CreateMissing File templates named in #8190,
// and the same group carries the `generation_id <> $generation_id` retracts
// that AllStatementsAreReplaySafe rejects.
func realNonFirstGenerationGroup(t *testing.T) []Statement {
	t.Helper()
	exec := &mockGroupExecutor{}
	writer := NewCanonicalNodeWriter(exec, 500, nil)
	mat := canonical.CanonicalMaterialization{
		ScopeID: "scope-1", GenerationID: "gen-2", RepoID: "repo-1", RepoPath: "/repos/my-repo",
		FirstGeneration: false,
		Repository:      &canonical.RepositoryRow{RepoID: "repo-1", Name: "my-repo", Path: "/repos/my-repo"},
		Directories: []canonical.DirectoryRow{
			{Path: "/repos/my-repo/src", Name: "src", ParentPath: "/repos/my-repo", RepoID: "repo-1", Depth: 0},
		},
		Files: []canonical.FileRow{
			{Path: "/repos/my-repo/src/main.go", RelativePath: "src/main.go", Name: "main.go", Language: "go", RepoID: "repo-1", DirPath: "/repos/my-repo/src"},
			{Path: "/repos/my-repo/README.md", RelativePath: "README.md", Name: "README.md", Language: "markdown", RepoID: "repo-1", DirPath: "/repos/my-repo"},
		},
		Entities: []canonical.EntityRow{
			{EntityID: "e1", Label: "Function", EntityName: "main", FilePath: "/repos/my-repo/src/main.go", RelativePath: "src/main.go", StartLine: 5, EndLine: 10, Language: "go", RepoID: "repo-1"},
		},
	}
	if err := writer.Write(context.Background(), mat); err != nil {
		t.Fatalf("CanonicalNodeWriter.Write() error = %v", err)
	}
	if exec.groupCalls == 0 || len(exec.groupStmts) == 0 {
		t.Fatal("CanonicalNodeWriter.Write() sent no atomic group")
	}
	return exec.groupStmts
}

// TestParserIntervalRaceMatchesRealNonFirstGenerationGroup is the #8190
// regression on the production subject: the real non-first-generation atomic
// group, not a one-statement stand-in. The group is not replay-safe by
// statement shape, and the reason must still fire, because a parse-time
// SyntaxError inside one managed transaction applies nothing. Re-adding a
// replay-safe gate to the classifier fails this test.
func TestParserIntervalRaceMatchesRealNonFirstGenerationGroup(t *testing.T) {
	t.Parallel()

	group := realNonFirstGenerationGroup(t)
	if AllStatementsAreReplaySafe(group) {
		t.Fatal("fixture drift: the non-first-generation group is replay-safe by shape; this test no longer pins the #8190 subject")
	}
	cases := map[string]int{
		canonicalNodeRootFileCreateMissingCypher: 266,
		canonicalNodeFileCreateMissingCypher:     245,
	}
	found := 0
	for _, stmt := range group {
		off, ok := cases[stmt.Cypher]
		if !ok {
			continue
		}
		found++
		err := parserRaceError(stmt.Cypher, off, "U")
		if got := classifyRetryableGraphWriteGroupError(err, group); got != graphWriteRetryReasonParserIntervalRace {
			t.Errorf("group(len=%d) reason at offset %d = %q, want %q", len(group), off, got, graphWriteRetryReasonParserIntervalRace)
		}
		other := &neo4jdriver.Neo4jError{Code: "Neo.ClientError.Statement.SemanticError", Msg: err.(*neo4jdriver.Neo4jError).Msg}
		if got := classifyRetryableGraphWriteGroupError(other, group); got != "" {
			t.Errorf("same message under a non-SyntaxError code: reason = %q, want terminal", got)
		}
		plain := errors.New(err.(*neo4jdriver.Neo4jError).Msg)
		if got := classifyRetryableGraphWriteGroupError(plain, group); got != "" {
			t.Errorf("same message as a non-Neo4jError: reason = %q, want terminal", got)
		}
	}
	if found != len(cases) {
		t.Fatalf("found %d of %d CreateMissing templates in the group (len=%d)", found, len(cases), len(group))
	}
}

// TestParserIntervalRaceIgnoresStatementShape proves the class rests on the
// error, not on statement shape: a CREATE statement and a mixed group both
// retry, because the SyntaxError is raised before any statement runs.
func TestParserIntervalRaceIgnoresStatementShape(t *testing.T) {
	t.Parallel()

	createStmt := Statement{Operation: OperationCanonicalUpsert, Cypher: "CREATE (f:File {path: $p})\nSET f.x = 1"}
	createOff := strings.Index(createStmt.Cypher, "f:File")
	if got := classifyRetryableGraphWriteError(parserRaceError(createStmt.Cypher, createOff, "C"), createStmt); got != graphWriteRetryReasonParserIntervalRace {
		t.Errorf("single CREATE reason = %q, want %q", got, graphWriteRetryReasonParserIntervalRace)
	}
	root := fileStatement(canonicalNodeRootFileCreateMissingCypher)
	mixed := []Statement{root, createStmt}
	if got := classifyRetryableGraphWriteGroupError(parserRaceError(root.Cypher, 266, "U"), mixed); got != graphWriteRetryReasonParserIntervalRace {
		t.Errorf("mixed group reason = %q, want %q", got, graphWriteRetryReasonParserIntervalRace)
	}
}

// TestParserIntervalRaceRejectsNonTokenOffsets pins the token-boundary checks:
// an offset on the last character of a multi-character identifier, on
// punctuation, or on a space never matches, even with a name that is a prefix
// of the statement.
func TestParserIntervalRaceRejectsNonTokenOffsets(t *testing.T) {
	t.Parallel()

	root := canonicalNodeRootFileCreateMissingCypher
	rowW := strings.Index(root, "row.path") + 2 // 'w' of `row`: previous character is an identifier part
	equals := strings.Index(root, " = ") + 1    // '=' with a space on each side
	space := strings.Index(root, "\n    ") + 1  // indent space after a newline, next to another space
	if root[rowW] != 'w' || root[equals] != '=' || root[space] != ' ' || root[space-1] != '\n' || root[space+1] != ' ' {
		t.Fatalf("fixture drift: rowW=%q equals=%q space=%q", root[rowW], root[equals], root[space])
	}
	tests := []struct {
		name string
		off  int
		rep  string
	}{
		{"last char of row, prefix name", rowW, "U"},
		{"last char of row, same name", rowW, "w"},
		{"punctuation, prefix name", equals, "U"},
		{"space, prefix name", space, "U"},
		{"space, empty name", space, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := parserRaceError(root, tc.off, tc.rep)
			stmt := fileStatement(root)
			if got := classifyRetryableGraphWriteError(err, stmt); got != "" {
				t.Errorf("single reason = %q, want terminal", got)
			}
			if got := classifyRetryableGraphWriteGroupError(err, []Statement{stmt}); got != "" {
				t.Errorf("group reason = %q, want terminal", got)
			}
		})
	}
}

// assertParserRaceExhaustedErrorIsTerminal checks the contract after the retry
// budget: the original Neo4j SyntaxError surfaces, it does not opt into queue
// retry, and it carries no durable failure class (so it is not counted as a
// graph write timeout and feeds no producer backpressure).
func assertParserRaceExhaustedErrorIsTerminal(t *testing.T, err error) {
	t.Helper()
	var neo4jErr *neo4jdriver.Neo4jError
	if !errors.As(err, &neo4jErr) || neo4jErr.Code != nornicDBStatementSyntaxErrorCode {
		t.Fatalf("exhausted error = %v, want the original %s Neo4jError", err, nornicDBStatementSyntaxErrorCode)
	}
	if failure.IsRetryable(err) {
		t.Fatalf("exhausted error is retryable (%T), want terminal", err)
	}
	var classed interface{ FailureClass() string }
	if errors.As(err, &classed) {
		t.Fatalf("exhausted error carries FailureClass %q, want none", classed.FailureClass())
	}
}

// TestRetryingExecutorParserIntervalRaceExhaustedOnRealGroup drives the real
// group through the retry loop with a persistent race error: every retry is
// counted, and the surfaced error is the original terminal SyntaxError.
func TestRetryingExecutorParserIntervalRaceExhaustedOnRealGroup(t *testing.T) {
	t.Parallel()

	group := realNonFirstGenerationGroup(t)
	inner := &parserRaceGroupExecutor{failFor: 1 << 20, err: parserRaceError(canonicalNodeRootFileCreateMissingCypher, 266, "U")}
	inst, reader := newParserRaceInstruments(t)
	err := (&RetryingExecutor{Inner: inner, MaxRetries: 3, BaseDelay: time.Millisecond, Instruments: inst}).
		ExecuteGroup(context.Background(), group)
	if got := inner.calls.Load(); got != 4 {
		t.Fatalf("calls = %d, want 4 (1 attempt + MaxRetries 3)", got)
	}
	if got := retryCounterReasons(t, reader)[graphWriteRetryReasonParserIntervalRace]; got != 3 {
		t.Fatalf("retry counter %s = %d, want 3", graphWriteRetryReasonParserIntervalRace, got)
	}
	assertParserRaceExhaustedErrorIsTerminal(t, err)
}

// TestRetryingExecutorParserIntervalRaceLogBoundsName proves the WARN caps
// reported_name at parserRaceLogNameLimit characters and reports the full
// length separately. It swaps the process default logger, so it must not run
// in parallel.
func TestRetryingExecutorParserIntervalRaceLogBoundsName(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	stmt := fileStatement(canonicalNodeRootFileCreateMissingCypher)
	reported := stmt.Cypher[:200]
	inner := &parserRaceGroupExecutor{failFor: 1, err: parserRaceError(stmt.Cypher, 266, reported)}
	if err := (&RetryingExecutor{Inner: inner, BaseDelay: time.Millisecond}).
		ExecuteGroup(context.Background(), []Statement{stmt}); err != nil {
		t.Fatalf("ExecuteGroup() error = %v", err)
	}
	var record map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var r map[string]any
		if json.Unmarshal([]byte(line), &r) == nil && r["reason"] == graphWriteRetryReasonParserIntervalRace {
			record = r
			break
		}
	}
	if record == nil {
		t.Fatalf("no parser race WARN record in:\n%s", buf.String())
	}
	name, _ := record["reported_name"].(string)
	if n := utf8.RuneCountInString(name); n != parserRaceLogNameLimit {
		t.Errorf("reported_name has %d runes, want %d (capped)", n, parserRaceLogNameLimit)
	}
	if got, _ := record["reported_name_length"].(float64); int(got) != utf8.RuneCountInString(reported) {
		t.Errorf("reported_name_length = %v, want %d", record["reported_name_length"], utf8.RuneCountInString(reported))
	}
}
