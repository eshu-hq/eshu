// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/status/summary"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/workflow/snapshot"
)

// coordQueryer answers the coordinator's live statements with one collector
// instance and one claim row, the coordinator_snapshot row read, and the
// summary clock, and counts how many live coordinator statements ran.
type coordQueryer struct {
	installed bool
	row       []any // the stored coordinator_snapshot row; nil means none
	clockErr  error
	liveRuns  atomic.Int32
	modelRead atomic.Int32
	clockRead atomic.Int32
}

func (q *coordQueryer) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	switch {
	case strings.Contains(query, "to_regclass('status_summary_snapshots')"):
		q.clockRead.Add(1)
		if q.clockErr != nil {
			return nil, q.clockErr
		}
		return &readerRows{rows: [][]any{{sourceTestNow, q.installed}}}, nil
	case strings.Contains(query, "FROM status_summary_snapshots"):
		if len(args) == 1 && args[0] == summary.ModelCoordinatorSnapshot {
			q.modelRead.Add(1)
			if q.row == nil {
				return &readerRows{}, nil
			}
			return &readerRows{rows: [][]any{q.row}}, nil
		}
		return &readerRows{}, nil
	case strings.Contains(query, "configuration->'scan_interval'"):
		q.liveRuns.Add(1)
		return &readerRows{rows: [][]any{
			{"inst-1", "git", "continuous", true, false, true, "Git", sourceTestAsOf, sourceTestAsOf, nil, nil, "{}"},
			// A component-activated instance: the live read marks it
			// extension-hosted (#8951) and the stored row must keep that.
			{"inst-ext", "git", "continuous", true, false, true, "Component", sourceTestAsOf, sourceTestAsOf, nil, nil, `{"component_id":"acme.jira"}`},
		}}, nil
	case strings.Contains(query, "WITH workflow_collector_backpressure"):
		q.liveRuns.Add(1)
		return &readerRows{}, nil
	case strings.Contains(query, "AS active_claim_count") && !strings.Contains(query, "FROM workflow_claims AS claim"):
		q.liveRuns.Add(1)
		return &readerRows{rows: [][]any{{int64(3), int64(1), float64(40)}}}, nil
	case isOtherCoordinatorStatement(query):
		q.liveRuns.Add(1)
		return &readerRows{}, nil
	default:
		return &readerRows{}, nil
	}
}

// isOtherCoordinatorStatement matches the coordinator's remaining live
// statements by text that no other status read shares, so liveRuns counts the
// coordinator read and nothing else in the snapshot.
func isOtherCoordinatorStatement(query string) bool {
	for _, marker := range []string{
		"FROM workflow_runs\nGROUP BY status",
		"FROM workflow_work_items\nGROUP BY status",
		"FROM workflow_run_completeness\nGROUP BY status",
		"recent_failed_runs",
		"last_failure_class,\n    COUNT(*) AS count\nFROM workflow_work_items",
	} {
		if strings.Contains(query, marker) {
			return true
		}
	}
	return false
}

func coordStoredRow(t *testing.T, asOf time.Time, sha string, snap *statuspkg.CoordinatorSnapshot) []any {
	t.Helper()
	entries, err := snapshot.Encode(snap)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := summary.EncodeEntries(entries)
	if err != nil {
		t.Fatal(err)
	}
	row := storedRow(asOf, summary.SchemaVersion, sha, len(entries), string(encoded))
	row[0] = summary.ModelCoordinatorSnapshot
	return row
}

// liveCoordinator is what the live coordinator statements return for coordQueryer.
func liveCoordinator(t *testing.T) *statuspkg.CoordinatorSnapshot {
	t.Helper()
	q := &coordQueryer{installed: true}
	got := readSnapshot(t, readerStore(q, false)).Coordinator
	if got == nil || len(got.CollectorInstances) != 2 || !got.CollectorInstances[1].ExtensionHosted || got.ActiveClaims != 3 {
		t.Fatalf("fixture live coordinator = %#v", got)
	}
	return got
}

func TestCoordinatorReaderOffRunsOnlyTheLiveStatements(t *testing.T) {
	t.Parallel()
	q := &coordQueryer{installed: true, row: coordStoredRow(t, sourceTestAsOf.Add(-5*time.Second), pgstatus.CoordinatorSummarySourceSHA256(), liveCoordinator(t))}
	got := readSnapshot(t, readerStore(q, false))
	if q.modelRead.Load() != 0 || q.clockRead.Load() != 0 {
		t.Fatalf("flag off read the stored row %d times, clock %d times; want none", q.modelRead.Load(), q.clockRead.Load())
	}
	if q.liveRuns.Load() == 0 {
		t.Fatal("flag off ran no live coordinator statement")
	}
	if src := got.CoordinatorSource; src.Source != statuspkg.ActiveWorkSourceLive || src.Reason != statuspkg.ActiveWorkReasonFlagOff ||
		!src.AsOf.Equal(sourceTestNow) || src.Age != 0 || src.Stale {
		t.Fatalf("coordinator source = %+v, want live/flag_off at the snapshot clock", src)
	}
}

func TestCoordinatorReaderServesAFreshRowWithNoLiveStatementAndAgedDurations(t *testing.T) {
	t.Parallel()
	stored := liveCoordinator(t)
	stored.OldestPendingAge = 40 * time.Second
	storedAt := sourceTestNow.Add(-12 * time.Second)
	q := &coordQueryer{installed: true, row: coordStoredRow(t, storedAt, pgstatus.CoordinatorSummarySourceSHA256(), stored)}
	got := readSnapshot(t, readerStore(q, true))

	if n := q.liveRuns.Load(); n != 0 {
		t.Fatalf("%d live coordinator statements ran beside a fresh stored row; the two must never mix", n)
	}
	if src := got.CoordinatorSource; src.Source != statuspkg.ActiveWorkSourceModel || src.Reason != statuspkg.ActiveWorkReasonFresh ||
		!src.AsOf.Equal(storedAt) || src.Age != 12*time.Second || src.Stale {
		t.Fatalf("coordinator source = %+v, want model/fresh at %v age 12s", src, storedAt)
	}
	want := liveCoordinator(t)
	want.OldestPendingAge = 52 * time.Second
	if !reflect.DeepEqual(got.Coordinator, want) {
		t.Fatalf("coordinator\n got: %#v\nwant: %#v", got.Coordinator, want)
	}
}

func TestCoordinatorReaderFallsBackWholeWithATypedReason(t *testing.T) {
	t.Parallel()
	good := liveCoordinator(t)
	cases := []struct {
		name   string
		q      func() *coordQueryer
		reason string
	}{
		{"missing", func() *coordQueryer { return &coordQueryer{installed: true} }, statuspkg.ActiveWorkReasonMissing},
		{"not installed", func() *coordQueryer { return &coordQueryer{installed: false} }, statuspkg.ActiveWorkReasonNotInstalled},
		{"stale", func() *coordQueryer {
			return &coordQueryer{installed: true, row: coordStoredRow(t, sourceTestNow.Add(-34*time.Second), pgstatus.CoordinatorSummarySourceSHA256(), good)}
		}, statuspkg.ActiveWorkReasonStale},
		{"version", func() *coordQueryer {
			return &coordQueryer{installed: true, row: coordStoredRow(t, sourceTestNow.Add(-5*time.Second), "other-statement-digest", good)}
		}, statuspkg.ActiveWorkReasonVersion},
		{"decode", func() *coordQueryer {
			row := coordStoredRow(t, sourceTestNow.Add(-5*time.Second), pgstatus.CoordinatorSummarySourceSHA256(), good)
			row[7] = []byte(`[["mystery",1,"{}"]]`)
			row[6] = int64(1)
			return &coordQueryer{installed: true, row: row}
		}, statuspkg.ActiveWorkReasonDecode},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			q := tc.q()
			got := readSnapshot(t, readerStore(q, true))
			if src := got.CoordinatorSource; src.Source != statuspkg.ActiveWorkSourceLiveFallback || src.Reason != tc.reason {
				t.Fatalf("coordinator source = %+v, want live_fallback/%s", src, tc.reason)
			}
			if q.liveRuns.Load() == 0 {
				t.Fatal("fallback ran no live coordinator statement")
			}
			if !reflect.DeepEqual(got.Coordinator, liveCoordinator(t)) {
				t.Fatalf("fallback must answer with the whole live snapshot, got %#v", got.Coordinator)
			}
		})
	}
}

func TestCoordinatorReaderDatabaseErrorFailsTheRead(t *testing.T) {
	t.Parallel()
	q := &coordQueryer{installed: true, clockErr: errors.New("connection reset")}
	_, err := readerStore(q, true).ReadStatusSnapshotFiltered(context.Background(), sourceTestNow, statuspkg.FullSnapshotSelection())
	if err == nil || !strings.Contains(err.Error(), "connection reset") {
		t.Fatalf("error = %v, want the database error, not a fallback", err)
	}
}

func TestCoordinatorScrapeNeverRunsALiveStatement(t *testing.T) {
	t.Parallel()
	good := liveCoordinator(t)
	selection := scrapeSelection()
	read := func(q *coordQueryer) statuspkg.RawSnapshot {
		t.Helper()
		snap, err := readerStore(q, true).ReadStatusSnapshotFiltered(context.Background(), sourceTestNow, selection)
		if err != nil {
			t.Fatal(err)
		}
		return snap
	}

	fresh := &coordQueryer{installed: true, row: coordStoredRow(t, sourceTestNow.Add(-4*time.Second), pgstatus.CoordinatorSummarySourceSHA256(), good)}
	snap := read(fresh)
	if src := snap.CoordinatorSource; src.Source != statuspkg.ActiveWorkSourceModel || src.Stale || src.Age != 4*time.Second {
		t.Fatalf("fresh scrape source = %+v", src)
	}
	if fresh.liveRuns.Load() != 0 || snap.Coordinator == nil || snap.Coordinator.ActiveClaims != 3 {
		t.Fatalf("fresh scrape ran %d live statements, coordinator %#v", fresh.liveRuns.Load(), snap.Coordinator)
	}

	staleRow := coordStoredRow(t, sourceTestNow.Add(-300*time.Second), pgstatus.CoordinatorSummarySourceSHA256(), good)
	stale := &coordQueryer{installed: true, row: staleRow}
	snap = read(stale)
	if src := snap.CoordinatorSource; src.Source != statuspkg.ActiveWorkSourceLastRow || !src.Stale || src.Age != 300*time.Second {
		t.Fatalf("stale scrape source = %+v, want last_row, stale, age 300s", src)
	}
	if stale.liveRuns.Load() != 0 || snap.Coordinator == nil || snap.Coordinator.ActiveClaims != 3 {
		t.Fatalf("stale scrape ran %d live statements, coordinator %#v: it must serve the stored row aged to the read", stale.liveRuns.Load(), snap.Coordinator)
	}

	missing := &coordQueryer{installed: true}
	snap = read(missing)
	if src := snap.CoordinatorSource; src.Source != statuspkg.ActiveWorkSourceZero || !src.Stale {
		t.Fatalf("missing scrape source = %+v, want zero, stale", src)
	}
	if missing.liveRuns.Load() != 0 || snap.Coordinator != nil {
		t.Fatalf("missing scrape ran %d live statements, coordinator %#v; want none and nil", missing.liveRuns.Load(), snap.Coordinator)
	}
}
