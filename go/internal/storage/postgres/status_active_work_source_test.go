// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/status/summary"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

var (
	sourceTestNow  = time.Date(2026, 10, 6, 12, 0, 30, 0, time.UTC)
	sourceTestAsOf = time.Date(2026, 10, 6, 12, 0, 30, 0, time.UTC)
)

const (
	queueEntryJSON    = `{"total_count":4,"outstanding_count":2,"pending_count":2,"in_flight_count":0,"retrying_count":0,"succeeded_count":2,"dead_letter_count":0,"failed_count":0,"provenance_edge_identity_upgrade_applied":false,"provenance_edge_identity_upgrade_required":0,"oldest_outstanding_age_seconds":5,"overdue_claim_count":0}`
	backlogEntryJSON  = `{"domain":"repo_dependency","outstanding_count":2,"in_flight_count":0,"retrying_count":0,"dead_letter_count":0,"failed_count":0,"oldest_outstanding_age_seconds":7.5}`
	blockageEntryJSON = `{"stage":"reducer","domain":"d","conflict_domain":"c","conflict_key":"k","blocked_count":1,"oldest_blocked_age_seconds":3}`
	stageEntryJSON    = `{"stage":"reducer","status":"pending","count":2}`
)

// sourceQueryer answers the summary reader's statements and the live
// active-work statement, and records which of them ran.
type sourceQueryer struct {
	mu          sync.Mutex
	installed   bool
	row         []any // the stored row, as Read scans it; nil means no row
	clockErr    error
	liveGate    chan struct{} // when set, the live statement waits on it
	liveRuns    atomic.Int32
	modelReads  atomic.Int32
	clockReads  atomic.Int32
	liveStage   int
	labels      []string
	liveStarted chan struct{}
}

func (q *sourceQueryer) QueryContext(ctx context.Context, query string, _ ...any) (db.Rows, error) {
	switch {
	case strings.Contains(query, "to_regclass('status_summary_snapshots')"):
		q.clockReads.Add(1)
		if q.clockErr != nil {
			return nil, q.clockErr
		}
		return &fakeRows{rows: [][]any{{sourceTestNow, q.installed}}}, nil
	case strings.Contains(query, "FROM status_summary_snapshots"):
		q.modelReads.Add(1)
		q.mu.Lock()
		q.labels = append(q.labels, db.QuerySummaryFromContext(ctx))
		q.mu.Unlock()
		if q.row == nil {
			return &fakeRows{}, nil
		}
		return &fakeRows{rows: [][]any{q.row}}, nil
	case query == activeWorkSummaryQuery:
		q.liveRuns.Add(1)
		if q.liveStarted != nil {
			select {
			case q.liveStarted <- struct{}{}:
			default:
			}
		}
		if q.liveGate != nil {
			<-q.liveGate
		}
		return &fakeRows{rows: [][]any{
			{"stage", int64(1), fmt.Sprintf(`{"stage":"reducer","status":"pending","count":%d}`, 100+q.liveStage)},
			{"queue", int64(1), strings.Replace(queueEntryJSON, `"oldest_outstanding_age_seconds":5`, `"oldest_outstanding_age_seconds":1`, 1)},
		}}, nil
	default:
		return &fakeRows{}, nil
	}
}

// storedRow builds the row Read scans for entries written at asOf.
func storedRow(asOf time.Time, schemaVersion int, sha string, rowCount int, payload string) []any {
	return []any{
		summary.ModelActiveWorkSummary, int64(schemaVersion), sha, asOf, asOf, float64(300), int64(rowCount), []byte(payload),
	}
}

func encodedEntries(t *testing.T, entries ...summary.Entry) string {
	t.Helper()
	encoded, err := summary.EncodeEntries(entries)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func goodEntries() []summary.Entry {
	return []summary.Entry{
		{Section: "backlog", Ordinal: 1, JSON: backlogEntryJSON},
		{Section: "blockage", Ordinal: 1, JSON: blockageEntryJSON},
		{Section: "queue", Ordinal: 1, JSON: queueEntryJSON},
		{Section: "stage", Ordinal: 1, JSON: stageEntryJSON},
	}
}

func readerStore(q db.Queryer, enabled bool) StatusStore {
	return NewStatusStore(q).WithSummaryRead(summary.ReadConfig{Enabled: enabled, StaleAfter: 33 * time.Second})
}

func readSnapshot(t *testing.T, store StatusStore) statuspkg.RawSnapshot {
	t.Helper()
	snapshot, err := store.ReadStatusSnapshotFiltered(context.Background(), sourceTestNow, statuspkg.FullSnapshotSelection())
	if err != nil {
		t.Fatalf("ReadStatusSnapshotFiltered() error = %v", err)
	}
	return snapshot
}

func TestReaderOffRunsOnlyTheLiveStatement(t *testing.T) {
	t.Parallel()

	q := &sourceQueryer{installed: true, row: storedRow(sourceTestAsOf, summary.SchemaVersion, ActiveWorkSummarySourceSHA256(), 4, encodedEntries(t, goodEntries()...))}
	snapshot := readSnapshot(t, readerStore(q, false))

	if q.clockReads.Load() != 0 || q.modelReads.Load() != 0 {
		t.Fatalf("flag off issued %d clock and %d model reads, want none", q.clockReads.Load(), q.modelReads.Load())
	}
	if q.liveRuns.Load() != 1 {
		t.Fatalf("live statement ran %d times, want 1", q.liveRuns.Load())
	}
	got := snapshot.ActiveWorkSource
	if got.Source != statuspkg.ActiveWorkSourceLive || got.Reason != statuspkg.ActiveWorkReasonFlagOff || got.Stale {
		t.Fatalf("source = %+v, want live/flag_off", got)
	}
	if !got.AsOf.Equal(sourceTestNow) || got.Age != 0 {
		t.Fatalf("as_of/age = %v/%v, want the live clock and zero age", got.AsOf, got.Age)
	}
}

func TestReaderOnServesAFreshRowWithoutTheLiveStatement(t *testing.T) {
	t.Parallel()

	storedAt := sourceTestNow.Add(-10 * time.Second)
	q := &sourceQueryer{installed: true, row: storedRow(storedAt, summary.SchemaVersion, ActiveWorkSummarySourceSHA256(), 4, encodedEntries(t, goodEntries()...))}
	snapshot := readSnapshot(t, readerStore(q, true))

	if q.liveRuns.Load() != 0 {
		t.Fatalf("live statement ran %d times beside a fresh stored row; the two must never mix", q.liveRuns.Load())
	}
	got := snapshot.ActiveWorkSource
	if got.Source != statuspkg.ActiveWorkSourceModel || got.Reason != statuspkg.ActiveWorkReasonFresh {
		t.Fatalf("source = %+v, want model/fresh", got)
	}
	if !got.AsOf.Equal(storedAt) || got.Age != 10*time.Second {
		t.Fatalf("as_of/age = %v/%v, want %v/10s", got.AsOf, got.Age, storedAt)
	}
	// The ages are the stored ages advanced by the row's 10 s age.
	if snapshot.Queue.OldestOutstandingAge != 15*time.Second {
		t.Fatalf("queue oldest age = %v, want 5s stored + 10s age", snapshot.Queue.OldestOutstandingAge)
	}
	if len(snapshot.DomainBacklogs) != 1 || snapshot.DomainBacklogs[0].OldestAge != 17500*time.Millisecond {
		t.Fatalf("backlog = %+v, want oldest age 7.5s + 10s", snapshot.DomainBacklogs)
	}
	if len(snapshot.QueueBlockages) != 1 || snapshot.QueueBlockages[0].OldestAge != 13*time.Second {
		t.Fatalf("blockages = %+v, want oldest age 3s + 10s", snapshot.QueueBlockages)
	}
	// Counts stay as stored.
	if snapshot.Queue.Outstanding != 2 || len(snapshot.StageCounts) != 1 || snapshot.StageCounts[0].Count != 2 {
		t.Fatalf("counts changed: queue=%+v stages=%+v", snapshot.Queue, snapshot.StageCounts)
	}
}

func TestReaderOnFallsBackToTheLiveStatementWithATypedReason(t *testing.T) {
	t.Parallel()

	sha := ActiveWorkSummarySourceSHA256()
	good := encodedEntries(t, goodEntries()...)
	badSection := encodedEntries(t, summary.Entry{Section: "mystery", Ordinal: 1, JSON: `{}`})
	for _, tc := range []struct {
		name      string
		q         *sourceQueryer
		reason    string
		wantStale bool
	}{
		{"table not installed", &sourceQueryer{installed: false}, "not_installed", false},
		{"row missing", &sourceQueryer{installed: true}, "missing", false},
		{"schema version", &sourceQueryer{installed: true, row: storedRow(sourceTestAsOf.Add(-time.Second), summary.SchemaVersion+1, sha, 4, good)}, "version", false},
		{"source digest", &sourceQueryer{installed: true, row: storedRow(sourceTestAsOf.Add(-time.Second), summary.SchemaVersion, "other", 4, good)}, "version", false},
		{"row count", &sourceQueryer{installed: true, row: storedRow(sourceTestAsOf.Add(-time.Second), summary.SchemaVersion, sha, 9, good)}, "row_count", false},
		{"stale", &sourceQueryer{installed: true, row: storedRow(sourceTestNow.Add(-34*time.Second), summary.SchemaVersion, sha, 4, good)}, "stale", true},
		{"payload not decodable", &sourceQueryer{installed: true, row: storedRow(sourceTestAsOf.Add(-time.Second), summary.SchemaVersion, sha, 1, `{"x":1}`)}, "decode", false},
		{"section the production decoder rejects", &sourceQueryer{installed: true, row: storedRow(sourceTestAsOf.Add(-time.Second), summary.SchemaVersion, sha, 1, badSection)}, "decode", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			snapshot := readSnapshot(t, readerStore(tc.q, true))
			if tc.q.liveRuns.Load() != 1 {
				t.Fatalf("live statement ran %d times, want 1", tc.q.liveRuns.Load())
			}
			got := snapshot.ActiveWorkSource
			if got.Source != statuspkg.ActiveWorkSourceLiveFallback || got.Reason != tc.reason || got.Stale != tc.wantStale {
				t.Fatalf("source = %+v, want live_fallback/%s stale=%v", got, tc.reason, tc.wantStale)
			}
			// The answer is entirely the live one: nothing from the stored row.
			if len(snapshot.StageCounts) != 1 || snapshot.StageCounts[0].Count != 100 || len(snapshot.QueueBlockages) != 0 || len(snapshot.DomainBacklogs) != 0 {
				t.Fatalf("a fallback mixed stored data into the live answer: stages=%+v blockages=%+v backlogs=%+v",
					snapshot.StageCounts, snapshot.QueueBlockages, snapshot.DomainBacklogs)
			}
			if !got.AsOf.Equal(sourceTestNow) || got.Age != 0 {
				t.Fatalf("a fallback reports the live clock and zero age, got %v/%v", got.AsOf, got.Age)
			}
		})
	}
}

func TestReaderOnFailsClosedOnADatabaseErrorReadingTheRow(t *testing.T) {
	t.Parallel()

	q := &sourceQueryer{installed: true, clockErr: fmt.Errorf("conflict with recovery")}
	_, err := readerStore(q, true).ReadStatusSnapshotFiltered(context.Background(), sourceTestNow, statuspkg.FullSnapshotSelection())
	if err == nil || !strings.Contains(err.Error(), "conflict with recovery") {
		t.Fatalf("error = %v, want the database error returned, not a quiet fallback", err)
	}
	if q.liveRuns.Load() != 0 {
		t.Fatalf("the live statement ran after a failed row read; a wrong answer must not hide behind a fallback")
	}
}

func TestReaderConfigErrorFailsReadsNamingTheVariable(t *testing.T) {
	t.Setenv(summary.ReadEnabledEnv, "true")
	t.Setenv(summary.StaleAfterEnv, "soon")
	q := &sourceQueryer{installed: true}
	store := NewStatusStore(q)
	_, err := store.ReadStatusSnapshotFiltered(context.Background(), sourceTestNow, statuspkg.FullSnapshotSelection())
	if err == nil || !strings.Contains(err.Error(), summary.StaleAfterEnv) {
		t.Fatalf("error = %v, want a startup-style error naming %s", err, summary.StaleAfterEnv)
	}
	if q.liveRuns.Load() != 0 || q.clockReads.Load() != 0 {
		t.Fatal("a misconfigured reader issued statements")
	}
}

func TestNewStatusStoreReadsTheEnvironmentFlag(t *testing.T) {
	t.Setenv(summary.ReadEnabledEnv, "true")
	t.Setenv(summary.StaleAfterEnv, "45s")
	storedAt := sourceTestNow.Add(-40 * time.Second)
	q := &sourceQueryer{installed: true, row: storedRow(storedAt, summary.SchemaVersion, ActiveWorkSummarySourceSHA256(), 4, encodedEntries(t, goodEntries()...))}
	snapshot := readSnapshot(t, NewStatusStore(q))
	if got := snapshot.ActiveWorkSource; got.Source != statuspkg.ActiveWorkSourceModel || got.Age != 40*time.Second {
		t.Fatalf("source = %+v, want a 40s old row served under the 45s limit from the environment", got)
	}
}

func TestReaderSharesOneLiveStatementAcrossConcurrentFallbacks(t *testing.T) {
	t.Parallel()

	const requests = 50
	q := &sourceQueryer{installed: true, liveGate: make(chan struct{}), liveStarted: make(chan struct{}, 1)}
	store := readerStore(q, true)
	var wg sync.WaitGroup
	sources := make([]statuspkg.ActiveWorkSource, requests)
	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			snapshot, err := store.ReadStatusSnapshotFiltered(context.Background(), sourceTestNow, statuspkg.FullSnapshotSelection())
			if err != nil {
				t.Errorf("request %d: %v", i, err)
				return
			}
			sources[i] = snapshot.ActiveWorkSource
		}()
	}
	<-q.liveStarted
	deadline := time.Now().Add(10 * time.Second)
	for store.liveFlight.Waiting(summary.ModelActiveWorkSummary) < requests-1 {
		if time.Now().After(deadline) {
			t.Fatalf("only %d of %d requests joined the in-flight live statement", store.liveFlight.Waiting(summary.ModelActiveWorkSummary), requests-1)
		}
		time.Sleep(time.Millisecond)
	}
	close(q.liveGate)
	wg.Wait()

	if got := q.liveRuns.Load(); got != 1 {
		t.Fatalf("live statement ran %d times for %d concurrent fallbacks, want exactly 1 per process", got, requests)
	}
	for i, source := range sources {
		if source.Source != statuspkg.ActiveWorkSourceLiveFallback || source.Reason != statuspkg.ActiveWorkReasonMissing {
			t.Fatalf("request %d source = %+v, want live_fallback/missing", i, source)
		}
	}
}

func TestReaderSharedLiveResultsAreIndependentCopies(t *testing.T) {
	t.Parallel()

	q := &sourceQueryer{installed: true, liveGate: make(chan struct{}), liveStarted: make(chan struct{}, 1)}
	store := readerStore(q, true)
	results := make([]statuspkg.RawSnapshot, 2)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], _ = store.ReadStatusSnapshotFiltered(context.Background(), sourceTestNow, statuspkg.FullSnapshotSelection())
		}()
	}
	<-q.liveStarted
	for store.liveFlight.Waiting(summary.ModelActiveWorkSummary) < 1 {
		time.Sleep(time.Millisecond)
	}
	close(q.liveGate)
	wg.Wait()
	results[0].StageCounts[0].Count = -1
	if results[1].StageCounts[0].Count == -1 {
		t.Fatal("two requests share one StageCounts backing array; a mutation by one changes the other")
	}
}

func TestReaderRecordsTheModelReadUnderItsOwnLabel(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatal(err)
	}
	storedAt := sourceTestNow.Add(-10 * time.Second)
	q := &sourceQueryer{installed: true, row: storedRow(storedAt, summary.SchemaVersion, ActiveWorkSummarySourceSHA256(), 4, encodedEntries(t, goodEntries()...))}
	store := readerStore(q, true)
	store.Instruments = instruments
	readSnapshot(t, store)

	if len(q.labels) != 1 || q.labels[0] != "active_work_summary_model" {
		t.Fatalf("the row read carried labels %q, want active_work_summary_model", q.labels)
	}
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatal(err)
	}
	reads := map[string]uint64{}
	var counted int64
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			switch data := m.Data.(type) {
			case metricdata.Histogram[float64]:
				if m.Name != "eshu_dp_status_snapshot_read_duration_seconds" {
					continue
				}
				for _, point := range data.DataPoints {
					read, _ := point.Attributes.Value(attribute.Key("read"))
					reads[read.AsString()] += point.Count
				}
			case metricdata.Sum[int64]:
				if m.Name == "eshu_dp_status_summary_read_total" {
					for _, point := range data.DataPoints {
						counted += point.Value
					}
				}
			}
		}
	}
	if reads["active_work_summary_model"] != 1 || reads["active_work_summary"] != 0 {
		t.Fatalf("read samples = %v, want one active_work_summary_model and no live active_work_summary", reads)
	}
	if counted != 1 {
		t.Fatalf("eshu_dp_status_summary_read_total = %d, want 1", counted)
	}
}

// TestAgeAdvanceReachesEveryDecodedDuration proves the summary package's age
// key table names the keys this package's decoder reads as durations: the
// aged entries, decoded by the production decoder, differ from the unaged
// decode by exactly the age in the three duration fields and nowhere else.
func TestAgeAdvanceReachesEveryDecodedDuration(t *testing.T) {
	t.Parallel()

	decode := func(entries []summary.Entry) activeWorkSummary {
		var out activeWorkSummary
		for _, entry := range entries {
			if err := out.add(entry.Section, entry.JSON); err != nil {
				t.Fatal(err)
			}
		}
		return out
	}
	base := goodEntries()
	aged, err := summary.AddAge(base, 20*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	before, after := decode(base), decode(aged)
	if got := after.Queue.OldestOutstandingAge - before.Queue.OldestOutstandingAge; got != 20*time.Second {
		t.Fatalf("queue age moved %v, want 20s", got)
	}
	if got := after.DomainBacklogs[0].OldestAge - before.DomainBacklogs[0].OldestAge; got != 20*time.Second {
		t.Fatalf("backlog age moved %v, want 20s", got)
	}
	if got := after.Blockages[0].OldestAge - before.Blockages[0].OldestAge; got != 20*time.Second {
		t.Fatalf("blockage age moved %v, want 20s", got)
	}
	after.Queue.OldestOutstandingAge = before.Queue.OldestOutstandingAge
	after.DomainBacklogs[0].OldestAge = before.DomainBacklogs[0].OldestAge
	after.Blockages[0].OldestAge = before.Blockages[0].OldestAge
	b1, _ := json.Marshal(before)
	b2, _ := json.Marshal(after)
	if string(b1) != string(b2) {
		t.Fatalf("aging changed something other than the three durations:\n%s\n%s", b1, b2)
	}
}
