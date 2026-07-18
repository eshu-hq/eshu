// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package costcounting_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
	"github.com/eshu-hq/eshu/go/internal/correlation/drift/multicloud"
	"github.com/eshu-hq/eshu/go/internal/correlation/model"
	"github.com/eshu-hq/eshu/go/internal/correlation/rules"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// multiCloudRuntimeDriftBudgetRelPath is the committed cost budget for the
// multi_cloud_runtime_drift scenario (fact-kind-registry family
// reducer_derived, reducer_domain reducer_derived_findings, kind
// reducer_multi_cloud_runtime_drift_finding, specs/fact-kind-registry.v1.yaml
// :127-138, C-14 issue #4367). The production writer,
// reducer.PostgresMultiCloudRuntimeDriftWriter.WriteMultiCloudRuntimeDriftFindings
// (go/internal/reducer/multi_cloud_runtime_drift_writer.go:35), operates over
// []model.Candidate Go values, not a CanonicalMaterialization, so the fixture
// candidates live inline in this file, matching the container_image_identity_
// cost_test.go / package_source_correlation_cost_test.go convention for
// writers with no committed cassette.
var multiCloudRuntimeDriftBudgetRelPath = filepath.Join(
	"..", "..", "..", "..", "testdata", "cassettes", "replayoffline", "multi-cloud-runtime-drift.cost-budget.json",
)

const multiCloudRuntimeDriftCostIntentID = "intent-multi-cloud-runtime-drift-cost"

// multiCloudRuntimeDriftFixtureCandidates is the deterministic input for this
// scenario: two admitted candidates with distinct canonical
// cloud_resource_uid identity (one orphaned GCP compute instance, one
// unmanaged Azure storage account), shaped like the evidence atoms
// multicloud.BuildCandidates emits (go/internal/correlation/drift/multicloud/
// candidate.go buildOneCandidate) and the fixture
// TestPostgresMultiCloudRuntimeDriftWriterPersistsOneFactPerFinding drives
// (go/internal/reducer/multi_cloud_runtime_drift_test.go
// gcpAndAzureDriftRows/buildAdmittedMultiCloudWrite). Built directly at the
// Candidate level (not through BuildCandidates' Row/classify pipeline) so
// this scenario stays independent of that classification path, mirroring
// aws_cloud_runtime_drift_cost_test.go's manual-Evidence-atom convention.
func multiCloudRuntimeDriftFixtureCandidates() []model.Candidate {
	const scope = "multi:tenant"
	candidate := func(uid string, kind cloudruntime.FindingKind, provider string) model.Candidate {
		candidateID := "multi_cloud_runtime_drift:" + uid + ":" + string(kind)
		return model.Candidate{
			ID:             candidateID,
			Kind:           rules.MultiCloudRuntimeDriftPackName,
			CorrelationKey: uid,
			Confidence:     1,
			State:          model.CandidateStateAdmitted,
			Evidence: []model.EvidenceAtom{
				{
					ID:           candidateID + "/uid",
					SourceSystem: "reducer/multi_cloud_runtime_drift",
					EvidenceType: multicloud.EvidenceTypeCloudResourceUID,
					ScopeID:      scope,
					Key:          "cloud_resource_uid",
					Value:        uid,
					Confidence:   1,
				},
				{
					ID:           candidateID + "/finding_kind",
					SourceSystem: "reducer/multi_cloud_runtime_drift",
					EvidenceType: multicloud.EvidenceTypeFindingKind,
					ScopeID:      scope,
					Key:          "finding_kind",
					Value:        string(kind),
					Confidence:   1,
				},
				{
					ID:           candidateID + "/provider",
					SourceSystem: "reducer/multi_cloud_runtime_drift",
					EvidenceType: multicloud.EvidenceTypeProvider,
					ScopeID:      scope,
					Key:          "provider",
					Value:        provider,
					Confidence:   1,
				},
			},
		}
	}
	return []model.Candidate{
		candidate(
			"//compute.googleapis.com/projects/proj/zones/z/instances/orphan",
			cloudruntime.FindingKindOrphanedCloudResource,
			"gcp",
		),
		candidate(
			"/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/unmanaged",
			cloudruntime.FindingKindUnmanagedCloudResource,
			"azure",
		),
	}
}

// newInstrumentedMultiCloudRuntimeDriftWriter builds the PRODUCTION Postgres
// write dispatch for this domain: reducer.PostgresMultiCloudRuntimeDriftWriter
// over the shared newInstrumentedReducerDB seam (postgres_cost_helpers_test.go),
// the same postgres.InstrumentedDB shape go/cmd/reducer/observed_service_
// wiring.go wires for every reducer Postgres writer. WriteMultiCloudRuntimeDriftFindings
// (go/internal/reducer/multi_cloud_runtime_drift_writer.go) loops over
// write.Candidates and issues ONE ExecContext PER candidate — there is no
// reducerBatchInsertFacts call anywhere in this writer, exactly like its
// sibling reducer.PostgresAWSCloudRuntimeDriftWriter
// (aws_cloud_runtime_drift_cost_test.go) — so this writer's Postgres write
// cost is O(N) round-trips per candidate list, not O(N/batchSize).
func newInstrumentedMultiCloudRuntimeDriftWriter(t *testing.T) (
	writer reducer.PostgresMultiCloudRuntimeDriftWriter,
	fake *countingExecQueryer,
	reader *sdkmetric.ManualReader,
) {
	t.Helper()

	fake = &countingExecQueryer{}
	db, manualReader := newInstrumentedReducerDB(t, fake)
	writer = reducer.PostgresMultiCloudRuntimeDriftWriter{
		DB:  db,
		Now: func() time.Time { return time.Date(2026, time.July, 12, 12, 0, 0, 0, time.UTC) },
	}
	return writer, fake, manualReader
}

// TestCostBudget_MultiCloudRuntimeDrift is the exact-equality cost-counting
// gate for the multi_cloud_runtime_drift reducer projection (the
// reducer_derived_findings family in specs/fact-kind-registry.v1.yaml, C-14
// issue #4367). It drives the production
// PostgresMultiCloudRuntimeDriftWriter.WriteMultiCloudRuntimeDriftFindings
// over two admitted candidates with distinct canonical identity in one scope,
// through a real InstrumentedDB-backed sdkmetric.ManualReader, then asserts
// eshu_dp_postgres_query_duration_seconds's write-attributed observation
// count EQUALS the committed budget exactly.
//
// Instrument read: eshu_dp_postgres_query_duration_seconds{operation="write"}.
// postgres.InstrumentedDB.ExecContext (go/internal/storage/postgres/
// instrumented.go) records this once per ExecContext round-trip. This writer
// has no batched insert path: its single candidate loop issues one
// ExecContext per candidate, so its Postgres write cost is inherently O(N)
// round-trips — there is no batching boundary for a within-writer N+1
// control to break (splitting N candidates across N separate Write calls
// costs the identical N round-trips as one call carrying all N; confirmed
// empirically for the structurally identical aws_cloud_runtime_drift writer,
// see aws_cloud_runtime_drift_cost_test.go's "N+1 control shape" doc
// comment, and for package_source_correlation, see package_source_
// correlation_cost_test.go). The exact-equality assertion (== budget) is the
// regression gate for this domain. Migrating this writer onto the shared
// reducerBatchInsertFacts bounded bulk-insert path is a follow-on tracked
// separately (C-14 issue #4367 orchestration); this budget intentionally
// encodes the CURRENT known per-row write amplification rather than
// absorbing it silently.
func TestCostBudget_MultiCloudRuntimeDrift(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, multiCloudRuntimeDriftBudgetRelPath)
	writer, fake, reader := newInstrumentedMultiCloudRuntimeDriftWriter(t)

	result, err := writer.WriteMultiCloudRuntimeDriftFindings(context.Background(), reducer.MultiCloudRuntimeDriftWrite{
		IntentID:     multiCloudRuntimeDriftCostIntentID,
		ScopeID:      "multi:tenant",
		GenerationID: "generation-multi-cloud-runtime-drift-cost",
		SourceSystem: "gcp",
		Cause:        "reducer/multi_cloud_runtime_drift",
		Candidates:   multiCloudRuntimeDriftFixtureCandidates(),
		Summary:      multicloud.Summary{OrphanedResources: 1, UnmanagedResources: 1},
	})
	if err != nil {
		t.Fatalf("WriteMultiCloudRuntimeDriftFindings() error = %v", err)
	}
	if result.CanonicalWrites != 2 {
		t.Fatalf("CanonicalWrites = %d, want 2 (both fixture candidates are distinct admitted findings)", result.CanonicalWrites)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	// PRIMARY assertion: read eshu_dp_postgres_query_duration_seconds's
	// write-attributed observation count off the real otel reader, asserted
	// EXACT (this per-row writer's cost is O(N)).
	writes := collectAttributedHistogramCount(rm, "eshu_dp_postgres_query_duration_seconds", "operation", "write")
	wantWrites, ok := budget.Budgets["eshu_dp_postgres_query_duration_seconds"]
	if !ok {
		t.Fatal("budget missing required key eshu_dp_postgres_query_duration_seconds")
	}
	if writes != uint64(wantWrites) {
		t.Fatalf(
			"eshu_dp_postgres_query_duration_seconds write observations = %d, want exactly %d "+
				"(scenario=%s): this per-row writer's cost is O(N) round-trips — any deviation, "+
				"up or down, is a regression against the committed known-amplification budget",
			writes, wantWrites, budget.Scenario,
		)
	}
	if writes == 0 {
		t.Fatal("eshu_dp_postgres_query_duration_seconds write observations = 0: instrument not recording (false green guard)")
	}

	// SECONDARY assertion: raw ExecContext call count from the counting fake.
	execs := fake.totalExecs()
	if wantExecs, ok := budget.Budgets["statements_executed"]; ok {
		if execs != wantExecs {
			t.Fatalf(
				"statements_executed = %d, want exactly %d (scenario=%s)",
				execs, wantExecs, budget.Scenario,
			)
		}
	}

	t.Logf(
		"scenario=%s eshu_dp_postgres_query_duration_seconds_writes=%d (budget=%d, exact) statements_executed=%d (budget=%d, exact)",
		budget.Scenario, writes, wantWrites, execs, budget.Budgets["statements_executed"],
	)
}
