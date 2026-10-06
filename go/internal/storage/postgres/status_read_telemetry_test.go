// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// statusSnapshotFullReads is the bounded read-label set a full status
// snapshot records, one label per reader.
var statusSnapshotFullReads = []string{
	"active_work_summary",
	"aws_cloud_scans",
	"aws_freshness",
	"collector_fact_evidence",
	"collector_generation_dead_letters",
	"coordinator",
	"generation_counts",
	"generation_transitions",
	"infra_inventory",
	"producer_activity",
	"registry_collectors",
	"scope_counts",
	"semantic_extraction",
	"terraform_state",
	"vulnerability_sources",
}

// stageCountingQueryer answers the active-work summary with one stage row
// whose count increments on every snapshot, answers everything else with no
// rows, and records the query-summary label each call carried.
type stageCountingQueryer struct {
	summaries []string
	calls     int
}

func (q *stageCountingQueryer) QueryContext(ctx context.Context, query string, _ ...any) (db.Rows, error) {
	q.summaries = append(q.summaries, db.QuerySummaryFromContext(ctx))
	if query != activeWorkSummaryQuery {
		return &fakeRows{}, nil
	}
	q.calls++
	return &fakeRows{rows: [][]any{
		{"stage", int64(1), fmt.Sprintf(`{"stage":"reducer","status":"pending","count":%d}`, q.calls)},
		{"queue", int64(1), `{"total_count":0,"outstanding_count":0,"pending_count":0,"in_flight_count":0,"retrying_count":0,"succeeded_count":0,"dead_letter_count":0,"failed_count":0,"provenance_edge_identity_upgrade_applied":false,"provenance_edge_identity_upgrade_required":0,"oldest_outstanding_age_seconds":0,"overdue_claim_count":0}`},
	}}, nil
}

// TestReadStatusSnapshotServesFreshStageCounts guards #6794 review finding
// F-04: the stage counts arrive in the same statement as the rest of the
// active-work summary, so a snapshot must never replace them with an older
// cached copy (the retired 2s stage-counts cache did exactly that).
func TestReadStatusSnapshotServesFreshStageCounts(t *testing.T) {
	t.Parallel()

	queryer := &stageCountingQueryer{}
	store := NewStatusStore(queryer)
	asOf := time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)
	for want := 1; want <= 2; want++ {
		snapshot, err := store.ReadStatusSnapshotFiltered(context.Background(), asOf, statuspkg.FullSnapshotSelection())
		if err != nil {
			t.Fatalf("ReadStatusSnapshotFiltered() error = %v", err)
		}
		if len(snapshot.StageCounts) != 1 || snapshot.StageCounts[0].Count != want {
			t.Fatalf("snapshot %d stage counts = %#v, want the fresh count %d", want, snapshot.StageCounts, want)
		}
	}
}

// TestReadStatusSnapshotLabelsEveryRead guards #6794 review finding F-09: each
// status snapshot read must carry a bounded query-summary label so the
// postgres.query span and the per-read duration metric say which read ran.
func TestReadStatusSnapshotLabelsEveryRead(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	queryer := &stageCountingQueryer{}
	store := NewInstrumentedStatusStore(queryer, instruments)
	if _, err := store.ReadStatusSnapshotFiltered(
		context.Background(), time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC), statuspkg.FullSnapshotSelection(),
	); err != nil {
		t.Fatalf("ReadStatusSnapshotFiltered() error = %v", err)
	}

	for i, summary := range queryer.summaries {
		if !slices.Contains(statusSnapshotFullReads, summary) {
			t.Fatalf("query %d carried summary %q, want one of %v", i, summary, statusSnapshotFullReads)
		}
	}

	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	reads := map[string]uint64{}
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_status_snapshot_read_duration_seconds" {
				continue
			}
			histogram, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("status snapshot read metric type = %T, want float64 histogram", m.Data)
			}
			for _, point := range histogram.DataPoints {
				read, _ := point.Attributes.Value(attribute.Key("read"))
				reads[read.AsString()] += point.Count
			}
		}
	}
	for _, want := range statusSnapshotFullReads {
		if reads[want] == 0 {
			t.Fatalf("no eshu_dp_status_snapshot_read_duration_seconds sample for read %q; got %v", want, reads)
		}
	}
	for read := range reads {
		if !slices.Contains(statusSnapshotFullReads, read) {
			t.Fatalf("unbounded read label %q recorded", read)
		}
	}
}

// TestInstrumentedDBStampsQuerySummary proves the postgres.query span names
// the read that ran when the caller labeled it.
func TestInstrumentedDBStampsQuerySummary(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	instrumented := &InstrumentedDB{
		Inner:     &instrumentedTestExecQueryer{},
		Tracer:    sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)).Tracer("test"),
		StoreName: "status_snapshot",
	}
	ctx := db.WithQuerySummary(context.Background(), "active_work_summary")
	rows, err := instrumented.QueryContext(ctx, "SELECT 1")
	if err != nil {
		t.Fatalf("QueryContext() error = %v", err)
	}
	_ = rows.Close()

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(spans))
	}
	found := false
	for _, attr := range spans[0].Attributes() {
		if attr.Key == "db.query.summary" && attr.Value.AsString() == "active_work_summary" {
			found = true
		}
	}
	if !found {
		t.Fatalf("postgres.query span attributes = %v, want db.query.summary=active_work_summary", spans[0].Attributes())
	}
}

// outcomeQueryer answers one named query with fixed rows and every other query
// with no rows.
type outcomeQueryer struct {
	query string
	rows  [][]any
}

func (q outcomeQueryer) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	if query == q.query {
		return &fakeRows{rows: q.rows}, nil
	}
	return &fakeRows{}, nil
}

// TestReadStatusSnapshotRecordsConsumerFailuresAsErrors guards #6794 review
// finding (PR #6808, Codex P2): a read whose rows iterate cleanly but fail to
// scan or decode is a failed read, so its duration sample must carry
// outcome=error rather than success.
func TestReadStatusSnapshotRecordsConsumerFailuresAsErrors(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		queryer outcomeQueryer
		read    string
	}{
		"scan conversion error": {
			queryer: outcomeQueryer{query: scopeCountsQuery, rows: [][]any{{"active", "not-a-count"}}},
			read:    "scope_counts",
		},
		"post-scan decode error": {
			queryer: outcomeQueryer{query: activeWorkSummaryQuery, rows: [][]any{{"stage", int64(1), `{not json`}}},
			read:    "active_work_summary",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			reader := sdkmetric.NewManualReader()
			instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
			if err != nil {
				t.Fatalf("NewInstruments() error = %v", err)
			}
			store := NewInstrumentedStatusStore(tc.queryer, instruments)
			if _, err := store.ReadStatusSnapshotFiltered(
				context.Background(), time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC), statuspkg.FullSnapshotSelection(),
			); err == nil {
				t.Fatal("ReadStatusSnapshotFiltered() error = nil, want the consumer failure")
			}
			outcomes := statusReadOutcomes(t, reader)
			if got := outcomes[tc.read]; !slices.Equal(got, []string{"error"}) {
				t.Fatalf("read %q outcomes = %v, want [error]; all = %v", tc.read, got, outcomes)
			}
		})
	}
}

// statusReadOutcomes returns the outcome labels recorded per read label.
func statusReadOutcomes(t *testing.T, reader *sdkmetric.ManualReader) map[string][]string {
	t.Helper()
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	outcomes := map[string][]string{}
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			histogram, ok := m.Data.(metricdata.Histogram[float64])
			if m.Name != "eshu_dp_status_snapshot_read_duration_seconds" || !ok {
				continue
			}
			for _, point := range histogram.DataPoints {
				read, _ := point.Attributes.Value(attribute.Key(telemetry.MetricDimensionRead))
				outcome, _ := point.Attributes.Value(attribute.Key(telemetry.MetricDimensionOutcome))
				for range point.Count {
					outcomes[read.AsString()] = append(outcomes[read.AsString()], outcome.AsString())
				}
			}
		}
	}
	return outcomes
}

func TestSkippedTerraformStatusReadEmitsNoTerraformPhase(t *testing.T) {
	t.Parallel()
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatal(err)
	}
	queryer := &stageCountingQueryer{}
	store := NewInstrumentedStatusStore(queryer, instruments)
	_, err = store.ReadStatusSnapshotFiltered(context.Background(), time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC), statuspkg.SnapshotSelection{SkipTerraformStateEvidence: true})
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(queryer.summaries, "terraform_state") {
		t.Fatal("skipped Terraform query carried telemetry label")
	}
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatal(err)
	}
	active := uint64(0)
	for _, scope := range collected.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name != "eshu_dp_status_snapshot_read_duration_seconds" {
				continue
			}
			histogram, ok := metric.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("read metric type = %T", metric.Data)
			}
			for _, point := range histogram.DataPoints {
				value, _ := point.Attributes.Value(attribute.Key("read"))
				if value.AsString() == "terraform_state" {
					t.Fatal("skipped Terraform read emitted duration metric")
				}
				if value.AsString() == "active_work_summary" {
					active += point.Count
				}
			}
		}
	}
	if active != 1 {
		t.Fatalf("active work read metric count = %d, want 1", active)
	}
}

// TestReadActiveWorkSummaryEntriesKeepsStatementRows proves the read model
// writer's pass (#7009) runs the live active-work statement byte for byte at
// the given asOf and returns every row as an entry, in statement order, with
// the section JSON text untouched.
func TestReadActiveWorkSummaryEntriesKeepsStatementRows(t *testing.T) {
	t.Parallel()

	queueJSON := `{"total_count":3,"outstanding_count":2,"pending_count":2,"in_flight_count":0,"retrying_count":0,"succeeded_count":1,"dead_letter_count":0,"failed_count":0,"provenance_edge_identity_upgrade_applied":false,"provenance_edge_identity_upgrade_required":0,"oldest_outstanding_age_seconds":12.5,"overdue_claim_count":0}`
	queryer := &recordingSummaryQueryer{rows: [][]any{
		{"backlog", int64(1), `{"domain":"d1","outstanding_count":2,"in_flight_count":0,"retrying_count":0,"dead_letter_count":0,"failed_count":0,"oldest_outstanding_age_seconds":12.5}`},
		{"queue", int64(1), queueJSON},
		{"stage", int64(1), `{"stage":"reducer","status":"pending","count":2}`},
		{"stage", int64(2), `{"stage":"reducer","status":"succeeded","count":1}`},
	}}
	asOf := time.Date(2026, 10, 6, 12, 0, 0, 0, time.FixedZone("EDT", -4*60*60))

	entries, err := ReadActiveWorkSummaryEntries(context.Background(), queryer, asOf)
	if err != nil {
		t.Fatalf("ReadActiveWorkSummaryEntries() error = %v", err)
	}
	if queryer.query != activeWorkSummaryQuery {
		t.Fatal("ReadActiveWorkSummaryEntries() did not run activeWorkSummaryQuery byte for byte")
	}
	if len(queryer.args) != 1 {
		t.Fatalf("statement args = %#v, want one asOf argument", queryer.args)
	}
	if bound, ok := queryer.args[0].(time.Time); !ok || !bound.Equal(asOf) || bound.Location() != time.UTC {
		t.Fatalf("statement $1 = %#v, want %v in UTC", queryer.args[0], asOf.UTC())
	}
	if len(entries) != len(queryer.rows) {
		t.Fatalf("entries = %d, want %d", len(entries), len(queryer.rows))
	}
	for i, row := range queryer.rows {
		got := entries[i]
		if got.Section != row[0] || got.Ordinal != row[1] || got.JSON != row[2] {
			t.Fatalf("entry %d = %#v, want %v", i, got, row)
		}
	}
}

// TestReadActiveWorkSummaryEntriesRejectsUndecodableRows proves a row the
// live decoder would reject is never handed to the writer, so the model can
// never store an answer the reader cannot decode.
func TestReadActiveWorkSummaryEntriesRejectsUndecodableRows(t *testing.T) {
	t.Parallel()

	for name, row := range map[string][]any{
		"invalid json":    {"stage", int64(1), `{not json`},
		"unknown section": {"surprise", int64(1), `{}`},
		"bad count":       {"stage", int64(1), `{"stage":"reducer","status":"pending","count":"many"}`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			queryer := &recordingSummaryQueryer{rows: [][]any{row}}
			entries, err := ReadActiveWorkSummaryEntries(context.Background(), queryer, time.Now())
			if err == nil {
				t.Fatalf("ReadActiveWorkSummaryEntries() = %#v, nil; want a decode error", entries)
			}
		})
	}
}

// TestReadActiveWorkSummaryEntriesEmptyResult proves an empty statement
// result is an empty, non-nil entry list, which encodes as [] and not null.
func TestReadActiveWorkSummaryEntriesEmptyResult(t *testing.T) {
	t.Parallel()

	entries, err := ReadActiveWorkSummaryEntries(context.Background(), &recordingSummaryQueryer{}, time.Now())
	if err != nil {
		t.Fatalf("ReadActiveWorkSummaryEntries() error = %v", err)
	}
	if entries == nil || len(entries) != 0 {
		t.Fatalf("entries = %#v, want an empty non-nil slice", entries)
	}
}

// TestActiveWorkSummarySourceSHA256PinsStatementText proves the rolling
// upgrade fence digests the exact statement text, so any change to the
// statement changes the digest the reader compares.
func TestActiveWorkSummarySourceSHA256PinsStatementText(t *testing.T) {
	t.Parallel()

	sum := sha256.Sum256([]byte(activeWorkSummaryQuery))
	if got, want := ActiveWorkSummarySourceSHA256(), hex.EncodeToString(sum[:]); got != want {
		t.Fatalf("ActiveWorkSummarySourceSHA256() = %q, want %q", got, want)
	}
	other := sha256.Sum256([]byte(activeWorkSummaryQuery + " "))
	if ActiveWorkSummarySourceSHA256() == hex.EncodeToString(other[:]) {
		t.Fatal("digest does not change with the statement text")
	}
}

// recordingSummaryQueryer answers any query with rows and records the last
// statement text and arguments.
type recordingSummaryQueryer struct {
	rows  [][]any
	query string
	args  []any
}

func (q *recordingSummaryQueryer) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	q.query = query
	q.args = args
	return &fakeRows{rows: q.rows}, nil
}
