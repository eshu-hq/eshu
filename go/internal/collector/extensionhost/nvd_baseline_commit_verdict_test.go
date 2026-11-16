// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// The chunk start of testNVDBaselineScopeForCommit, in Unix microseconds
// (2002-01-01T00:00:00Z): the stamp a resumed claim starts from.
const testNVDBaselineChunkStartMicros = int64(1009843200000000)

// baselineWriterArgs runs the commit policy over a payload and returns the
// checkpoint-advance statement args when the writer is armed, or nil when
// the checkpoint holds.
func baselineWriterArgs(t *testing.T, payload map[string]any) []any {
	t.Helper()
	source := testNVDBaselineSource(t, testNVDBaselinePolicy(t))
	item := testWorkItem()
	item.ScopeID = testNVDBaselineScopeForCommit
	result := completeResult(item)
	envelopes := []facts.Envelope{testNVDWindowSnapshotEnvelope(payload)}
	collected := source.collectedGeneration(item, result, envelopes, "")
	if err := source.applyCommitPolicy(item, result.Generation.ObservedAt, envelopes, &collected); err != nil {
		t.Fatalf("applyCommitPolicy() error = %v, want a decision (armed or held)", err)
	}
	if collected.StateWriter == nil {
		return nil
	}
	recorder := &recordingCommitExecutor{}
	if err := collected.StateWriter(context.Background(), recorder); err != nil {
		t.Fatalf("StateWriter() error = %v", err)
	}
	if len(recorder.args) != 1 {
		t.Fatalf("StateWriter() ran %d statements, want one", len(recorder.args))
	}
	return recorder.args[0]
}

// TestBaselineCommitCompleteVerdictAtChunkStartArmsTheCheckpoint is the
// #10761 F3 cell. The last chunk of a year can end exactly at the stream
// tail with complete=false; the next chunk starts at that resume, finds
// nothing left, and stamps complete=true with advance_to equal to its own
// start. That verdict must land year_complete, or the planner re-mints the
// same scope every pace interval and the lane stalls on the year.
func TestBaselineCommitCompleteVerdictAtChunkStartArmsTheCheckpoint(t *testing.T) {
	t.Parallel()

	payload := testNVDBaselineVerdictPayload()
	payload["nvd_baseline_advance_to_unix_us"] = testNVDBaselineChunkStartMicros
	payload["nvd_baseline_year_complete"] = true
	args := baselineWriterArgs(t, payload)
	if args == nil {
		t.Fatal("complete verdict with advance_to == chunk start armed no writer: year_complete never lands and the planner re-mints the same chunk")
	}
	if args[2] != testNVDBaselineChunkStartMicros {
		t.Fatalf("resume arg = %v, want the chunk start %d", args[2], testNVDBaselineChunkStartMicros)
	}
	if args[4] != true {
		t.Fatalf("complete arg = %v, want true", args[4])
	}
}

// TestBaselineCommitHoldsWhenAdvanceDoesNotProgress pins the other side of
// the advance_to contract: an incomplete verdict must move strictly past the
// chunk start, and no verdict may move behind it. Complete does not excuse
// a regression.
func TestBaselineCommitHoldsWhenAdvanceDoesNotProgress(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		advance  int64
		complete bool
	}{
		{name: "incomplete at start", advance: testNVDBaselineChunkStartMicros},
		{name: "incomplete behind start", advance: testNVDBaselineChunkStartMicros - 1000},
		{name: "complete behind start", advance: testNVDBaselineChunkStartMicros - 1000, complete: true},
		// A sub-millisecond advance floors to the chunk start: it is no
		// progress in the whole-millisecond domain, so an incomplete
		// verdict holds instead of storing a value the scope id cannot
		// carry.
		{name: "incomplete sub-millisecond past start", advance: testNVDBaselineChunkStartMicros + 500},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			payload := testNVDBaselineVerdictPayload()
			payload["nvd_baseline_advance_to_unix_us"] = tc.advance
			payload["nvd_baseline_year_complete"] = tc.complete
			if args := baselineWriterArgs(t, payload); args != nil {
				t.Fatalf("writer armed with resume %v for %s, want the checkpoint held", args[2], tc.name)
			}
		})
	}
}

// TestBaselineCommitNormalizesAdvanceToWholeMilliseconds is the #10761 F7
// commit cell: a satellite advance_to with sub-millisecond digits
// (collector#123 widens the API window by one millisecond, so finer stamps
// can reach the stream) is stored as its whole-millisecond floor, the value
// the next plan's scope id and claim start can carry.
func TestBaselineCommitNormalizesAdvanceToWholeMilliseconds(t *testing.T) {
	t.Parallel()

	payload := testNVDBaselineVerdictPayload()
	payload["nvd_baseline_advance_to_unix_us"] = int64(1041379200000500)
	args := baselineWriterArgs(t, payload)
	if args == nil {
		t.Fatal("sub-millisecond advance past the start armed no writer, want the checkpoint armed")
	}
	if args[2] != int64(1041379200000000) {
		t.Fatalf("resume arg = %v, want the whole-millisecond floor 1041379200000000", args[2])
	}
}
