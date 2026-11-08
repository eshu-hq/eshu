// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"database/sql"
	"testing"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const testNVDWindowScopeForArming = "vuln-intel://nvd/_window/1767225600000000-1767398400000000"

func testNVDWindowSnapshotEnvelope(payload map[string]any) facts.Envelope {
	return facts.Envelope{
		FactKind:      "vulnerability.source_snapshot",
		StableFactKey: "vulnerability.source_snapshot:nvd",
		Payload:       payload,
	}
}

func testNVDWindowVerdictPayload(shouldAdvance bool) map[string]any {
	return map[string]any{
		"coverage_descriptor":            map[string]any{"version": "nvd.v1"},
		"nvd_window_advance_to_unix_us":  int64(1767398400000000),
		"nvd_window_head_unix_us":        int64(1767398400000000),
		"nvd_window_should_advance":      shouldAdvance,
		"nvd_window_time_anomaly_reason": "",
	}
}

// TestCollectedGenerationArmsWindowCursorWriter is the #9877 Path-A commit
// cell: a window-scope generation whose satellite-stamped verdict says
// advance arms the cursor writer in the commit transaction, so the cursor
// moves with its window's facts or not at all.
func TestCollectedGenerationArmsWindowCursorWriter(t *testing.T) {
	t.Parallel()

	source := mustNewSource(t, &recordingRunner{}, nil)
	item := testWorkItem()
	item.ScopeID = testNVDWindowScopeForArming
	result := completeResult(item)
	envelopes := []facts.Envelope{testNVDWindowSnapshotEnvelope(testNVDWindowVerdictPayload(true))}
	collected := source.collectedGeneration(item, result, envelopes, "")
	if collected.StateWriter == nil {
		t.Fatal("StateWriter = nil for an advancing window verdict, want the cursor writer armed")
	}
	recorder := &recordingCommitExecutor{}
	if err := collected.StateWriter(context.Background(), recorder); err != nil {
		t.Fatalf("StateWriter() error = %v", err)
	}
	if len(recorder.queries) != 1 {
		t.Fatalf("StateWriter() ran %d statements, want one cursor advance", len(recorder.queries))
	}
	// A JSON round trip decodes the stamped micros as float64; the arming
	// reads every transport number shape.
	floatPayload := testNVDWindowVerdictPayload(true)
	floatPayload["nvd_window_advance_to_unix_us"] = float64(1767398400000000)
	floatPayload["nvd_window_head_unix_us"] = float64(1767398400000000)
	floatCollected := source.collectedGeneration(
		item, result, []facts.Envelope{testNVDWindowSnapshotEnvelope(floatPayload)}, "",
	)
	if floatCollected.StateWriter == nil {
		t.Fatal("StateWriter = nil for a float64 verdict, want the cursor writer armed")
	}
	args := recorder.args[0]
	if len(args) < 3 {
		t.Fatalf("StateWriter() args = %v, want instance, advance-to, head", args)
	}
	if args[0] != "scorecard-instance" {
		t.Fatalf("StateWriter() instance = %v, want scorecard-instance", args[0])
	}
	if args[1] != int64(1767398400000000) {
		t.Fatalf("StateWriter() advance-to = %v, want 1767398400000000", args[1])
	}
	if args[2] != int64(1767398400000000) {
		t.Fatalf("StateWriter() head = %v, want 1767398400000000", args[2])
	}
}

// TestCollectedGenerationHoldsWindowCursorWriter pins fail-closed arming: no
// verdict, ShouldAdvance=false, or a non-window scope arms no writer, so
// facts still commit while the cursor holds. An unchanged result carries no
// envelopes and arms nothing by the same rule.
func TestCollectedGenerationHoldsWindowCursorWriter(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		scopeID   string
		envelopes []facts.Envelope
	}{
		{name: "no snapshot fact", scopeID: testNVDWindowScopeForArming},
		{
			name:    "no verdict keys",
			scopeID: testNVDWindowScopeForArming,
			envelopes: []facts.Envelope{testNVDWindowSnapshotEnvelope(
				map[string]any{"coverage_descriptor": map[string]any{"version": "nvd.v1"}},
			)},
		},
		{
			name:    "should advance false",
			scopeID: testNVDWindowScopeForArming,
			envelopes: []facts.Envelope{testNVDWindowSnapshotEnvelope(
				testNVDWindowVerdictPayload(false),
			)},
		},
		{
			name:    "non-window scope with verdict",
			scopeID: "vuln-intel://nvd/modified",
			envelopes: []facts.Envelope{testNVDWindowSnapshotEnvelope(
				testNVDWindowVerdictPayload(true),
			)},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			source := mustNewSource(t, &recordingRunner{}, nil)
			item := testWorkItem()
			item.ScopeID = tc.scopeID
			result := completeResult(item)
			collected := source.collectedGeneration(item, result, tc.envelopes, "")
			if collected.StateWriter != nil {
				t.Fatalf("StateWriter != nil for %s, want no writer (cursor holds)", tc.name)
			}
		})
	}
}

type recordingCommitExecutor struct {
	queries []string
	args    [][]any
}

func (r *recordingCommitExecutor) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	r.queries = append(r.queries, query)
	r.args = append(r.args, args)
	return recordingSQLResult{}, nil
}

type recordingSQLResult struct{}

func (recordingSQLResult) LastInsertId() (int64, error) { return 0, nil }
func (recordingSQLResult) RowsAffected() (int64, error) { return 0, nil }

var _ = sdkcollector.ResultComplete
