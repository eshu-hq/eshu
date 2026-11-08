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

// maxEmulatingCommitExecutor applies the cursor table's compare-and-advance
// to in-memory state the way NVDWindowCursorAdvanceQuery does in SQL (keyed
// upsert keeping GREATEST of cursor and head), so the two-claim race proves
// composition through the real armed writers without a live database.
type maxEmulatingCommitExecutor struct {
	cursor int64
	head   int64
	set    bool
}

func (r *maxEmulatingCommitExecutor) ExecContext(_ context.Context, _ string, args ...any) (sql.Result, error) {
	cursor, _ := args[1].(int64)
	head, _ := args[2].(int64)
	if !r.set || cursor > r.cursor {
		r.cursor = cursor
		r.set = true
	}
	if head > r.head {
		r.head = head
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

// TestSplitPartialAndFullCommitsComposeToMax runs the two-claim race both
// ways: a split-partial and a full advance planned from one cursor commit in
// either order and the final cursor is the max, with no regression.
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
	for _, order := range []struct {
		name    string
		writers []collector.GenerationStateWriter
	}{
		{name: "partial then full", writers: []collector.GenerationStateWriter{partial, full}},
		{name: "full then partial", writers: []collector.GenerationStateWriter{full, partial}},
	} {
		t.Run(order.name, func(t *testing.T) {
			t.Parallel()

			exec := &maxEmulatingCommitExecutor{}
			for _, writer := range order.writers {
				if err := writer(context.Background(), exec); err != nil {
					t.Fatalf("StateWriter() error = %v", err)
				}
			}
			if exec.cursor != int64(1767398400000000) {
				t.Fatalf("race cursor = %d, want max 1767398400000000", exec.cursor)
			}
		})
	}
}
