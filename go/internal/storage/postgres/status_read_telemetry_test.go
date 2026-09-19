// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
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
	q.summaries = append(q.summaries, querySummaryFromContext(ctx))
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
	ctx := withQuerySummary(context.Background(), "active_work_summary")
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
