// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package search

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graph"
)

type recordingRunner struct {
	calls []runnerCall
	rows  func(cypher string) []map[string]any
	err   error
}

type runnerCall struct {
	cypher string
	params map[string]any
}

func (r *recordingRunner) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	r.calls = append(r.calls, runnerCall{cypher: cypher, params: params})
	if r.err != nil {
		return nil, r.err
	}
	if r.rows == nil {
		return nil, nil
	}
	return r.rows(cypher), nil
}

// TestBackfillKeysEveryLabelWithTheWriterClause proves the backfill runs one
// batched statement per infra search label, anchored on that label, writing
// exactly the shared writer clause (so backfilled keys equal written keys),
// and only for nodes whose stored key or flag disagrees with their fields.
func TestBackfillKeysEveryLabelWithTheWriterClause(t *testing.T) {
	t.Parallel()

	runner := &recordingRunner{rows: func(string) []map[string]any {
		return []map[string]any{{"keyed": int64(3), "overflowed": int64(1)}}
	}}
	var logs bytes.Buffer
	results, err := Backfill(context.Background(), runner, BackfillOptions{
		BatchSize: 250,
		Logger:    slog.New(slog.NewJSONHandler(&logs, nil)),
	})
	if err != nil {
		t.Fatalf("Backfill() error = %v", err)
	}
	labels := graph.InfraSearchKeyLabels()
	if len(runner.calls) != len(labels) || len(results) != len(labels) {
		t.Fatalf("Backfill ran %d statements and returned %d results, want %d each", len(runner.calls), len(results), len(labels))
	}
	for i, call := range runner.calls {
		label := labels[i]
		if !strings.HasPrefix(call.cypher, "MATCH (n:"+label+")\n") {
			t.Errorf("statement %d does not anchor on %s: %q", i, label, call.cypher)
		}
		if !strings.Contains(call.cypher, "CALL (n) {\n  "+Clause("n")+"\n} IN TRANSACTIONS OF $batch_size ROWS") {
			t.Errorf("statement for %s does not write the shared clause in batched transactions", label)
		}
		if !strings.Contains(call.cypher, "WHERE n.infra_search_ovf IS NULL\n   OR n.infra_search_ovf <> CASE WHEN size(infra_search_joined) > 8000 THEN 'T' ELSE 'F' END") {
			t.Errorf("statement for %s rewrites nodes whose key already matches", label)
		}
		if strings.Contains(strings.ReplaceAll(call.cypher, Clause("n"), ""), "SET ") {
			t.Errorf("statement for %s sets a property outside the shared clause", label)
		}
		if got := call.params["batch_size"]; got != 250 {
			t.Errorf("batch_size param = %v, want 250", got)
		}
		if results[i].Label != label || results[i].Keyed != 3 || results[i].Overflowed != 1 {
			t.Errorf("result %d = %+v, want label %s keyed 3 overflowed 1", i, results[i], label)
		}
	}
	for _, want := range []string{`"event_name":"bootstrap.graph.infra_search_key.label_backfilled"`, `"event_name":"bootstrap.graph.infra_search_key.backfilled"`, `"nodes_keyed":3`, `"overflow_count":1`, `"label":"CloudResource"`} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("logs missing %s", want)
		}
	}
}

// TestBackfillStopsOnTheFirstFailedLabel returns the error with the label so
// bootstrap fails before it writes the graph schema marker and retries the
// whole idempotent backfill on the next run.
func TestBackfillStopsOnTheFirstFailedLabel(t *testing.T) {
	t.Parallel()

	runner := &recordingRunner{err: errors.New("boom")}
	_, err := Backfill(context.Background(), runner, BackfillOptions{})
	if err == nil || !strings.Contains(err.Error(), "CloudResource") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("Backfill() error = %v, want the failing label and cause", err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("Backfill ran %d statements after a failure, want 1", len(runner.calls))
	}
	if runner.calls[0].params["batch_size"] != DefaultBackfillBatchSize {
		t.Fatalf("default batch_size = %v, want %d", runner.calls[0].params["batch_size"], DefaultBackfillBatchSize)
	}
}

// TestCountUnkeyedReadsEveryLabel exposes the per-label count the read
// cutover (#7245 PR-2) gates on: nodes never keyed have no overflow flag.
func TestCountUnkeyedReadsEveryLabel(t *testing.T) {
	t.Parallel()

	runner := &recordingRunner{rows: func(cypher string) []map[string]any {
		if strings.HasPrefix(cypher, "MATCH (n:TerraformLocal)") {
			return []map[string]any{{"unkeyed": int64(7)}}
		}
		return []map[string]any{{"unkeyed": int64(0)}}
	}}
	counts, err := CountUnkeyed(context.Background(), runner)
	if err != nil {
		t.Fatalf("CountUnkeyed() error = %v", err)
	}
	if len(counts) != len(graph.InfraSearchKeyLabels()) {
		t.Fatalf("CountUnkeyed() returned %d labels, want %d", len(counts), len(graph.InfraSearchKeyLabels()))
	}
	if counts["TerraformLocal"] != 7 || counts["CloudResource"] != 0 {
		t.Fatalf("CountUnkeyed() = %v, want TerraformLocal=7 CloudResource=0", counts)
	}
	for _, call := range runner.calls {
		if !strings.Contains(call.cypher, "WHERE n.infra_search_ovf IS NULL\nRETURN count(n) AS unkeyed") {
			t.Fatalf("count statement = %q, want an overflow IS NULL count", call.cypher)
		}
	}
}

// TestOversizedKeyCountProbesEveryLabel pins the pre-index readiness probe:
// one statement over all 27 labels counting keys past the cap.
func TestOversizedKeyCountProbesEveryLabel(t *testing.T) {
	t.Parallel()

	runner := &recordingRunner{rows: func(string) []map[string]any {
		return []map[string]any{{"oversized": int64(2)}}
	}}
	got, err := OversizedKeyCount(context.Background(), runner)
	if err != nil || got != 2 {
		t.Fatalf("OversizedKeyCount() = %d, %v; want 2", got, err)
	}
	want := "MATCH (n:" + strings.Join(graph.InfraSearchKeyLabels(), "|") + ")\nWHERE size(n.infra_search_key) > 8000\nRETURN count(n) AS oversized"
	if len(runner.calls) != 1 || runner.calls[0].cypher != want {
		t.Fatalf("probe statement = %v, want %q", runner.calls, want)
	}
	if _, err := OversizedKeyCount(context.Background(), &recordingRunner{err: errors.New("down")}); err == nil {
		t.Fatal("OversizedKeyCount() error = nil on a failed probe, want an error")
	}
}
