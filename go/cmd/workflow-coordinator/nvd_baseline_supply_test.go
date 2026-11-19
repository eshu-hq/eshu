// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/vulnerability"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/fake"
	vulnerabilitystore "github.com/eshu-hq/eshu/go/internal/storage/postgres/vulnerability"
)

// TestNVDBaselineCheckpointReaderMapsStoredRows proves the cmd-level reader
// hands the coordinator every field it plans and probes from: the resume
// position and progress counters plus the last scope and generation the
// drain probe keys on.
func TestNVDBaselineCheckpointReaderMapsStoredRows(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 20, 12, 0, 0, 0, time.UTC)
	_ = now
	database := &fake.ExecQueryer{QueryResponses: []fake.Rows{{Data: [][]any{{
		sql.NullInt64{Int64: 2002, Valid: true},
		sql.NullInt64{Int64: 946684800000000, Valid: true},
		sql.NullString{String: "CVE-2002-0001", Valid: true},
		sql.NullString{String: "sha", Valid: true},
		sql.NullInt64{Int64: 1009843200000000, Valid: true},
		sql.NullBool{Bool: false, Valid: true},
		sql.NullString{String: "scope-2002", Valid: true},
		sql.NullString{String: "gen-2002", Valid: true},
		sql.NullInt64{Int64: 10, Valid: true},
		sql.NullInt64{Int64: 100, Valid: true},
	}}}}}
	got, err := nvdBaselineCheckpointReader{database: database}.
		ReadNVDBaselineCheckpoints(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("ReadNVDBaselineCheckpoints() error = %v, want nil", err)
	}
	row, ok := got[2002]
	if !ok {
		t.Fatalf("ReadNVDBaselineCheckpoints() = %+v, want the 2002 row", got)
	}
	if row.Checkpoint.ResumeUnixMicro != 946684800000000 || row.Checkpoint.ResumeCVEID != "CVE-2002-0001" {
		t.Fatalf("ReadNVDBaselineCheckpoints() checkpoint = %+v, want the stored position", row.Checkpoint)
	}
	if row.Checkpoint.RecordsDone != 10 || row.Checkpoint.RecordsTotal != 100 || row.Checkpoint.Complete {
		t.Fatalf("ReadNVDBaselineCheckpoints() checkpoint = %+v, want done 10 of 100, not complete", row.Checkpoint)
	}
	if row.LastScopeID != "scope-2002" || row.LastGenerationID != "gen-2002" {
		t.Fatalf("ReadNVDBaselineCheckpoints() drain key = %+v, want scope-2002/gen-2002", row)
	}
}

// TestNVDBaselineCheckpointReaderPropagatesProbeFailure proves a failed
// checkpoint read reaches the coordinator as an error, never as an empty
// map the planner would replay from zero.
func TestNVDBaselineCheckpointReaderPropagatesProbeFailure(t *testing.T) {
	t.Parallel()

	probeErr := errors.New("connection reset by peer")
	database := &fake.ExecQueryer{QueryResponses: []fake.Rows{{FailWith: probeErr}}}
	got, err := nvdBaselineCheckpointReader{database: database}.
		ReadNVDBaselineCheckpoints(context.Background(), "run-1")
	if !errors.Is(err, probeErr) {
		t.Fatalf("ReadNVDBaselineCheckpoints() error = %v, want it to wrap %v", err, probeErr)
	}
	if len(got) != 0 {
		t.Fatalf("ReadNVDBaselineCheckpoints() = %+v, want no rows alongside the error", got)
	}
}

// TestNVDBaselineLatchStoreMapsStoredRow proves the cmd-level latch adapter
// hands the coordinator the full trip state, sample time, and revision the
// guarded write compares on.
func TestNVDBaselineLatchStoreMapsStoredRow(t *testing.T) {
	t.Parallel()

	sampled := time.Date(2026, time.November, 20, 11, 30, 0, 0, time.UTC)
	database := &fake.ExecQueryer{QueryResponses: []fake.Rows{{Data: [][]any{{
		sql.NullBool{Bool: false, Valid: true},
		sql.NullInt64{Int64: 0, Valid: true},
		sql.NullBool{Bool: false, Valid: true},
		sql.NullInt64{Int64: 3, Valid: true},
		sql.NullBool{Bool: false, Valid: true},
		sql.NullInt64{Int64: 3, Valid: true},
		sql.NullTime{Time: sampled, Valid: true},
		sql.NullInt64{Int64: 7, Valid: true},
	}}}}}
	got, found, err := nvdBaselineLatchStore{database: database}.
		ReadNVDBaselineTripLatch(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("ReadNVDBaselineTripLatch() error = %v, want nil", err)
	}
	if !found {
		t.Fatal("ReadNVDBaselineTripLatch() found = false, want true")
	}
	if got.State.ReEnableSamples != 3 || got.State.SoftQuietStreak != 3 || got.State.Open || got.State.Page || got.State.ReEnableEligible {
		t.Fatalf("ReadNVDBaselineTripLatch() state = %+v, want shut with streaks of 3", got.State)
	}
	if !got.SampledAt.Equal(sampled) || got.Revision != 7 {
		t.Fatalf("ReadNVDBaselineTripLatch() = %+v, want sampled %v at revision 7", got, sampled)
	}
}

// stubDrainReader scripts one drain probe answer for the adapter test.
type stubDrainReader struct {
	row vulnerabilitystore.NVDBaselineDrainRow
	err error
}

func (s stubDrainReader) ReadNVDBaselineDrain(_ context.Context, _, _ string) (vulnerabilitystore.NVDBaselineDrainRow, error) {
	return s.row, s.err
}

// TestNVDBaselineDrainStoreMapsTimeoutApart proves a timed-out probe reaches
// the coordinator as the timeout type the service counts apart, while the
// phase maps by value.
func TestNVDBaselineDrainStoreMapsTimeoutApart(t *testing.T) {
	t.Parallel()

	since := time.Date(2026, time.November, 20, 10, 0, 0, 0, time.UTC)
	got, err := nvdBaselineDrainStore{reader: stubDrainReader{row: vulnerabilitystore.NVDBaselineDrainRow{
		Phase:        vulnerabilitystore.NVDBaselineDrainProjecting,
		NotDoneSince: since,
	}}}.ReadNVDBaselineDrain(context.Background(), vulnerability.NVDBaselineDrainKey{ScopeID: "scope-2002", GenerationID: "gen-2002"})
	if err != nil {
		t.Fatalf("ReadNVDBaselineDrain() error = %v, want nil", err)
	}
	if got.Phase != vulnerability.NVDBaselineDrainProjecting || !got.NotDoneSince.Equal(since) {
		t.Fatalf("ReadNVDBaselineDrain() = %+v, want projecting since %v", got, since)
	}

	timeout := errors.Join(errors.New("probe nvd baseline drain for year 2002"), vulnerabilitystore.ErrNVDBaselineDrainTimeout)
	_, err = nvdBaselineDrainStore{reader: stubDrainReader{err: timeout}}.
		ReadNVDBaselineDrain(context.Background(), vulnerability.NVDBaselineDrainKey{ScopeID: "scope-2002", GenerationID: "gen-2002"})
	var timeoutErr *vulnerability.NVDBaselineDrainTimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("ReadNVDBaselineDrain() error = %v, want the timeout type", err)
	}
	if got := vulnerability.NVDBaselineDrainTimeouts(err); got != 1 {
		t.Fatalf("NVDBaselineDrainTimeouts() = %d, want 1", got)
	}
}

// TestNVDBaselineTripSamplerMapsRates proves the cmd-level sampler hands the
// coordinator the ruled CVE-keyed rate, the scope rate beside it, and the
// oldest dirty age.
func TestNVDBaselineTripSamplerMapsRates(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 20, 12, 0, 0, 0, time.UTC)
	oldest := now.Add(-90 * time.Minute)
	database := &fake.ExecQueryer{QueryResponses: []fake.Rows{{Data: [][]any{{
		sql.NullInt64{Int64: 120, Valid: true},
		sql.NullInt64{Int64: 2, Valid: true},
		sql.NullTime{Time: oldest, Valid: true},
	}}}}}
	got, err := nvdBaselineTripSampler{database: database}.
		SampleNVDBaselineTrip(context.Background(), now)
	if err != nil {
		t.Fatalf("SampleNVDBaselineTrip() error = %v, want nil", err)
	}
	if got.DirtyProxyRowsPerHour != 120 || got.NVDScopeRowsPerHour != 2 {
		t.Fatalf("SampleNVDBaselineTrip() = %+v, want 120 proxy and 2 scope rows", got)
	}
	if got.OldestDirtyAge != 90*time.Minute {
		t.Fatalf("SampleNVDBaselineTrip() oldest age = %v, want 90m", got.OldestDirtyAge)
	}
	if !got.At.Equal(now) {
		t.Fatalf("SampleNVDBaselineTrip() at = %v, want %v", got.At, now)
	}
}

// TestNVDBaselinePlanReaderReturnsWrittenStamp proves the cmd-level reader
// hands the coordinator the stamp the admission wrote beside its work item:
// the year's last planned scope, the database clock of that insert, and
// whether the scope still has an open work item. The read binds the run id,
// so plan rows of a superseded run (#10831) never enter another run's
// inputs.
func TestNVDBaselinePlanReaderReturnsWrittenStamp(t *testing.T) {
	t.Parallel()

	plannedAt := time.Date(2026, time.November, 20, 11, 0, 0, 0, time.UTC)
	database := &fake.ExecQueryer{QueryResponses: []fake.Rows{{Data: [][]any{{
		2002,
		"scope-2002",
		plannedAt,
		true,
	}}}}}
	got, err := nvdBaselinePlanReader{database: database}.
		ReadNVDBaselinePlans(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("ReadNVDBaselinePlans() error = %v, want nil", err)
	}
	row, ok := got[2002]
	if !ok {
		t.Fatalf("ReadNVDBaselinePlans() = %+v, want the 2002 row", got)
	}
	if row.LastPlannedScopeID != "scope-2002" || !row.LastPlannedAt.Equal(plannedAt) || !row.InFlight {
		t.Fatalf("ReadNVDBaselinePlans() row = %+v, want scope-2002 at %v in flight", row, plannedAt)
	}
	if len(database.Queries) != 1 || len(database.Queries[0].Args) == 0 || database.Queries[0].Args[0] != "run-1" {
		t.Fatalf("ReadNVDBaselinePlans() queries = %+v, want the one read bound to run-1", database.Queries)
	}
}

// TestNVDBaselinePlanReaderPropagatesProbeFailure proves a failed plan read
// reaches the coordinator as an error, never as an empty map the planner
// would read as "never planned": a chunk whose row is missing or dropped
// holds loudly per the fail-closed rule.
func TestNVDBaselinePlanReaderPropagatesProbeFailure(t *testing.T) {
	t.Parallel()

	probeErr := errors.New("connection reset by peer")
	database := &fake.ExecQueryer{QueryResponses: []fake.Rows{{FailWith: probeErr}}}
	got, err := nvdBaselinePlanReader{database: database}.
		ReadNVDBaselinePlans(context.Background(), "run-1")
	if !errors.Is(err, probeErr) {
		t.Fatalf("ReadNVDBaselinePlans() error = %v, want it to wrap %v", err, probeErr)
	}
	if len(got) != 0 {
		t.Fatalf("ReadNVDBaselinePlans() = %+v, want no rows alongside the error", got)
	}
}
