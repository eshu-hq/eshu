// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

func TestFileGroupTimingOptIn(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		raw  string
		want bool
	}{
		{"", false}, {"false", false}, {"true", true},
	} {
		got, err := fileGroupTimingEnabled(func(string) string { return tc.raw })
		if err != nil || got != tc.want {
			t.Fatalf("value %q: got %t, %v; want %t", tc.raw, got, err, tc.want)
		}
	}
	if _, err := fileGroupTimingEnabled(func(string) string { return "sometimes" }); err == nil {
		t.Fatal("invalid opt-in accepted")
	}
}

func TestFileGroupProbeSanitizesAttemptsAndFinalOutcome(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	stmt := testFileProbeStatement(t)
	stmt.Parameters["secret"] = "private-token"
	probe := newFileGroupProbe(logger, []sourcecypher.Statement{stmt})
	if probe == nil {
		t.Fatal("known file group was not selected")
	}
	runError := errors.New("private-error-text")
	_, err := probe.runAttempt(context.Background(), []sourcecypher.Statement{stmt}, func(context.Context, sourcecypher.Statement) (fileResultConsumer, error) {
		return nil, runError
	})
	if !errors.Is(err, runError) {
		t.Fatalf("Run error = %v, want sentinel", err)
	}
	_, err = probe.runAttempt(context.Background(), []sourcecypher.Statement{stmt}, func(context.Context, sourcecypher.Statement) (fileResultConsumer, error) {
		return func(context.Context) (fileResult, error) { return fileResult{}, runError }, nil
	})
	if !errors.Is(err, runError) {
		t.Fatalf("Consume error = %v, want sentinel", err)
	}
	probe.finish(context.Background(), runError)
	got := logs.String()
	firstEvent := strings.SplitN(got, "\n", 2)[0]
	if strings.Contains(firstEvent, `"consume_duration_s":`) {
		t.Errorf("reported unobserved Consume time after Run failed: %s", firstEvent)
	}
	for _, want := range []string{`"attempt":1`, `"attempt":2`, `"outcome":"run_failed"`, `"outcome":"consume_failed"`, `"outcome":"failed"`, `"group_call_id":`, `"pipeline_phase":"projection"`, `"template_id":"file.nested.first_generation"`, `"row_count":1`, `"run_duration_s":`, `"consume_duration_s":`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
	if strings.Contains(got, `"post_callback_duration_s":`) {
		t.Errorf("reported unobserved post-callback time: %s", got)
	}
	for _, forbidden := range []string{"/sensitive/source.go", "private-token", "private-error-text", "UNWIND", "statement_summary", "first_path", "parameters", `"error"`} {
		if strings.Contains(got, forbidden) {
			t.Errorf("log leaked %q: %s", forbidden, got)
		}
	}
}

func TestFileGroupProbeConcurrentIDsAndSelection(t *testing.T) {
	t.Parallel()
	stmt := testFileProbeStatement(t)
	if newFileGroupProbe(slog.Default(), []sourcecypher.Statement{{Cypher: "RETURN 1"}}) != nil {
		t.Fatal("unknown group selected")
	}
	const workers = 40
	ids := make(chan uint64, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ids <- newFileGroupProbe(slog.Default(), []sourcecypher.Statement{stmt}).id
		}()
	}
	wg.Wait()
	close(ids)
	seen := make(map[uint64]bool)
	for id := range ids {
		if seen[id] || id == 0 {
			t.Fatalf("duplicate/zero group ID %d", id)
		}
		seen[id] = true
	}
}

type fileProbeCaptureExecutor struct{ calls []sourcecypher.Statement }

func (e *fileProbeCaptureExecutor) Execute(_ context.Context, stmt sourcecypher.Statement) error {
	e.calls = append(e.calls, stmt)
	return nil
}

func testFileProbeStatement(t *testing.T) sourcecypher.Statement {
	return testFileProbeStatementFor(t, "repo", "/sensitive", "/sensitive/source.go")
}

func testFileProbeStatementFor(t *testing.T, repoID, dirPath, filePath string) sourcecypher.Statement {
	t.Helper()
	exec := &fileProbeCaptureExecutor{}
	writer := sourcecypher.NewCanonicalNodeWriter(exec, 100, nil)
	err := writer.Write(context.Background(), projector.CanonicalMaterialization{
		ScopeID: "scope", GenerationID: "generation", RepoID: repoID,
		FirstGeneration: true,
		Files: []projector.FileRow{{
			Path: filePath, RelativePath: "src/source.go",
			Name: "source.go", RepoID: repoID, DirPath: dirPath,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range exec.calls {
		if _, _, ok := sourcecypher.CanonicalFileStatementProfile(stmt); ok {
			return stmt
		}
	}
	t.Fatal("writer emitted no file statement")
	return sourcecypher.Statement{}
}

func TestFileGroupProbeSeparatesPostCallbackFailure(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	probe := newFileGroupProbe(slog.New(slog.NewJSONHandler(&logs, nil)), []sourcecypher.Statement{testFileProbeStatement(t)})
	stmt := testFileProbeStatement(t)
	counts, err := probe.runAttempt(context.Background(), []sourcecypher.Statement{stmt}, func(context.Context, sourcecypher.Statement) (fileResultConsumer, error) {
		return func(context.Context) (fileResult, error) {
			return fileResult{Statement: stmt}, nil
		}, nil
	})
	if err != nil || len(counts) != 1 {
		t.Fatalf("callback = (%d counts, %v), want one successful attempt", len(counts), err)
	}
	probe.finish(context.Background(), errors.New("private commit error"))
	got := logs.String()
	for _, want := range []string{`"outcome":"attempt_completed"`, `"outcome":"failed"`, `"attempts":1`, `"post_callback_duration_s":`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
	if strings.Contains(got, "private commit error") {
		t.Errorf("commit error leaked: %s", got)
	}
}

var fileTimingBenchEnabled bool

func BenchmarkFileGroupProbeDisabledPath(b *testing.B) {
	stmt := benchmarkFileProbeStatement(b)
	rows := make([]map[string]any, 100)
	for i := range rows {
		rows[i] = map[string]any{"path": "/benchmark/source.go"}
	}
	stmt.Parameters["rows"] = rows
	stmts := []sourcecypher.Statement{stmt, stmt, stmt, stmt, stmt}
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	b.Run("disabled", func(b *testing.B) {
		fileTimingBenchEnabled = false
		b.ReportAllocs()
		for b.Loop() {
			if fileTimingBenchEnabled {
				_ = newFileGroupProbe(logger, stmts)
			}
		}
	})
	b.Run("enabled", func(b *testing.B) {
		fileTimingBenchEnabled = true
		b.ReportAllocs()
		for b.Loop() {
			probe := newFileGroupProbe(logger, stmts)
			_, _ = probe.runAttempt(context.Background(), stmts, func(_ context.Context, stmt sourcecypher.Statement) (fileResultConsumer, error) {
				return func(context.Context) (fileResult, error) { return fileResult{Statement: stmt}, nil }, nil
			})
			probe.finish(context.Background(), nil)
		}
	})
}

func benchmarkFileProbeStatement(b *testing.B) sourcecypher.Statement {
	b.Helper()
	exec := &fileProbeCaptureExecutor{}
	writer := sourcecypher.NewCanonicalNodeWriter(exec, 100, nil)
	err := writer.Write(context.Background(), projector.CanonicalMaterialization{
		ScopeID: "scope", GenerationID: "generation", RepoID: "repo",
		FirstGeneration: true,
		Files: []projector.FileRow{{
			Path: "/benchmark/source.go", RelativePath: "src/source.go",
			Name: "source.go", RepoID: "repo", DirPath: "/benchmark",
		}},
	})
	if err != nil {
		b.Fatal(err)
	}
	for _, stmt := range exec.calls {
		if _, _, ok := sourcecypher.CanonicalFileStatementProfile(stmt); ok {
			return stmt
		}
	}
	b.Fatal("writer emitted no file statement")
	return sourcecypher.Statement{}
}
