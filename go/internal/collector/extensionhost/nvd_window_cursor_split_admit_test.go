// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"database/sql"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// testNVDWindowSplitPartialPayload stamps a split-partial verdict: the
// advance-to sits strictly inside the minted window (midpoint), the way a
// satellite emitted-prefix split advances only to its emitted bound.
func testNVDWindowSplitPartialPayload() map[string]any {
	return map[string]any{
		"coverage_descriptor":            map[string]any{"version": "nvd.v1"},
		"nvd_window_advance_to_unix_us":  int64(1767312000000000),
		"nvd_window_head_unix_us":        int64(1767398400000000),
		"nvd_window_should_advance":      true,
		"nvd_window_time_anomaly_reason": "",
	}
}

// conditionalCommitExecutor applies the cursor table's compare-and-advance
// with the real NVDWindowCursorAdvanceQuery gating (nvd_window_cursor.go):
// only a strictly greater candidate cursor moves the row, and the head rides
// along as the max of that advance only, so a stale candidate leaves the
// stored cursor and head untouched. The race below proves composition
// through the real armed writers under these production semantics, without
// a live database.
type conditionalCommitExecutor struct {
	cursor int64
	head   int64
	set    bool
}

func (r *conditionalCommitExecutor) ExecContext(_ context.Context, _ string, args ...any) (sql.Result, error) {
	cursor, _ := args[1].(int64)
	head, _ := args[2].(int64)
	if !r.set || cursor > r.cursor {
		r.cursor = cursor
		if head > r.head {
			r.head = head
		}
		r.set = true
	}
	return recordingSQLResult{}, nil
}

// TestSplitPartialVerdictArmsWindowCursorWriter is the #9877 capacity-split
// host-admissibility cell: a verdict with AdvanceTo strictly inside the
// minted window arms the cursor writer, and the writer carries the partial
// advance-to (not the minted end) into the commit transaction.
func TestSplitPartialVerdictArmsWindowCursorWriter(t *testing.T) {
	t.Parallel()

	source := mustNewSource(t, &recordingRunner{}, nil)
	item := testWorkItem()
	item.ScopeID = testNVDWindowScopeForArming
	result := completeResult(item)
	collected := source.collectedGeneration(
		item, result,
		[]facts.Envelope{testNVDWindowSnapshotEnvelope(testNVDWindowSplitPartialPayload())},
		"",
	)
	if collected.StateWriter == nil {
		t.Fatal("StateWriter = nil for a split-partial verdict, want the cursor writer armed")
	}
	recorder := &recordingCommitExecutor{}
	if err := collected.StateWriter(context.Background(), recorder); err != nil {
		t.Fatalf("StateWriter() error = %v", err)
	}
	args := recorder.args[0]
	if len(args) < 3 {
		t.Fatalf("StateWriter() args = %v, want instance, advance-to, head", args)
	}
	if args[1] != int64(1767312000000000) {
		t.Fatalf("StateWriter() advance-to = %v, want the split-partial 1767312000000000", args[1])
	}
	if args[2] != int64(1767398400000000) {
		t.Fatalf("StateWriter() head = %v, want the server head 1767398400000000", args[2])
	}
}

// testNVDWindowSplitStalePayload stamps an adversarial stale verdict: an
// older advance-to carrying a newer observed head, the shape that
// distinguishes the real conditional head gating (stale leaves head
// untouched) from an independent max (stale moves head).
func testNVDWindowSplitStalePayload() map[string]any {
	return map[string]any{
		"coverage_descriptor":            map[string]any{"version": "nvd.v1"},
		"nvd_window_advance_to_unix_us":  int64(1767225600000000),
		"nvd_window_head_unix_us":        int64(1767484800000000),
		"nvd_window_should_advance":      true,
		"nvd_window_time_anomaly_reason": "",
	}
}

// TestSplitPartialAndFullCommitsComposeToMax runs the claim race every way:
// a split-partial and a full advance commit in either order, and a stale
// adversarial claim around the full one. The cursor always ends at the max
// and the head at the max of the observations that advanced, with no
// regression.
func TestSplitPartialAndFullCommitsComposeToMax(t *testing.T) {
	t.Parallel()

	arm := func(payload map[string]any) collector.GenerationStateWriter {
		source := mustNewSource(t, &recordingRunner{}, nil)
		item := testWorkItem()
		item.ScopeID = testNVDWindowScopeForArming
		collected := source.collectedGeneration(
			item, completeResult(item),
			[]facts.Envelope{testNVDWindowSnapshotEnvelope(payload)},
			"",
		)
		if collected.StateWriter == nil {
			t.Fatal("StateWriter = nil, want the cursor writer armed")
		}
		return collected.StateWriter
	}
	partial := arm(testNVDWindowSplitPartialPayload())
	full := arm(testNVDWindowVerdictPayload(true))
	stale := arm(testNVDWindowSplitStalePayload())
	for _, order := range []struct {
		name       string
		writers    []collector.GenerationStateWriter
		wantCursor int64
		wantHead   int64
	}{
		{name: "partial then full", writers: []collector.GenerationStateWriter{partial, full}, wantCursor: 1767398400000000, wantHead: 1767398400000000},
		{name: "full then partial", writers: []collector.GenerationStateWriter{full, partial}, wantCursor: 1767398400000000, wantHead: 1767398400000000},
		// A stale claim committing first stores its newer head; the full
		// claim then moves the cursor while the head keeps the max of both
		// observations, exactly like the SQL CASE.
		{name: "stale then full", writers: []collector.GenerationStateWriter{stale, full}, wantCursor: 1767398400000000, wantHead: 1767484800000000},
		// A stale claim after the full one advances nothing: the row keeps
		// the full claim's cursor and head.
		{name: "full then stale", writers: []collector.GenerationStateWriter{full, stale}, wantCursor: 1767398400000000, wantHead: 1767398400000000},
	} {
		t.Run(order.name, func(t *testing.T) {
			t.Parallel()

			exec := &conditionalCommitExecutor{}
			for _, writer := range order.writers {
				if err := writer(context.Background(), exec); err != nil {
					t.Fatalf("StateWriter() error = %v", err)
				}
			}
			if exec.cursor != order.wantCursor {
				t.Fatalf("race cursor = %d, want %d", exec.cursor, order.wantCursor)
			}
			if exec.head != order.wantHead {
				t.Fatalf("race head = %d, want %d", exec.head, order.wantHead)
			}
		})
	}
}
