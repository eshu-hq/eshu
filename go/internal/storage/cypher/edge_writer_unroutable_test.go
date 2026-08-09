// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"bytes"
	"context"
	"encoding/json"
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

// TestEdgeWriterWriteEdgesAllRowsUnroutableReportsEveryLostRow pins the
// accuracy contract from #5984 as the owner settled it on PR #6008.
//
// A non-empty batch where nothing routed is "nothing could be done", not
// "nothing to do". The original defect was returning nil with no record: the
// worker completed the intent, and completed rows are never reopened, so the
// edges were permanently absent and unrecorded.
//
// The fix is NOT to fail the write. buildRowMap decides from the persisted
// payload, so the rejection is deterministic and no retry can ever succeed,
// and this path has no attempt budget or dead letter — failing stalls the
// partition forever. The writer reports every rejected row instead, and the
// caller records them durably before completing.
func TestEdgeWriterWriteEdgesAllRowsUnroutableReportsEveryLostRow(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		unroutableRepoDependencyRow("i1"),
		unroutableRepoDependencyRow("i2"),
	}

	report, err := writer.WriteEdges(context.Background(), reducer.DomainRepoDependency, rows, "finalization/workloads")
	assertAllRowsUnroutable(t, report, err, len(executor.calls), reducer.DomainRepoDependency, 2)

	seen := map[string]bool{}
	for _, row := range report.UnroutableRows {
		seen[row.IntentID] = true
		if row.EvidenceSource != "finalization/workloads" {
			t.Errorf("evidence source = %q, want the write's own source", row.EvidenceSource)
		}
		if row.DecidedAt.IsZero() {
			t.Error("DecidedAt is zero; the durable row needs a decision time")
		}
	}
	for _, want := range []string{"i1", "i2"} {
		if !seen[want] {
			t.Errorf("intent %q missing from the report; every lost row must be recordable", want)
		}
	}
}

// TestEdgeWriterWriteEdgesEmptyBatchStaysSuccessful keeps the genuine
// "nothing to do" fast path intact: an empty batch is not a failure.
func TestEdgeWriterWriteEdgesEmptyBatchStaysSuccessful(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	if _, err := writer.WriteEdges(
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

	if _, err := writer.WriteEdges(
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

// TestEdgeWriterWriteEdgesControlRowOnlyBatchStaysSuccessful is the guard that
// keeps the #5984 accuracy check from becoming an availability bug.
//
// A repo refresh row exists to drive the repo-wide retract and carries no edge
// of its own. A code-call delta that only deleted call sites is exactly this
// batch. Treating it as an unroutable edge would refuse to complete the intent
// on every poll and wedge the partition on work that was correct. Caught by
// BenchmarkEdgeWriterCodeCallRetractAndWrite/delta_deleted_only_50_files_0_call_rows,
// which drives this shape directly.
func TestEdgeWriterWriteEdgesControlRowOnlyBatchStaysSuccessful(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)
	writer.Logger = slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "refresh-delta",
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"repo_id":          "repo-a",
				"delta_projection": true,
				"delta_file_paths": []string{"a.go"},
				"intent_type":      "repo_refresh",
			},
		},
	}

	if _, err := writer.WriteEdges(
		context.Background(),
		reducer.DomainCodeCalls,
		rows,
		"parser/code-calls",
	); err != nil {
		t.Fatalf("WriteEdges() error = %v, want nil for a control-row-only batch", err)
	}
	if got := len(executor.calls); got != 0 {
		t.Fatalf("executor calls = %d, want 0", got)
	}
	if _, ok := findLogEntry(t, logs.Bytes(), "shared edge rows unroutable"); ok {
		t.Fatalf("control row was reported as a dropped edge; logs:\n%s", logs.String())
	}
}

// TestEdgeWriterWriteEdgesControlRowPlusUnroutableRowReportsBothCounts covers
// the mixed shape that has neither a routable row nor a uniform batch: one
// control row that carries no edge by design, and one row that should have
// become an edge and could not be routed.
//
// The control row is not a dropped edge, so only the unroutable one is
// reported. Copilot's original finding was that the message conflated the two
// counts; under the report contract the same distinction is now structural —
// the control row simply does not appear in UnroutableRows at all.
func TestEdgeWriterWriteEdgesControlRowPlusUnroutableRowReportsOnlyTheLostEdge(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "refresh",
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"repo_id":     "repo-a",
				"intent_type": "repo_refresh",
			},
		},
		unroutableRepoDependencyRow("i2"),
	}

	report, err := writer.WriteEdges(context.Background(), reducer.DomainRepoDependency, rows, "finalization/workloads")
	if err != nil {
		t.Fatalf("WriteEdges() error = %v, want nil", err)
	}
	if got, want := len(report.UnroutableRows), 1; got != want {
		t.Fatalf("reported rows = %d, want %d (the control row carries no edge by design)", got, want)
	}
	if got, want := report.UnroutableRows[0].IntentID, "i2"; got != want {
		t.Errorf("reported intent = %q, want %q", got, want)
	}
	if got, want := report.UnroutableRows[0].Reason, reducer.UnroutableReasonMissingRequiredField; got != want {
		t.Errorf("reason = %q, want %q", got, want)
	}
	if got := len(executor.calls); got != 0 {
		t.Fatalf("executor calls = %d, want 0", got)
	}
}

// TestEdgeWriterWriteEdgesReportsVersionSkewSeparately keeps a rolling upgrade
// from reading as data loss.
//
// A repo_dependency row with a relationship type this binary has no statement
// for is WELL FORMED — a newer producer emitted a type an older writer has not
// learned yet, and the same row routes once the writer catches up. Recording
// that as a missing-field loss would send someone hunting a corrupt producer
// during a deploy that is working as intended.
func TestEdgeWriterWriteEdgesReportsVersionSkewSeparately(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{{
		IntentID:     "i1",
		RepositoryID: "repo-a",
		Payload: map[string]any{
			"repo_id":           "repo-a",
			"target_repo_id":    "repo-b",
			"relationship_type": "INVENTED_BY_A_NEWER_PRODUCER",
		},
	}}

	report, err := writer.WriteEdges(context.Background(), reducer.DomainRepoDependency, rows, "finalization/workloads")
	if err != nil {
		t.Fatalf("WriteEdges() error = %v, want nil", err)
	}
	if got, want := len(report.UnroutableRows), 1; got != want {
		t.Fatalf("reported rows = %d, want %d", got, want)
	}
	if got, want := report.UnroutableRows[0].Reason, reducer.UnroutableReasonNoStatementForType; got != want {
		t.Errorf("reason = %q, want %q: a type this binary lacks is version skew, not a malformed payload", got, want)
	}
}

// assertAllRowsUnroutable is the shared assertion for a batch whose every row
// is missing its required MATCH fields.
//
// The contract changed on PR #6008 after review: this is no longer an error.
// buildRowMap decides from the persisted payload, so a rejected row is
// rejected identically forever, and this path has no attempt budget or dead
// letter — failing here stalled the partition permanently on work that could
// never succeed. The writer now REPORTS the rejected rows and the caller
// records them durably before completing, so the partition keeps draining and
// the loss is queryable rather than silent.
func assertAllRowsUnroutable(
	t *testing.T,
	report reducer.SharedProjectionWriteReport,
	err error,
	calls int,
	domain string,
	wantDropped int,
) {
	t.Helper()

	if err != nil {
		t.Fatalf("WriteEdges() error = %v, want nil: an unroutable row is a loss to record, not a retryable failure", err)
	}
	if got := len(report.UnroutableRows); got != wantDropped {
		t.Fatalf("reported unroutable rows = %d, want %d", got, wantDropped)
	}
	for _, row := range report.UnroutableRows {
		if row.ProjectionDomain != domain {
			t.Errorf("reported domain = %q, want %q", row.ProjectionDomain, domain)
		}
		if row.Reason == "" {
			t.Error("reported row has no reason; an operator cannot tell a lost edge from version skew")
		}
		if row.IntentID == "" {
			t.Error("reported row has no intent id; the loss cannot be looked up")
		}
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
