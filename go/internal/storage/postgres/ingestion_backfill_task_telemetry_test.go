// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// taskTelemetryFixture builds the shared params/queryer pair used by both
// tests below: one catalog anchor (repo-infra), one partition (scope-infra),
// and one deferred fact the per-scope query resolves for it. Both tests load
// exactly one query task for one partition, so the recorded telemetry has a
// single, deterministic data point.
func taskTelemetryFixture(t *testing.T) (deferredScopedFactQueryParams, *fakeExecQueryer, []scopeGenerationPartition) {
	t.Helper()

	params, ok := buildDeferredScopedFactQueryParams([]relationships.CatalogEntry{
		{RepoID: "repo-infra", Aliases: []string{"repo-infra", "infra-repo"}},
	})
	if !ok {
		t.Fatal("buildDeferredScopedFactQueryParams returned ok=false; test fixture has no usable anchor")
	}

	queryer := &fakeExecQueryer{
		deferredFactsByScope: map[string][][]any{
			"scope-infra": {
				contentFactRow(
					"fact-cross",
					"scope-infra",
					"gen-infra",
					"content",
					`{"repo_id":"repo-infra","artifact_type":"terraform","relative_path":"main.tf","content":"app_repo = \"app-repo\""}`,
				),
			},
		},
	}

	partitions := []scopeGenerationPartition{{ScopeID: "scope-infra", GenerationID: "gen-infra"}}
	return params, queryer, partitions
}

// TestLoadDeferredScopedFactsAcrossPartitionsRecordsPartitionLoadCompletedEvent
// is the #5096 promotion gate for the per-task fact-load timing: every
// successful query task must add a partition_load_completed event to the
// active relationship.backfill_deferred span (mirroring the existing
// partition_load_failed event on the error path), carrying the same fields
// the deferred_backfill_fact_load_task_completed operator log already prints
// -- task, query_tasks, scope_id, repo_terms, non_repo_terms, loaded_facts,
// duration_s, workers -- so an operator can attribute a slow task from the
// trace, not only by grepping logs.
func TestLoadDeferredScopedFactsAcrossPartitionsRecordsPartitionLoadCompletedEvent(t *testing.T) {
	t.Parallel()

	params, queryer, partitions := taskTelemetryFixture(t)
	store := IngestionStore{maintenanceWorkers: 1}

	recorder, tracer := recordingTracer()
	ctx, span := tracer.Start(context.Background(), backfillDeferredSpanName)

	envelopes, _, err := store.loadDeferredScopedFactsAcrossPartitions(ctx, queryer, params, partitions, nil)
	span.End()

	if err != nil {
		t.Fatalf("loadDeferredScopedFactsAcrossPartitions() error = %v, want nil", err)
	}
	if len(envelopes) != 1 {
		t.Fatalf("loaded %d envelopes, want 1", len(envelopes))
	}

	ended := findEndedSpan(t, recorder.Ended(), backfillDeferredSpanName)

	var found bool
	var gotScope string
	gotInts := map[string]int64{}
	var gotDurationSeen bool
	for _, event := range ended.Events() {
		if event.Name != "partition_load_completed" {
			continue
		}
		found = true
		for _, attr := range event.Attributes {
			key := string(attr.Key)
			switch key {
			case "scope_id":
				gotScope = attr.Value.AsString()
			case "duration_s":
				gotDurationSeen = true
				if got := attr.Value.AsFloat64(); got < 0 {
					t.Errorf("duration_s = %v, want >= 0", got)
				}
			default:
				gotInts[key] = attr.Value.AsInt64()
			}
		}
	}
	if !found {
		t.Fatal("no partition_load_completed event recorded on relationship.backfill_deferred span")
	}
	if !gotDurationSeen {
		t.Fatal("partition_load_completed event missing duration_s attribute")
	}
	if gotScope != "scope-infra" {
		t.Fatalf("scope_id = %q, want %q", gotScope, "scope-infra")
	}

	wantInts := map[string]int64{
		"task":           1,
		"query_tasks":    1,
		"repo_terms":     int64(len(params.repoIDValues)),
		"non_repo_terms": int64(len(params.nonRepoIDLike)),
		"loaded_facts":   1,
		"workers":        1,
	}
	for key, want := range wantInts {
		got, ok := gotInts[key]
		if !ok {
			t.Errorf("partition_load_completed event missing attribute %q", key)
			continue
		}
		if got != want {
			t.Errorf("event attribute %q = %d, want %d", key, got, want)
		}
	}
}

// TestLoadDeferredScopedFactsAcrossPartitionsRecordsPartitionLoadFactCountMetric
// is the #5096 promotion gate for making the per-task loaded_facts count a
// first-class, alertable/dashboardable metric (not only a log field): the
// deferred backfill's per-scope fact-load fan-out must record
// eshu_dp_deferred_backfill_partition_load_fact_count once per query task,
// unattributed like its sibling
// eshu_dp_deferred_backfill_partition_load_duration_seconds, so no raw
// scope/repo id ever becomes a metric label.
func TestLoadDeferredScopedFactsAcrossPartitionsRecordsPartitionLoadFactCountMetric(t *testing.T) {
	t.Parallel()

	params, queryer, partitions := taskTelemetryFixture(t)

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}

	store := IngestionStore{maintenanceWorkers: 1}
	if _, _, err := store.loadDeferredScopedFactsAcrossPartitions(
		context.Background(), queryer, params, partitions, instruments,
	); err != nil {
		t.Fatalf("loadDeferredScopedFactsAcrossPartitions() error = %v, want nil", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}

	var found bool
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metricRecord := range scopeMetrics.Metrics {
			if metricRecord.Name != "eshu_dp_deferred_backfill_partition_load_fact_count" {
				continue
			}
			hist, ok := metricRecord.Data.(metricdata.Histogram[int64])
			if !ok {
				t.Fatalf("metric data = %T, want metricdata.Histogram[int64]", metricRecord.Data)
			}
			for _, dp := range hist.DataPoints {
				found = true
				if len(dp.Attributes.ToSlice()) != 0 {
					t.Errorf("data point attributes = %v, want none (unattributed like the sibling duration histogram)", dp.Attributes.ToSlice())
				}
				if dp.Sum != 1 {
					t.Errorf("histogram sum = %d, want 1 (one task loaded one fact)", dp.Sum)
				}
				if dp.Count != 1 {
					t.Errorf("histogram data point count = %d, want 1 (one Record call)", dp.Count)
				}
			}
		}
	}
	if !found {
		t.Fatal("no eshu_dp_deferred_backfill_partition_load_fact_count data point recorded")
	}
}

// failingDeferredFactQueryer answers the per-partition deferred scoped-fact
// query with an error, and delegates everything else (the memo gate lookups)
// to the shared fake so the surrounding pass still behaves normally.
type failingDeferredFactQueryer struct {
	*fakeExecQueryer
	err error
}

func (f *failingDeferredFactQueryer) QueryContext(
	ctx context.Context,
	query string,
	args ...any,
) (Rows, error) {
	if query == listDeferredScopedRelationshipFactRecordsQuery {
		return nil, f.err
	}
	return f.fakeExecQueryer.QueryContext(ctx, query, args...)
}

// TestLoadDeferredScopedFactsAcrossPartitionsRecordsZeroFactCountOnFailure
// pins the behavior documented in this package's README: both histograms
// record before the error check, so a task whose query fails still contributes
// its duration and a 0-fact data point. An operator reading a
// high-duration/low-fact-count bucket therefore cannot tell contention from a
// failure on the metric alone, and the README says so. Without this test the
// Record calls could move below the error check and both the success-path
// tests and that paragraph would stay green while silently diverging.
func TestLoadDeferredScopedFactsAcrossPartitionsRecordsZeroFactCountOnFailure(t *testing.T) {
	t.Parallel()

	params, queryer, partitions := taskTelemetryFixture(t)

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}

	failing := &failingDeferredFactQueryer{
		fakeExecQueryer: queryer,
		err:             errors.New("partition query failed"),
	}

	store := IngestionStore{maintenanceWorkers: 1}
	if _, _, err := store.loadDeferredScopedFactsAcrossPartitions(
		context.Background(), failing, params, partitions, instruments,
	); err == nil {
		t.Fatal("loadDeferredScopedFactsAcrossPartitions() error = nil, want the injected partition failure")
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}

	var found bool
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metricRecord := range scopeMetrics.Metrics {
			if metricRecord.Name != "eshu_dp_deferred_backfill_partition_load_fact_count" {
				continue
			}
			hist, ok := metricRecord.Data.(metricdata.Histogram[int64])
			if !ok {
				t.Fatalf("metric data = %T, want metricdata.Histogram[int64]", metricRecord.Data)
			}
			for _, dp := range hist.DataPoints {
				found = true
				if dp.Count != 1 {
					t.Errorf("histogram data point count = %d, want 1 (the failed task still records)", dp.Count)
				}
				if dp.Sum != 0 {
					t.Errorf("histogram sum = %d, want 0 (a failed task loads no facts)", dp.Sum)
				}
			}
		}
	}
	if !found {
		t.Fatal("no fact-count data point recorded for the failed task; the README documents that a failure contributes a 0-fact point")
	}
}

// TestLoadDeferredScopedFactsAcrossPartitionsLogsPerTaskCompletion pins the
// deferred_backfill_fact_load_task_completed per-task log line the README and
// the public telemetry reference document as a deliberately retained
// compatibility signal, not a leftover the #5096 span/metric promotion
// replaced. Without this test, a future cleanup could delete the log.Printf
// call while every other gate -- the metric test, the span-event test, and
// the doc build -- stayed green.
//
// This test does not call t.Parallel(): it redirects the process-global
// "log" package writer, which would race with the sibling tests in this file
// that also exercise loadDeferredScopedFactsAcrossPartitions under
// t.Parallel(). Go's testing package runs non-parallel tests to completion,
// one at a time, before any t.Parallel() test in the package resumes, so
// this ordering is race-free without additional synchronization.
func TestLoadDeferredScopedFactsAcrossPartitionsLogsPerTaskCompletion(t *testing.T) {
	params, queryer, partitions := taskTelemetryFixture(t)
	store := IngestionStore{maintenanceWorkers: 1}

	original := log.Writer()
	t.Cleanup(func() { log.SetOutput(original) })
	var buf bytes.Buffer
	log.SetOutput(&buf)

	if _, _, err := store.loadDeferredScopedFactsAcrossPartitions(
		context.Background(), queryer, params, partitions, nil,
	); err != nil {
		t.Fatalf("loadDeferredScopedFactsAcrossPartitions() error = %v, want nil", err)
	}

	if !strings.Contains(buf.String(), "deferred_backfill_fact_load_task_completed") {
		t.Fatalf("log output = %q, want it to contain the deferred_backfill_fact_load_task_completed line", buf.String())
	}
}
