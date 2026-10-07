// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary_test

import (
	"context"
	"database/sql"
	"reflect"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/fake"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/status/summary"
	statestore "github.com/eshu-hq/eshu/go/internal/storage/postgres/terraform/state"
)

var tfObservedAt = time.Date(2026, 10, 7, 9, 30, 15, 250000000, time.UTC)

func tfSerialRows() [][]any {
	return [][]any{
		{"hash-a", "s3", "lineage-a", "42", "terraform_state:state_snapshot:s3:hash-a:lineage-a:serial:42", sql.NullTime{Time: tfObservedAt, Valid: true}},
		{"hash-b", "s3", "lineage-b", "7", "terraform_state:state_snapshot:s3:hash-b:lineage-b:serial:7", sql.NullTime{}},
	}
}

func tfWarningRows() [][]any {
	return [][]any{
		{"hash-a", "s3", "state_missing", "r1", "warning", "operator", "terraform_state", "h1", "gen-1", sql.NullTime{Time: tfObservedAt, Valid: true}},
		{"hash-a", "git", "unresolved_backend_expression", "dynamic_backend", "warning", "author", "git", "infra/backend.tf", "gen-2", sql.NullTime{Time: tfObservedAt.Add(-time.Hour), Valid: true}},
	}
}

// tfEntries encodes the fixture rows the way the writer does.
func tfEntries(t *testing.T) []summary.Entry {
	t.Helper()
	queryer := &fake.ExecQueryer{QueryResponses: []fake.Rows{{Data: tfSerialRows()}, {Data: tfWarningRows()}}}
	entries, err := statestore.SummaryEntries(context.Background(), queryer)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func tfStoredRow(t *testing.T, asOf time.Time, sha string, entries []summary.Entry) []any {
	t.Helper()
	encoded, err := summary.EncodeEntries(entries)
	if err != nil {
		t.Fatal(err)
	}
	return storedRow(asOf, summary.SchemaVersion, sha, len(entries), string(encoded))
}

// liveEvidence is what the live statements return for the fixture rows.
func liveEvidence(t *testing.T) statestore.TerraformStateAdminEvidence {
	t.Helper()
	queryer := &fake.ExecQueryer{QueryResponses: []fake.Rows{{Data: tfSerialRows()}, {Data: tfWarningRows()}}}
	evidence, err := statestore.ReadTerraformStateAdminEvidence(context.Background(), queryer, statuspkg.MaxTerraformStateRecentWarnings, tfObservedAt)
	if err != nil {
		t.Fatal(err)
	}
	return evidence
}

func tfSnapshot(t *testing.T, store pgstatus.StatusStore) statuspkg.RawSnapshot {
	t.Helper()
	snapshot, err := store.ReadStatusSnapshotFiltered(context.Background(), sourceTestNow, statuspkg.FullSnapshotSelection())
	if err != nil {
		t.Fatalf("ReadStatusSnapshotFiltered() error = %v", err)
	}
	return snapshot
}

func tfQueryer(t *testing.T, row []any) *sourceQueryer {
	t.Helper()
	return &sourceQueryer{installed: true, tfRow: row, tfSerials: tfSerialRows(), tfWarnings: tfWarningRows()}
}

func TestTerraformReaderOffRunsOnlyTheLiveStatements(t *testing.T) {
	t.Parallel()

	q := tfQueryer(t, tfStoredRow(t, sourceTestNow.Add(-5*time.Second), statestore.SummarySourceSHA256(), tfEntries(t)))
	snapshot := tfSnapshot(t, readerStore(q, false))

	if q.tfModelRead.Load() != 0 || q.clockReads.Load() != 0 {
		t.Fatalf("flag off issued %d terraform row reads and %d clock reads, want none", q.tfModelRead.Load(), q.clockReads.Load())
	}
	if q.tfLiveRuns.Load() != 2 {
		t.Fatalf("live Terraform statements ran %d times, want both (2)", q.tfLiveRuns.Load())
	}
	if got := snapshot.TerraformStateSource; got.Source != statuspkg.ActiveWorkSourceLive || got.Reason != statuspkg.ActiveWorkReasonFlagOff ||
		!got.AsOf.Equal(sourceTestNow) || got.Age != 0 || got.Stale {
		t.Fatalf("terraform source = %+v, want live/flag_off at the snapshot clock %v, age 0, not stale", got, sourceTestNow)
	}
	want := liveEvidence(t)
	if !reflect.DeepEqual(snapshot.TerraformStateLastSerials, want.LastSerials) || !reflect.DeepEqual(snapshot.TerraformStateRecentWarnings, want.RecentWarnings) {
		t.Fatalf("flag off answered %#v / %#v, want the live read", snapshot.TerraformStateLastSerials, snapshot.TerraformStateRecentWarnings)
	}
}

func TestTerraformReaderServesAFreshRowEqualToTheLiveRead(t *testing.T) {
	t.Parallel()

	storedAt := sourceTestNow.Add(-12 * time.Second)
	q := tfQueryer(t, tfStoredRow(t, storedAt, statestore.SummarySourceSHA256(), tfEntries(t)))
	snapshot := tfSnapshot(t, readerStore(q, true))

	if q.tfLiveRuns.Load() != 0 {
		t.Fatalf("live Terraform statements ran %d times beside a fresh stored row; the two must never mix", q.tfLiveRuns.Load())
	}
	got := snapshot.TerraformStateSource
	if got.Source != statuspkg.ActiveWorkSourceModel || got.Reason != statuspkg.ActiveWorkReasonFresh || !got.AsOf.Equal(storedAt) || got.Age != 12*time.Second || got.Stale {
		t.Fatalf("terraform source = %+v, want model/fresh at %v aged 12s, stale=false", got, storedAt)
	}
	want := liveEvidence(t)
	if !reflect.DeepEqual(snapshot.TerraformStateLastSerials, want.LastSerials) || !reflect.DeepEqual(snapshot.TerraformStateRecentWarnings, want.RecentWarnings) {
		t.Fatalf("stored row served %#v / %#v, want exactly what the live read returns", snapshot.TerraformStateLastSerials, snapshot.TerraformStateRecentWarnings)
	}
}

func TestTerraformReaderFallsBackToBothLiveStatementsWithATypedReason(t *testing.T) {
	t.Parallel()

	sha := statestore.SummarySourceSHA256()
	good := tfEntries(t)
	badEntries := append(append([]summary.Entry(nil), good...), summary.Entry{Section: "mystery", Ordinal: 1, JSON: `{}`})
	for _, tc := range []struct {
		name   string
		row    []any
		reason string
	}{
		{"row missing", nil, "missing"},
		{"another digest", tfStoredRow(t, sourceTestNow.Add(-time.Second), "other", good), "version"},
		{"stale", tfStoredRow(t, sourceTestNow.Add(-40*time.Second), sha, good), "stale"},
		{"an entry the decoder rejects", tfStoredRow(t, sourceTestNow.Add(-time.Second), sha, badEntries), "decode"},
		{"a stored count that disagrees with the payload", storedRow(sourceTestNow.Add(-time.Second), summary.SchemaVersion, sha, len(good)+3, encodedEntries(t, good...)), "row_count"},
		// A foreign schema version is never decoded, so an undecodable payload
		// is a version fallback and not a decode failure.
		{"a foreign schema version with an object payload", storedRow(sourceTestNow.Add(-time.Second), summary.SchemaVersion+1, sha, 2, `{"future":"encoding"}`), "version"},
		{"table not installed", nil, "not_installed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			q := tfQueryer(t, tc.row)
			if tc.reason == "not_installed" {
				q.installed = false
			}
			snapshot := tfSnapshot(t, readerStore(q, true))
			if q.tfLiveRuns.Load() != 2 {
				t.Fatalf("live Terraform statements ran %d times, want both (2)", q.tfLiveRuns.Load())
			}
			got := snapshot.TerraformStateSource
			if got.Source != statuspkg.ActiveWorkSourceLiveFallback || got.Reason != tc.reason || got.Stale {
				t.Fatalf("terraform source = %+v, want live_fallback/%s", got, tc.reason)
			}
			want := liveEvidence(t)
			if !reflect.DeepEqual(snapshot.TerraformStateRecentWarnings, want.RecentWarnings) || !reflect.DeepEqual(snapshot.TerraformStateLastSerials, want.LastSerials) {
				t.Fatalf("a fallback answered %#v, want the live read", snapshot.TerraformStateRecentWarnings)
			}
		})
	}
}

// TestTerraformReaderDecidesEachModelOnItsOwn: the two rows have their own
// as_of and digest, so a fresh active-work row and a stale Terraform-state row
// serve the first from the model and the second from the live statements.
func TestTerraformReaderDecidesEachModelOnItsOwn(t *testing.T) {
	t.Parallel()

	q := tfQueryer(t, tfStoredRow(t, sourceTestNow.Add(-40*time.Second), statestore.SummarySourceSHA256(), tfEntries(t)))
	q.row = storedRow(sourceTestNow.Add(-5*time.Second), summary.SchemaVersion, pgstatus.ActiveWorkSummarySourceSHA256(), 4, encodedEntries(t, goodEntries()...))
	snapshot := tfSnapshot(t, readerStore(q, true))

	if got := snapshot.ActiveWorkSource; got.Source != statuspkg.ActiveWorkSourceModel {
		t.Fatalf("active work source = %+v, want model", got)
	}
	if got := snapshot.TerraformStateSource; got.Source != statuspkg.ActiveWorkSourceLiveFallback || got.Reason != statuspkg.ActiveWorkReasonStale {
		t.Fatalf("terraform source = %+v, want live_fallback/stale", got)
	}
	if q.liveRuns.Load() != 0 || q.tfLiveRuns.Load() != 2 {
		t.Fatalf("live active-work/terraform statements = %d/%d, want 0/2", q.liveRuns.Load(), q.tfLiveRuns.Load())
	}
}

func TestTerraformReaderIssuesNothingWhenTheRouteSkipsTerraformEvidence(t *testing.T) {
	t.Parallel()

	q := tfQueryer(t, tfStoredRow(t, sourceTestNow.Add(-time.Second), statestore.SummarySourceSHA256(), tfEntries(t)))
	_, err := readerStore(q, true).ReadStatusSnapshotFiltered(context.Background(), sourceTestNow,
		statuspkg.FullSnapshotSelection().WithoutTerraformStateEvidence())
	if err != nil {
		t.Fatal(err)
	}
	if q.tfModelRead.Load() != 0 || q.tfLiveRuns.Load() != 0 {
		t.Fatalf("a route that skips Terraform evidence read the row %d times and ran %d live statements, want none", q.tfModelRead.Load(), q.tfLiveRuns.Load())
	}
}

func TestTerraformReaderRowReadCarriesItsOwnLabel(t *testing.T) {
	t.Parallel()

	q := tfQueryer(t, tfStoredRow(t, sourceTestNow.Add(-time.Second), statestore.SummarySourceSHA256(), tfEntries(t)))
	tfSnapshot(t, readerStore(q, true))
	q.mu.Lock()
	defer q.mu.Unlock()
	var found bool
	for _, label := range q.labels {
		if label == "terraform_state_model" {
			found = true
		}
	}
	if !found {
		t.Fatalf("row read labels = %q, want terraform_state_model among them", q.labels)
	}
}

var _ db.Queryer = (*sourceQueryer)(nil)

// TestTerraformReadSpanAttributesUseTheirOwnPrefix drives the production store:
// the active-work row is served and the Terraform-state row is stale, so the
// snapshot span must say model under status.active_work.* and live_fallback with
// a stale reason under status.terraform_state.*. A terraform observation that
// lost its prefix would overwrite the active-work attributes.
func TestTerraformReadSpanAttributesUseTheirOwnPrefix(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	tracer := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)).Tracer("test")
	ctx, span := tracer.Start(context.Background(), "postgres.status_snapshot")
	q := tfQueryer(t, tfStoredRow(t, sourceTestNow.Add(-40*time.Second), statestore.SummarySourceSHA256(), tfEntries(t)))
	q.row = storedRow(sourceTestNow.Add(-5*time.Second), summary.SchemaVersion, pgstatus.ActiveWorkSummarySourceSHA256(), 4, encodedEntries(t, goodEntries()...))
	if _, err := readerStore(q, true).ReadStatusSnapshotFiltered(ctx, sourceTestNow, statuspkg.FullSnapshotSelection()); err != nil {
		t.Fatal(err)
	}
	span.End()

	attrs := map[string]attribute.Value{}
	for _, kv := range recorder.Ended()[0].Attributes() {
		attrs[string(kv.Key)] = kv.Value
	}
	if got := attrs["status.active_work.source"].AsString(); got != "model" {
		t.Fatalf("status.active_work.source = %q, want model (overwritten by the terraform read?): %v", got, attrs)
	}
	if got := attrs["status.active_work.as_of_age_seconds"].AsFloat64(); got != 5 {
		t.Fatalf("status.active_work.as_of_age_seconds = %v, want 5 (the active-work row's age)", got)
	}
	if _, present := attrs["status.active_work.fallback_reason"]; present {
		t.Fatalf("status.active_work.fallback_reason is set although the active-work row was served: %v", attrs)
	}
	if attrs["status.terraform_state.source"].AsString() != "live_fallback" ||
		attrs["status.terraform_state.fallback_reason"].AsString() != "stale" {
		t.Fatalf("status.terraform_state.* = %v, want live_fallback/stale", attrs)
	}
}
