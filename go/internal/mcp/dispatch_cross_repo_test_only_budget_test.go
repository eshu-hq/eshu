// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codequery/deadcode"
)

// testOnlyBarBytesPerRow is the most one flagged row may add to the est2x
// metric: the key and value, `"test_only_consumers":true,`, are 27 bytes per
// wire copy, and the dispatch budget counts two copies.
const testOnlyBarBytesPerRow = 2 * 40

// calibratedLiveStore is the calibrated base where every evidence row is a
// strong live consumer whose root sits in a test file: the worst case for the
// test_only_consumers term, because every one of the 24 rows carries the flag.
// withFlag false answers no root path, so the rows are identical but unflagged.
func calibratedLiveStore(withFlag bool) *crossRepoHandlesStore {
	store := calibratedPathologicalStore(0, true)
	producerRow := func(entityID string) (int, bool) {
		var row int
		if _, err := fmt.Sscanf(entityID, "ts-%d", &row); err != nil || row >= calibratedEvidenceRows {
			return 0, false
		}
		return row, true
	}
	store.evidenceFor = func(entityID string) []deadcode.CrossRepoDeadCodeEvidence {
		row, ok := producerRow(entityID)
		if !ok {
			return nil
		}
		item := handlesFixtureItem(row, 0, row)
		item.Ambiguous, item.NeedsEvidence, item.Reason = false, false, ""
		item.Confidence, item.ConfidenceLabel, item.ResolutionMethod = 0.95, "high", "scip"
		return []deadcode.CrossRepoDeadCodeEvidence{item}
	}
	if withFlag {
		store.rootPathFor = func(string) (string, bool) { return "tests/unit/test_handler.ts", true }
	}
	return store
}

// TestFindCrossRepoDeadCodeTestOnlyConsumersCalibratedBaseBar is the #7603 byte
// bar on the #7168 calibrated base. Every evidence row is live and flagged, the
// worst case for the flag; it may add at most testOnlyBarBytesPerRow bytes per
// row and the reply must still be delivered as structuredContent.
func TestFindCrossRepoDeadCodeTestOnlyConsumersCalibratedBaseBar(t *testing.T) {
	t.Parallel()

	plain := dispatchCrossRepoHandlesFixture(t, calibratedLiveStore(false), nil)
	flagged := dispatchCrossRepoHandlesFixture(t, calibratedLiveStore(true), nil)

	flaggedRows := 0
	data, _ := flagged.Envelope.Data.(map[string]any)
	buckets, _ := data["candidate_buckets"].(map[string]any)
	live, _ := buckets["live_by_consumer"].([]any)
	for _, raw := range live {
		row, _ := raw.(map[string]any)
		if row["test_only_consumers"] == true {
			flaggedRows++
		}
	}
	// Fixture-honesty guard: without live flagged rows the bar passes vacuously.
	if flaggedRows != calibratedEvidenceRows {
		t.Fatalf("flagged live rows = %d, want %d", flaggedRows, calibratedEvidenceRows)
	}

	plainBytes, flaggedBytes := twoCopyBytes(t, plain), twoCopyBytes(t, flagged)
	added := flaggedBytes - plainBytes
	ceiling := calibratedEvidenceRows * testOnlyBarBytesPerRow
	t.Logf("calibrated live rows: unflagged est2x=%d, flagged est2x=%d (%.1f%% of %d), flag adds %d bytes for %d rows, ceiling %d, class=%s",
		plainBytes, flaggedBytes, float64(flaggedBytes)*100/float64(defaultToolResponseByteBudget),
		defaultToolResponseByteBudget, added, flaggedRows, ceiling, replyClassName(replyClass(flagged)))
	if added <= 0 || added > ceiling {
		t.Errorf("flag added %d bytes, want 0 < added <= %d", added, ceiling)
	}
	if flaggedBytes > defaultToolResponseByteBudget || replyClass(flagged) != 0 {
		t.Errorf("flagged reply est2x=%d class=%s, want within %d and structuredContent delivered",
			flaggedBytes, replyClassName(replyClass(flagged)), defaultToolResponseByteBudget)
	}
	if replyClass(plain) != 0 {
		t.Errorf("unflagged reply class = %s, want delivered", replyClassName(replyClass(plain)))
	}
}
