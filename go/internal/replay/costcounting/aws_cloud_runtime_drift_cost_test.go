// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package costcounting_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
	"github.com/eshu-hq/eshu/go/internal/correlation/model"
	"github.com/eshu-hq/eshu/go/internal/correlation/rules"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// awsCloudRuntimeDriftBudgetRelPath is the committed cost budget for the
// aws_cloud_runtime_drift scenario (fact-kind-registry family aws,
// specs/fact-kind-registry.v1.yaml:44-64, reducer_domain
// aws_cloud_runtime_drift). Unlike every other scenario in this package, the
// production writer for this domain
// (reducer.PostgresAWSCloudRuntimeDriftWriter) is a Postgres fact writer, not
// a Cypher graph writer — it has no committed cassette or graph-shaped input,
// so the fixture candidates live inline in this file and the budget records
// that explicitly instead of pointing at a cassette path.
var awsCloudRuntimeDriftBudgetRelPath = filepath.Join(
	"..", "..", "..", "..", "testdata", "cassettes", "replayoffline", "aws-cloud-runtime-drift.cost-budget.json",
)

// awsCloudRuntimeDriftFixtureCandidates is the deterministic input for both
// the positive and duplicate-invocation N+1 scenarios: two admitted
// aws_cloud_runtime_drift candidates (one orphaned, one unmanaged cloud
// resource), shaped like aws_cloud_runtime_drift_test.go's
// TestPostgresAWSCloudRuntimeDriftWriterPersistsOneFactPerFinding fixture.
func awsCloudRuntimeDriftFixtureCandidates() []model.Candidate {
	return []model.Candidate{
		{
			ID:             "aws_cloud_runtime_drift:arn:aws:lambda:us-east-1:123456789012:function:orphan:orphaned_cloud_resource",
			Kind:           rules.AWSCloudRuntimeDriftPackName,
			CorrelationKey: "arn:aws:lambda:us-east-1:123456789012:function:orphan",
			Confidence:     1,
			State:          model.CandidateStateAdmitted,
			Evidence: []model.EvidenceAtom{
				{
					ID:           "candidate/arn",
					SourceSystem: "reducer/aws_cloud_runtime_drift",
					EvidenceType: cloudruntime.EvidenceTypeCloudResourceARN,
					ScopeID:      "aws:123456789012:us-east-1",
					Key:          "arn",
					Value:        "arn:aws:lambda:us-east-1:123456789012:function:orphan",
					Confidence:   1,
				},
				{
					ID:           "candidate/finding_kind",
					SourceSystem: "reducer/aws_cloud_runtime_drift",
					EvidenceType: cloudruntime.EvidenceTypeFindingKind,
					ScopeID:      "aws:123456789012:us-east-1",
					Key:          "finding_kind",
					Value:        string(cloudruntime.FindingKindOrphanedCloudResource),
					Confidence:   1,
				},
			},
		},
		{
			ID:             "aws_cloud_runtime_drift:arn:aws:lambda:us-east-1:123456789012:function:unmanaged:unmanaged_cloud_resource",
			Kind:           rules.AWSCloudRuntimeDriftPackName,
			CorrelationKey: "arn:aws:lambda:us-east-1:123456789012:function:unmanaged",
			Confidence:     1,
			State:          model.CandidateStateAdmitted,
			Evidence: []model.EvidenceAtom{
				{
					ID:           "candidate/finding_kind",
					SourceSystem: "reducer/aws_cloud_runtime_drift",
					EvidenceType: cloudruntime.EvidenceTypeFindingKind,
					ScopeID:      "aws:123456789012:us-east-1",
					Key:          "finding_kind",
					Value:        string(cloudruntime.FindingKindUnmanagedCloudResource),
					Confidence:   1,
				},
			},
		},
	}
}

// postgresExecCountingQueryer is an in-memory postgres.ExecQueryer that
// records each ExecContext call. QueryContext is never exercised by
// PostgresAWSCloudRuntimeDriftWriter (it only writes) and is implemented as a
// no-op solely to satisfy the postgres.ExecQueryer interface.
type postgresExecCountingQueryer struct {
	execCalls atomic.Int64
}

func (q *postgresExecCountingQueryer) ExecContext(_ context.Context, _ string, _ ...any) (sql.Result, error) {
	q.execCalls.Add(1)
	return postgresFakeResult{}, nil
}

func (q *postgresExecCountingQueryer) QueryContext(_ context.Context, _ string, _ ...any) (postgres.Rows, error) {
	return nil, nil
}

func (q *postgresExecCountingQueryer) count() int64 { return q.execCalls.Load() }

// postgresFakeResult is a no-op sql.Result for the counting queryer above.
type postgresFakeResult struct{}

func (postgresFakeResult) LastInsertId() (int64, error) { return 0, nil }
func (postgresFakeResult) RowsAffected() (int64, error) { return 1, nil }

// newInstrumentedAWSCloudRuntimeDriftWriter builds the production
// reducer.PostgresAWSCloudRuntimeDriftWriter (constructed at
// go/cmd/reducer/wiring_handlers.go:61
// "AWSCloudRuntimeDriftWriter: reducer.PostgresAWSCloudRuntimeDriftWriter{DB:
// database}"), wired over a postgresExecCountingQueryer wrapped by the
// production postgres.InstrumentedDB — the same wrapper
// go/cmd/reducer/observed_service_wiring.go buildObservedReducerService
// applies to the real *sql.DB ("instrumentedDB := &postgres.InstrumentedDB{
// Inner: postgres.SQLDB{DB: db}, ..., StoreName: "reducer"}") before
// threading it into buildReducerService as database, which
// wiring_handlers.go then receives as its database parameter.
// postgres.InstrumentedDB.ExecContext records one observation on the
// eshu_dp_postgres_query_duration_seconds histogram per call
// (operation="write", store="reducer") — the PRIMARY instrument this scenario
// asserts, not a hand-counted call slice.
func newInstrumentedAWSCloudRuntimeDriftWriter(t *testing.T) (
	writer reducer.PostgresAWSCloudRuntimeDriftWriter,
	exec *postgresExecCountingQueryer,
	reader *sdkmetric.ManualReader,
) {
	t.Helper()

	inst, manualReader := newManualReaderInstruments(t)
	exec = &postgresExecCountingQueryer{}
	instrumentedDB := &postgres.InstrumentedDB{Inner: exec, Instruments: inst, StoreName: "reducer"}
	writer = reducer.PostgresAWSCloudRuntimeDriftWriter{
		DB:  instrumentedDB,
		Now: func() time.Time { return time.Date(2026, time.July, 12, 12, 0, 0, 0, time.UTC) },
	}
	return writer, exec, manualReader
}

// collectHistogramCount reads one named Float64 histogram's total observation
// count off a Collect snapshot: the sum of every data point's Count field.
// Unlike collectCounter (a Sum[int64] counter), eshu_dp_postgres_query_duration_seconds
// is a duration histogram, but each ExecContext call records exactly one
// observation, so its total Count is a genuine per-call cost signal recorded
// by production instrumentation, not a hand-counted call slice.
func collectHistogramCount(rm metricdata.ResourceMetrics, name string) uint64 {
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			hist, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				return 0
			}
			var total uint64
			for _, dp := range hist.DataPoints {
				total += dp.Count
			}
			return total
		}
	}
	return 0
}

// TestCostBudget_AWSCloudRuntimeDrift is the positive cost-counting gate for
// the aws_cloud_runtime_drift reducer projection (the aws family in
// specs/fact-kind-registry.v1.yaml, C-14 issue #4367). It drives the
// production
// reducer.PostgresAWSCloudRuntimeDriftWriter.WriteAWSCloudRuntimeDriftFindings
// over two deterministic admitted candidates through a real
// InstrumentedDB-backed sdkmetric.ManualReader, then asserts the
// eshu_dp_postgres_query_duration_seconds observation count is within the
// committed budget.
//
// Instrument read: eshu_dp_postgres_query_duration_seconds (a histogram, not a
// counter — see collectHistogramCount).
// WriteAWSCloudRuntimeDriftFindings now calls the shared
// reducerBatchInsertVersionedFacts bounded chunked bulk insert
// (go/internal/reducer/reducer_fact_batch_insert.go, issue #5317) instead of
// one ExecContext per candidate, so two admitted candidates fit one chunk and
// give exactly 1 observation. The companion N+1 negative control below still
// exercises the real regression shape for this domain: a duplicate
// invocation (retry without idempotency, or a dedup bug) now costs 2 batched
// round-trips instead of 1, which still exceeds this tightened budget.
func TestCostBudget_AWSCloudRuntimeDrift(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, awsCloudRuntimeDriftBudgetRelPath)
	writer, exec, reader := newInstrumentedAWSCloudRuntimeDriftWriter(t)

	if _, err := writer.WriteAWSCloudRuntimeDriftFindings(context.Background(), reducer.AWSCloudRuntimeDriftWrite{
		IntentID:     "intent-aws-drift-cost",
		ScopeID:      "aws:123456789012:us-east-1",
		GenerationID: "generation-aws-cost",
		SourceSystem: "aws",
		Cause:        "reducer/aws_cloud_runtime_drift",
		Candidates:   awsCloudRuntimeDriftFixtureCandidates(),
	}); err != nil {
		t.Fatalf("WriteAWSCloudRuntimeDriftFindings() error = %v", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	// PRIMARY assertion: read eshu_dp_postgres_query_duration_seconds's
	// observation count off the real otel reader. This histogram is recorded
	// by the production postgres.InstrumentedDB.ExecContext wrapper, NOT by a
	// hand-counted call slice.
	observations := collectHistogramCount(rm, "eshu_dp_postgres_query_duration_seconds")
	maxObservations, ok := budget.Budgets["eshu_dp_postgres_query_duration_seconds"]
	if !ok {
		t.Fatal("budget missing required key eshu_dp_postgres_query_duration_seconds")
	}
	if int64(observations) > maxObservations {
		t.Fatalf(
			"eshu_dp_postgres_query_duration_seconds observations = %d exceeds budget %d "+
				"(scenario=%s): algorithmic regression detected",
			observations, maxObservations, budget.Scenario,
		)
	}
	if observations == 0 {
		t.Fatal("eshu_dp_postgres_query_duration_seconds observations = 0: instrument not recording (false green guard)")
	}

	// SECONDARY assertion: raw ExecContext call count from the counting
	// queryer.
	execs := exec.count()
	if maxExecs, ok := budget.Budgets["statements_executed"]; ok {
		if execs > maxExecs {
			t.Fatalf(
				"statements_executed = %d exceeds budget %d (scenario=%s): too many Postgres write operations",
				execs, maxExecs, budget.Scenario,
			)
		}
		if execs == 0 {
			t.Fatal("statements_executed = 0: executor not recording (false green guard)")
		}
	}

	t.Logf(
		"scenario=%s eshu_dp_postgres_query_duration_seconds_observations=%d (budget=%d) statements_executed=%d (budget=%d)",
		budget.Scenario, observations, maxObservations, execs, budget.Budgets["statements_executed"],
	)
}

// TestCostBudget_AWSCloudRuntimeDrift_N1_ExceedsBudget is a negative control
// for the DUPLICATE-INVOCATION regression shape: a retry without an
// idempotency check, or an evidence-loader/candidate-dedup bug that admits
// the same candidate set twice, doubling Postgres write cost for identical
// logical work. This control simulates exactly that: it calls
// WriteAWSCloudRuntimeDriftFindings TWICE with the SAME candidate set and
// asserts the accumulated observation count EXCEEDS the committed budget.
// Even though WriteAWSCloudRuntimeDriftFindings now batches all candidates of
// a single Write call into one ExecContext round-trip (issue #5317), two
// separate Write calls still cost two round-trips, so this remains a valid
// negative control against the tightened budget=1. The companion
// TestCostBudget_AWSCloudRuntimeDrift_WithinCallN1_ExceedsBudget below covers
// the standard WITHIN-CALL N+1 shape (call once per candidate instead of once
// for the whole batch) that the batching migration introduced.
func TestCostBudget_AWSCloudRuntimeDrift_N1_ExceedsBudget(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, awsCloudRuntimeDriftBudgetRelPath)
	candidates := awsCloudRuntimeDriftFixtureCandidates()
	if len(candidates) < 2 {
		t.Fatalf("N+1 control needs >=2 candidates to exceed the budget; fixture has %d", len(candidates))
	}

	writer, _, reader := newInstrumentedAWSCloudRuntimeDriftWriter(t)

	for i := 0; i < 2; i++ {
		if _, err := writer.WriteAWSCloudRuntimeDriftFindings(context.Background(), reducer.AWSCloudRuntimeDriftWrite{
			IntentID:     "intent-aws-drift-cost",
			ScopeID:      "aws:123456789012:us-east-1",
			GenerationID: "generation-aws-cost",
			SourceSystem: "aws",
			Cause:        "reducer/aws_cloud_runtime_drift",
			Candidates:   candidates,
		}); err != nil {
			t.Fatalf("N+1 (duplicate invocation %d) WriteAWSCloudRuntimeDriftFindings() error = %v", i, err)
		}
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	observations := collectHistogramCount(rm, "eshu_dp_postgres_query_duration_seconds")
	maxObservations, ok := budget.Budgets["eshu_dp_postgres_query_duration_seconds"]
	if !ok {
		t.Fatal("budget has no eshu_dp_postgres_query_duration_seconds entry")
	}

	if int64(observations) <= maxObservations {
		t.Fatalf(
			"N+1 negative control: eshu_dp_postgres_query_duration_seconds observations = %d did NOT exceed "+
				"budget %d — budget is too loose to catch duplicate-invocation regressions or the negative "+
				"control is generating too few writes; tighten the budget or increase the duplicate fanout",
			observations, maxObservations,
		)
	}

	t.Logf(
		"N+1 negative control passed: eshu_dp_postgres_query_duration_seconds observations = %d > budget %d "+
			"(N=%d duplicate invocations of a %d-candidate set, scenario=%s)",
		observations, maxObservations, 2, len(candidates), budget.Scenario,
	)
}

// TestCostBudget_AWSCloudRuntimeDrift_WithinCallN1_ExceedsBudget is the
// standard WITHIN-CALL N+1 negative control this batching migration (issue
// #5317) introduced: it drives the SAME production batched dispatch as the
// positive test, calling WriteAWSCloudRuntimeDriftFindings once per fixture
// candidate instead of once for the whole batch — the classic N+1
// anti-pattern for a batched writer — and asserts the accumulated
// eshu_dp_postgres_query_duration_seconds observation count EXCEEDS the
// committed budget.
func TestCostBudget_AWSCloudRuntimeDrift_WithinCallN1_ExceedsBudget(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, awsCloudRuntimeDriftBudgetRelPath)
	candidates := awsCloudRuntimeDriftFixtureCandidates()
	if len(candidates) < 2 {
		t.Fatalf("N+1 control needs >=2 candidates to exceed the budget; fixture has %d", len(candidates))
	}

	writer, _, reader := newInstrumentedAWSCloudRuntimeDriftWriter(t)

	for _, candidate := range candidates {
		if _, err := writer.WriteAWSCloudRuntimeDriftFindings(context.Background(), reducer.AWSCloudRuntimeDriftWrite{
			IntentID:     "intent-aws-drift-cost",
			ScopeID:      "aws:123456789012:us-east-1",
			GenerationID: "generation-aws-cost",
			SourceSystem: "aws",
			Cause:        "reducer/aws_cloud_runtime_drift",
			Candidates:   []model.Candidate{candidate},
		}); err != nil {
			t.Fatalf("within-call N+1 WriteAWSCloudRuntimeDriftFindings() error = %v", err)
		}
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	observations := collectHistogramCount(rm, "eshu_dp_postgres_query_duration_seconds")
	maxObservations, ok := budget.Budgets["eshu_dp_postgres_query_duration_seconds"]
	if !ok {
		t.Fatal("budget has no eshu_dp_postgres_query_duration_seconds entry")
	}

	if int64(observations) <= maxObservations {
		t.Fatalf(
			"within-call N+1 negative control: eshu_dp_postgres_query_duration_seconds observations = %d did "+
				"NOT exceed budget %d — budget is too loose to catch N+1 regressions or the negative control is "+
				"generating too few writes; tighten the budget or increase the N+1 fanout",
			observations, maxObservations,
		)
	}

	t.Logf(
		"within-call N+1 negative control passed: eshu_dp_postgres_query_duration_seconds observations = %d > "+
			"budget %d (N=%d candidates, scenario=%s)",
		observations, maxObservations, len(candidates), budget.Scenario,
	)
}
