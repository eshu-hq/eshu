// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/workflow/snapshot"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// keyedFakeDB answers a query with the rows registered for the first key its
// text contains, and with no rows otherwise, so the coordinator snapshot's
// many statements need no order script.
type keyedFakeDB struct {
	responses map[string][][]any
	queries   []string
}

func (f *keyedFakeDB) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	f.queries = append(f.queries, query)
	for key, rows := range f.responses {
		if strings.Contains(query, key) {
			return &fakeRows{rows: rows}, nil
		}
	}
	return &fakeRows{}, nil
}

// overdueEngagementDB scripts one source engaged two hours ago (past the
// 30-minute MaxEngagement) whose queue is still deep and old.
func overdueEngagementDB(now time.Time) *keyedFakeDB {
	return &keyedFakeDB{responses: map[string][][]any{
		"FROM workflow_claim_pacing": {{"vulnerability_intelligence", true, now.Add(-2 * time.Hour)}},
		"rows_past_engage_age":       {{"vulnerability_intelligence", float64(7200), int64(400), int64(400), int64(400)}},
	}}
}

// TestCoordinatorSummaryStoresStuckWhenThresholdsAreConfigured proves #10882
// F7 on the stored-summary path: a writer that holds the
// ESHU_SOURCE_BACKPRESSURE_* thresholds stores the source_pacing rows, with
// stuck=true for an overdue engagement, and a writer without them stores the
// rows with stuck absent. Before this the stored snapshot carried no
// source_pacing section at all, so the rows vanished from the summary path.
func TestCoordinatorSummaryStoresStuckWhenThresholdsAreConfigured(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 22, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name      string
		cfg       workflow.SourceBackpressureConfig
		wantStuck bool
	}{
		{"configured", sourceBackpressureTestConfig(), true},
		{"zero config", workflow.SourceBackpressureConfig{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			entries, err := ReadCoordinatorSummaryEntriesWithConfig(context.Background(), overdueEngagementDB(now), now, tc.cfg)
			if err != nil {
				t.Fatalf("ReadCoordinatorSummaryEntriesWithConfig() error = %v, want nil", err)
			}
			decoded, err := snapshot.Decode(entries)
			if err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			if decoded == nil || len(decoded.SourcePacing) != 1 {
				t.Fatalf("stored source pacing rows = %+v, want 1 row", decoded)
			}
			row := decoded.SourcePacing[0]
			if !row.Slowed || row.SourceSystem != "vulnerability_intelligence" {
				t.Fatalf("stored row = %+v, want slowed vulnerability_intelligence", row)
			}
			if row.Stuck != tc.wantStuck {
				t.Fatalf("stored stuck = %v, want %v", row.Stuck, tc.wantStuck)
			}
		})
	}
}

// TestCoordinatorSummarySourceSHAFollowsSourcePacingStatements pins that the
// stored-summary digest covers the two statements the new section reads, so a
// row written by a build without the section is never decoded by one that
// has it (a rolling upgrade falls back to the live read instead).
func TestCoordinatorSummarySourceSHAFollowsSourcePacingStatements(t *testing.T) {
	t.Parallel()

	if CoordinatorSummarySourceSHA256() == "" {
		t.Fatal("CoordinatorSummarySourceSHA256() is empty")
	}
	covered := coordinatorSummaryStatements()
	for _, want := range []string{sourceClaimableSignalQuery, sourceBackpressureLatchQuery} {
		found := false
		for _, statement := range covered {
			if statement == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("coordinator summary digest does not cover a source-pacing statement:\n%s", want)
		}
	}
}

// TestStatusStoreReadRawSnapshotScansSourceSignalsWhenLatchEngaged pins the
// other side of the #10882 F3 gate on the whole status read: with an engaged
// pacing latch and no thresholds on the reading process, the grouped
// claimable-signal scan runs, so the snapshot costs 30 round trips where the
// leg-off fixture in TestStatusStoreReadRawSnapshot costs 29.
func TestStatusStoreReadRawSnapshotScansSourceSignalsWhenLatchEngaged(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 22, 12, 0, 0, 0, time.UTC)
	engaged := &keyedFakeDB{responses: map[string][][]any{
		"FROM workflow_claim_pacing": {{"vulnerability_intelligence", true, now.Add(-time.Hour)}},
	}}
	if _, err := NewStatusStore(engaged).ReadRawSnapshot(context.Background(), now); err != nil {
		t.Fatalf("ReadRawSnapshot() with an engaged latch error = %v, want nil", err)
	}
	released := &keyedFakeDB{}
	if _, err := NewStatusStore(released).ReadRawSnapshot(context.Background(), now); err != nil {
		t.Fatalf("ReadRawSnapshot() with no latch error = %v, want nil", err)
	}
	if got, want := len(engaged.queries)-len(released.queries), 1; got != want {
		t.Fatalf("engaged latch added %d status queries over the leg-off read, want %d (the grouped scan)", got, want)
	}
	scans := 0
	for _, query := range engaged.queries {
		if strings.Contains(query, "rows_past_engage_age") {
			scans++
		}
	}
	for _, query := range released.queries {
		if strings.Contains(query, "rows_past_engage_age") {
			t.Fatal("the leg-off status read ran the grouped claimable scan")
		}
	}
	if scans != 1 {
		t.Fatalf("engaged-latch status read ran the grouped scan %d times, want 1", scans)
	}
}
