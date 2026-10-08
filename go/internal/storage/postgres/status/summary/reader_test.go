// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/status/summary"
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

// readerRows is a scripted db.Rows: each row is a list of values that Scan
// assigns by destination type.
type readerRows struct {
	rows   [][]any
	cursor int
}

func (r *readerRows) Next() bool { r.cursor++; return r.cursor <= len(r.rows) }
func (r *readerRows) Err() error { return nil }
func (r *readerRows) Close() error {
	return nil
}

func (r *readerRows) Scan(dest ...any) error {
	row := r.rows[r.cursor-1]
	for i, target := range dest {
		switch d := target.(type) {
		case *string:
			*d = row[i].(string)
		case *bool:
			*d = row[i].(bool)
		case *int:
			*d = int(row[i].(int64))
		case *int64:
			*d = row[i].(int64)
		case *float64:
			*d = row[i].(float64)
		case *time.Time:
			*d = row[i].(time.Time)
		case *sql.NullTime:
			switch v := row[i].(type) {
			case sql.NullTime:
				*d = v
			case nil:
				*d = sql.NullTime{}
			case time.Time:
				*d = sql.NullTime{Time: v, Valid: true}
			default:
				return fmt.Errorf("readerRows: %T is not a time for a NullTime", v)
			}
		case *[]byte:
			*d = row[i].([]byte)
		default:
			return fmt.Errorf("readerRows: unsupported scan destination %T", target)
		}
	}
	return nil
}

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

	// tfRow is the stored terraform_state row (nil means none); tfSerials and
	// tfWarnings are the rows the live Terraform-state statements return.
	tfRow       []any
	tfSerials   [][]any
	tfWarnings  [][]any
	tfLiveRuns  atomic.Int32
	tfModelRead atomic.Int32
}

func (q *sourceQueryer) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	switch {
	case strings.Contains(query, "to_regclass('status_summary_snapshots')"):
		q.clockReads.Add(1)
		if q.clockErr != nil {
			return nil, q.clockErr
		}
		return &readerRows{rows: [][]any{{sourceTestNow, q.installed}}}, nil
	case strings.Contains(query, "FROM status_summary_snapshots"):
		q.mu.Lock()
		q.labels = append(q.labels, db.QuerySummaryFromContext(ctx))
		q.mu.Unlock()
		row := q.row
		if len(args) == 1 && args[0] == summary.ModelTerraformState {
			q.tfModelRead.Add(1)
			row = q.tfRow
		} else {
			q.modelReads.Add(1)
		}
		if row == nil {
			return &readerRows{}, nil
		}
		return &readerRows{rows: [][]any{row}}, nil
	case strings.Contains(query, "ranked_generations"):
		q.tfLiveRuns.Add(1)
		return &readerRows{rows: q.tfSerials}, nil
	case strings.Contains(query, "raw_warning_rows"):
		q.tfLiveRuns.Add(1)
		return &readerRows{rows: q.tfWarnings}, nil
	case strings.Contains(query, "active_work_stage"):
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
		return &readerRows{rows: [][]any{
			{"stage", int64(1), fmt.Sprintf(`{"stage":"reducer","status":"pending","count":%d}`, 100+q.liveStage)},
			{"queue", int64(1), strings.Replace(queueEntryJSON, `"oldest_outstanding_age_seconds":5`, `"oldest_outstanding_age_seconds":1`, 1)},
		}}, nil
	default:
		return &readerRows{}, nil
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

func readerStore(q db.Queryer, enabled bool) pgstatus.StatusStore {
	return pgstatus.NewStatusStore(q).WithSummaryReader(
		pgstatus.NewStatusSummaryReaderWithConfig(summary.ReadConfig{Enabled: enabled, StaleAfter: 33 * time.Second}))
}

func readSnapshot(t *testing.T, store pgstatus.StatusStore) statuspkg.RawSnapshot {
	t.Helper()
	snapshot, err := store.ReadStatusSnapshotFiltered(context.Background(), sourceTestNow, statuspkg.FullSnapshotSelection())
	if err != nil {
		t.Fatalf("ReadStatusSnapshotFiltered() error = %v", err)
	}
	return snapshot
}

func TestReaderOffRunsOnlyTheLiveStatement(t *testing.T) {
	t.Parallel()

	q := &sourceQueryer{installed: true, row: storedRow(sourceTestAsOf, summary.SchemaVersion, pgstatus.ActiveWorkSummarySourceSHA256(), 4, encodedEntries(t, goodEntries()...))}
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
	q := &sourceQueryer{installed: true, row: storedRow(storedAt, summary.SchemaVersion, pgstatus.ActiveWorkSummarySourceSHA256(), 4, encodedEntries(t, goodEntries()...))}
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

	sha := pgstatus.ActiveWorkSummarySourceSHA256()
	good := encodedEntries(t, goodEntries()...)
	badSection := encodedEntries(t, summary.Entry{Section: "mystery", Ordinal: 1, JSON: `{}`})
	for _, tc := range []struct {
		name   string
		q      *sourceQueryer
		reason string
	}{
		{"table not installed", &sourceQueryer{installed: false}, "not_installed"},
		{"row missing", &sourceQueryer{installed: true}, "missing"},
		{"schema version", &sourceQueryer{installed: true, row: storedRow(sourceTestAsOf.Add(-time.Second), summary.SchemaVersion+1, sha, 4, good)}, "version"},
		{"schema version plus one with an object payload", &sourceQueryer{installed: true, row: storedRow(sourceTestAsOf.Add(-time.Second), summary.SchemaVersion+1, sha, 4, `{"future":"encoding"}`)}, "version"},
		{"schema version plus one with a four-element tuple payload", &sourceQueryer{installed: true, row: storedRow(sourceTestAsOf.Add(-time.Second), summary.SchemaVersion+1, sha, 1, `[["queue",1,"{}","extra"]]`)}, "version"},
		{"schema version plus one with a wrong row count", &sourceQueryer{installed: true, row: storedRow(sourceTestAsOf.Add(-time.Second), summary.SchemaVersion+1, sha, 9, good)}, "version"},
		{"digest changed with a wrong row count", &sourceQueryer{installed: true, row: storedRow(sourceTestAsOf.Add(-time.Second), summary.SchemaVersion, "other", 9, good)}, "version"},
		{"source digest", &sourceQueryer{installed: true, row: storedRow(sourceTestAsOf.Add(-time.Second), summary.SchemaVersion, "other", 4, good)}, "version"},
		{"row count", &sourceQueryer{installed: true, row: storedRow(sourceTestAsOf.Add(-time.Second), summary.SchemaVersion, sha, 9, good)}, "row_count"},
		{"stale", &sourceQueryer{installed: true, row: storedRow(sourceTestNow.Add(-34*time.Second), summary.SchemaVersion, sha, 4, good)}, "stale"},
		{"payload not decodable", &sourceQueryer{installed: true, row: storedRow(sourceTestAsOf.Add(-time.Second), summary.SchemaVersion, sha, 1, `{"x":1}`)}, "decode"},
		{"section the production decoder rejects", &sourceQueryer{installed: true, row: storedRow(sourceTestAsOf.Add(-time.Second), summary.SchemaVersion, sha, 1, badSection)}, "decode"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			snapshot := readSnapshot(t, readerStore(tc.q, true))
			if tc.q.liveRuns.Load() != 1 {
				t.Fatalf("live statement ran %d times, want 1", tc.q.liveRuns.Load())
			}
			got := snapshot.ActiveWorkSource
			if got.Source != statuspkg.ActiveWorkSourceLiveFallback || got.Reason != tc.reason || got.Stale {
				t.Fatalf("source = %+v, want live_fallback/%s with stale=false: the served data is live", got, tc.reason)
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

func TestInstrumentedStatusStoreReportsAnInvalidReaderConfigAtStartup(t *testing.T) {
	t.Setenv(summary.ReadEnabledEnv, "true")
	t.Setenv(summary.StaleAfterEnv, "soon")
	q := &sourceQueryer{installed: true}
	store := pgstatus.NewInstrumentedStatusStore(q, nil)
	if err := store.StartupError(); err == nil || !strings.Contains(err.Error(), summary.StaleAfterEnv) {
		t.Fatalf("StartupError() = %v, want an error naming %s so the runtime fails at startup", err, summary.StaleAfterEnv)
	}
	_, err := store.ReadStatusSnapshotFiltered(context.Background(), sourceTestNow, statuspkg.FullSnapshotSelection())
	if err == nil || !strings.Contains(err.Error(), summary.StaleAfterEnv) {
		t.Fatalf("read error = %v, want the same error: a misconfigured reader never serves", err)
	}
	if q.liveRuns.Load() != 0 || q.clockReads.Load() != 0 {
		t.Fatal("a misconfigured reader issued statements")
	}
}

func TestInstrumentedStatusStoreReadsTheEnvironmentOnce(t *testing.T) {
	t.Setenv(summary.ReadEnabledEnv, "true")
	t.Setenv(summary.StaleAfterEnv, "45s")
	storedAt := sourceTestNow.Add(-40 * time.Second)
	q := &sourceQueryer{installed: true, row: storedRow(storedAt, summary.SchemaVersion, pgstatus.ActiveWorkSummarySourceSHA256(), 4, encodedEntries(t, goodEntries()...))}
	store := pgstatus.NewInstrumentedStatusStore(q, nil)
	// A later environment change does not change a running process.
	t.Setenv(summary.ReadEnabledEnv, "false")
	snapshot := readSnapshot(t, store)
	if got := snapshot.ActiveWorkSource; got.Source != statuspkg.ActiveWorkSourceModel || got.Age != 40*time.Second {
		t.Fatalf("source = %+v, want a 40s old row served under the 45s limit resolved at construction", got)
	}
}

func TestNewStatusStoreReadsNoEnvironment(t *testing.T) {
	t.Setenv(summary.ReadEnabledEnv, "true")
	q := &sourceQueryer{installed: true}
	snapshot := readSnapshot(t, pgstatus.NewStatusStore(q))
	if got := snapshot.ActiveWorkSource; got.Source != statuspkg.ActiveWorkSourceLive || got.Reason != statuspkg.ActiveWorkReasonFlagOff {
		t.Fatalf("source = %+v: NewStatusStore is pure and the reader is off until one is attached", got)
	}
	if q.clockReads.Load() != 0 {
		t.Fatal("a store without a reader issued summary statements")
	}
}
