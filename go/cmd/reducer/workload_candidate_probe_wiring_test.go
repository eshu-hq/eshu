// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// workloadProbeSQLError mimics the SQLState() accessor pgconn.PgError exposes.
type workloadProbeSQLError struct{}

func (workloadProbeSQLError) Error() string    { return "ERROR: syntax error (SQLSTATE 42601)" }
func (workloadProbeSQLError) SQLState() string { return "42601" }

// workloadProbeWiringDB fails the candidate-precondition probe statement and
// serves every other query from the shared reducer fake.
type workloadProbeWiringDB struct {
	fakeReducerDB
	probed bool
}

func (f *workloadProbeWiringDB) QueryContext(
	ctx context.Context,
	query string,
	args ...any,
) (db.Rows, error) {
	if strings.Contains(query, "jsonb_to_record") && strings.Contains(query, "$6::text[]") {
		f.probed = true
		return nil, workloadProbeSQLError{}
	}
	return f.fakeReducerDB.QueryContext(ctx, query, args...)
}

// TestBuildReducerServiceWiresWorkloadCandidateProbeSignals guards the
// Instruments and Logger fields on the production WorkloadProjectionInputLoader
// in buildReducerService (#9986).
//
// The probe fails open by design, so deleting those two fields leaves every
// unit test green while the only operator signal for a probe that always errors
// (the #9700 syntax error) disappears. This drives the real buildReducerService,
// executes a workload_materialization intent, makes the probe statement fail,
// and asserts the counter and the structured log both come out.
func TestBuildReducerServiceWiresWorkloadCandidateProbeSignals(t *testing.T) {
	// Not parallel: the probe error log limiter is process-wide, so a repeated
	// run (-count=2) would see its log suppressed without the reset below.
	reducer.ResetWorkloadCandidateProbeErrorLogForTesting()

	database := &workloadProbeWiringDB{}
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	logged := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(logged, nil))
	service, err := buildReducerService(
		context.Background(), database, stubGraphExecutor{}, stubCypherExecutor{},
		postgres.NewSharedIntentStore(database), stubCypherReader{}, stubCypherReader{},
		obligationConsumerDisabledGetenv(func(string) string { return "" }), nil, instruments, logger, nil,
	)
	if err != nil {
		t.Fatalf("buildReducerService() error = %v, want nil", err)
	}

	now := time.Now().UTC()
	_, _ = service.Executor.Execute(context.Background(), reducercontract.Intent{
		IntentID:     "workload-probe-wiring",
		Domain:       reducer.DomainWorkloadMaterialization,
		ScopeID:      "scope-123",
		GenerationID: "generation-456",
		SourceSystem: "git",
		EntityKeys:   []string{"repo-123"},
		EnqueuedAt:   now,
		AvailableAt:  now,
	})
	if !database.probed {
		t.Fatal("the candidate probe statement never ran: the loader's FactLoader is not the production fact store")
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	if got := probeErrorCounterTotal(rm); got != 1 {
		t.Fatalf("eshu_dp_reducer_workload_candidate_probe_total{outcome=error,cause_class=sql_invalid} = %d, want 1: Instruments is not wired", got)
	}
	if !strings.Contains(logged.String(), "workload candidate probe failed") {
		t.Fatalf("probe failure log missing: Logger is not wired; got:\n%s", logged.String())
	}
}

func probeErrorCounterTotal(rm metricdata.ResourceMetrics) int64 {
	var total int64
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_reducer_workload_candidate_probe_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, point := range sum.DataPoints {
				outcome, _ := point.Attributes.Value("outcome")
				cause, _ := point.Attributes.Value("cause_class")
				if outcome.AsString() == telemetry.WorkloadCandidateProbeOutcomeError &&
					cause.AsString() == telemetry.WorkloadCandidateProbeCauseSQLInvalid {
					total += point.Value
				}
			}
		}
	}
	return total
}
