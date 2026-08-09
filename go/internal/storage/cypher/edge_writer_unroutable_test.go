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

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// unroutableRepoDependencyRow is a repo_dependency intent row buildRowMap
// cannot route: repo_id is empty, so no MATCH endpoint exists. Before #5984
// WriteEdges dropped it, wrote nothing, and returned nil — which the shared
// projection worker read as success and used to complete the intent.
func unroutableRepoDependencyRow(intentID string) reducer.SharedProjectionIntentRow {
	return reducer.SharedProjectionIntentRow{
		IntentID:     intentID,
		RepositoryID: "repo-a",
		GenerationID: "gen-a",
		Payload: map[string]any{
			"repo_id":        "",
			"target_repo_id": "repo-b",
		},
	}
}

func routableRepoDependencyRow(intentID string) reducer.SharedProjectionIntentRow {
	return reducer.SharedProjectionIntentRow{
		IntentID:     intentID,
		RepositoryID: "repo-a",
		GenerationID: "gen-a",
		Payload: map[string]any{
			"repo_id":        "repo-a",
			"target_repo_id": "repo-b",
		},
	}
}

// TestEdgeWriterWriteEdgesAllRowsUnroutableFailsIntent pins the accuracy
// contract from #5984: a non-empty batch whose every row is unroutable is
// "nothing could be done", not "nothing to do". Returning nil there completes
// the shared-projection intent for edges that were never written, and the
// deterministic intent ID plus the completed-row fence mean the work is never
// reopened — the edges are simply absent and nothing records it.
func TestEdgeWriterWriteEdgesAllRowsUnroutableFailsIntent(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		unroutableRepoDependencyRow("i1"),
		unroutableRepoDependencyRow("i2"),
	}

	err := writer.WriteEdges(context.Background(), reducer.DomainRepoDependency, rows, "finalization/workloads")
	if err == nil {
		t.Fatal("WriteEdges() error = nil, want an error so the intent is not completed")
	}
	if got := err.Error(); !strings.Contains(got, "unroutable") {
		t.Fatalf("WriteEdges() error = %q, want it to name the unroutable rows", got)
	}
	if got := err.Error(); !strings.Contains(got, reducer.DomainRepoDependency) {
		t.Fatalf("WriteEdges() error = %q, want it to name the domain", got)
	}
	if got := len(executor.calls); got != 0 {
		t.Fatalf("executor calls = %d, want 0 (nothing was routable)", got)
	}
}

// TestEdgeWriterWriteEdgesEmptyBatchStaysSuccessful keeps the genuine
// "nothing to do" fast path intact: an empty batch is not a failure.
func TestEdgeWriterWriteEdgesEmptyBatchStaysSuccessful(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	if err := writer.WriteEdges(
		context.Background(),
		reducer.DomainRepoDependency,
		nil,
		"finalization/workloads",
	); err != nil {
		t.Fatalf("WriteEdges() error = %v, want nil for an empty batch", err)
	}
	if got := len(executor.calls); got != 0 {
		t.Fatalf("executor calls = %d, want 0", got)
	}
}

// TestEdgeWriterWriteEdgesPartialDropWarnsWithDroppedCount covers the mixed
// batch: the routable rows still write (dropping the whole partition would be
// a worse loss), but the partial loss must be visible to an operator rather
// than folded into a success log.
func TestEdgeWriterWriteEdgesPartialDropWarnsWithDroppedCount(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)
	writer.Logger = slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	rows := []reducer.SharedProjectionIntentRow{
		routableRepoDependencyRow("i1"),
		unroutableRepoDependencyRow("i2"),
	}

	if err := writer.WriteEdges(
		context.Background(),
		reducer.DomainRepoDependency,
		rows,
		"finalization/workloads",
	); err != nil {
		t.Fatalf("WriteEdges() error = %v, want nil when some rows routed", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}

	entry, ok := findLogEntry(t, logs.Bytes(), "shared edge rows unroutable")
	if !ok {
		t.Fatalf("no %q log entry; logs:\n%s", "shared edge rows unroutable", logs.String())
	}
	if got, want := entry["level"], "WARN"; got != want {
		t.Fatalf("dropped-row log level = %v, want %v", got, want)
	}
	if got, want := entry["domain"], reducer.DomainRepoDependency; got != want {
		t.Fatalf("dropped-row log domain = %v, want %v", got, want)
	}
	if got, want := entry["dropped_rows"], float64(1); got != want {
		t.Fatalf("dropped-row log dropped_rows = %v, want %v", got, want)
	}
	if got, want := entry["input_rows"], float64(2); got != want {
		t.Fatalf("dropped-row log input_rows = %v, want %v", got, want)
	}
	if got, ok := entry["sample_intent_id"].(string); !ok || got != "i2" {
		t.Fatalf("dropped-row log sample_intent_id = %v, want %q", entry["sample_intent_id"], "i2")
	}
}

// assertAllRowsUnroutable is the shared assertion for a batch whose every row
// is missing its required MATCH fields. It states the #5984 contract once:
// nothing reaches the executor AND the caller is told, so the shared
// projection intent is not completed for edges that were never written.
func assertAllRowsUnroutable(t *testing.T, err error, calls int, domain string, wantDropped int) {
	t.Helper()

	var unroutable *UnroutableRowsError
	if !errors.As(err, &unroutable) {
		t.Fatalf("WriteEdges() error = %v (%T), want *UnroutableRowsError", err, err)
	}
	if got := unroutable.Domain; got != domain {
		t.Fatalf("UnroutableRowsError.Domain = %q, want %q", got, domain)
	}
	if got := unroutable.DroppedRows; got != wantDropped {
		t.Fatalf("UnroutableRowsError.DroppedRows = %d, want %d", got, wantDropped)
	}
	if calls != 0 {
		t.Fatalf("executor calls = %d, want 0 (all rows filtered)", calls)
	}
}

// findLogEntry returns the first JSON log record whose msg matches want.
func findLogEntry(t *testing.T, raw []byte, want string) (map[string]any, bool) {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		entry := map[string]any{}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("log line is not JSON: %v (%s)", err, line)
		}
		if entry["msg"] == want {
			return entry, true
		}
	}
	return nil, false
}
