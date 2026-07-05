// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// TestWriteDeferredBackfillSkipsReadinessWhenGenerationAdvanced is the #3725
// regression guard for the codex P1. The #3710 deferred backfill no longer shares
// one snapshot between the partition read and the per-scope fact load and the
// readiness write: a repository that commits a new active generation between the
// partition snapshot and its own per-scope query loads no facts under the old
// generation (the per-scope query's latest_generations join rejects the stale
// generation), yet writeDeferredBackfillBatch re-reads the new generation under
// the repo lock. Publishing readiness for that new generation would mark it
// backward-evidence-committed with no relationship evidence and reopen deployment
// mapping with no guaranteed repair pass.
//
// The generation-consistency guard must skip any repository whose scope's snapshot
// generation differs from the generation current at lock time (or whose scope was
// absent from the snapshot), and publish only repositories whose generation is
// unchanged. A nil snapshot (no fact load ran — no anchors or no partitions)
// disables the guard so the legacy publish-for-every-active-repository contract
// holds. Without the guard every case publishes one readiness row, so the
// skip cases prove the guard fires.
func TestWriteDeferredBackfillSkipsReadinessWhenGenerationAdvanced(t *testing.T) {
	t.Parallel()

	// repo-a in scope-a whose active generation under the batch lock is gen-new.
	activeGen := [][]any{{"repo-a", "scope-a", "gen-new"}}

	cases := []struct {
		name      string
		snapshot  map[string]string
		wantReady int
	}{
		{
			// Generation advanced after the fact-load snapshot: skip readiness so
			// the next maintenance pass processes gen-new.
			name:      "generation_advanced_skips_readiness",
			snapshot:  map[string]string{"scope-a": "gen-old"},
			wantReady: 0,
		},
		{
			// Generation unchanged since the snapshot: publish readiness.
			name:      "generation_unchanged_publishes_readiness",
			snapshot:  map[string]string{"scope-a": "gen-new"},
			wantReady: 1,
		},
		{
			// Scope absent from the snapshot (created after the snapshot): its facts
			// were never loaded, so skip readiness.
			name:      "scope_absent_from_snapshot_skips_readiness",
			snapshot:  map[string]string{"scope-other": "gen-new"},
			wantReady: 0,
		},
		{
			// No fact load ran: the guard is disabled and readiness publishes for
			// every active repository (legacy contract).
			name:      "nil_snapshot_disables_guard_publishes_all",
			snapshot:  nil,
			wantReady: 1,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			store := NewIngestionStore(&concurrencyProbeDB{activeGenRows: activeGen})
			store.Now = func() time.Time { return time.Unix(0, 0).UTC() }

			readiness, err := store.writeDeferredBackfillInBatches(
				context.Background(),
				map[string][]relationships.EvidenceFact{},
				tc.snapshot,
				"",
				nil,
			)
			if err != nil {
				t.Fatalf("writeDeferredBackfillInBatches() error = %v, want nil", err)
			}
			if readiness != tc.wantReady {
				t.Fatalf("published %d readiness rows, want %d", readiness, tc.wantReady)
			}
		})
	}
}
