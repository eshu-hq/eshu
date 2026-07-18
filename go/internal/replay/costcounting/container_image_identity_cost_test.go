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

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// containerImageIdentityBudgetRelPath is the committed cost budget for the
// container_image_identity scenario (fact-kind-registry family reducer_derived,
// reducer_domain reducer_derived_findings, kind reducer_supply_chain_impact_
// finding's sibling kind is NOT this one — container_image_identity instead
// publishes fact kind "reducer_container_image_identity" through the
// same generic canonicalReducerFactInsertQuery/batched-insert machinery every
// PostgresXxxWriter in go/internal/reducer shares). This writer operates over
// []ContainerImageIdentityDecision Go values, not a CanonicalMaterialization,
// so the fixture decisions live inline in this file, matching the pilot's
// ec2_instance_node_cost_test.go / aws_cloud_runtime_drift_cost_test.go
// convention for writers with no committed cassette.
var containerImageIdentityBudgetRelPath = filepath.Join(
	"..", "..", "..", "..", "testdata", "cassettes", "replayoffline", "container-image-identity.cost-budget.json",
)

const containerImageIdentityCostIntentID = "intent-container-image-identity-cost"

// containerImageIdentityFixtureDecisions is the deterministic input for the
// positive and N+1 scenarios: two canonical (CanonicalWrites=1) exact-digest
// decisions for distinct image references in one scope. Both survive
// containerImageIdentityCanonicalDecisions' CanonicalWrites>0 filter
// (go/internal/reducer/container_image_identity.go), so both become rows the
// batched writer persists.
func containerImageIdentityFixtureDecisions() []reducer.ContainerImageIdentityDecision {
	row := func(id string) reducer.ContainerImageIdentityDecision {
		return reducer.ContainerImageIdentityDecision{
			ImageRef:         "registry.example.com/team/api:" + id,
			Digest:           "sha256:" + id + "11111111111111111111111111111111111111111111111111111111111111",
			RepositoryID:     "oci-registry://registry.example.com/team/api",
			Outcome:          reducer.ContainerImageIdentityExactDigest,
			CanonicalWrites:  1,
			IdentityStrength: "tag_observation_with_digest",
		}
	}
	return []reducer.ContainerImageIdentityDecision{row("aaaa"), row("bbbb")}
}

// newInstrumentedContainerImageIdentityWriter builds the PRODUCTION Postgres
// write dispatch for this domain: reducer.PostgresContainerImageIdentityWriter
// over a postgres.InstrumentedDB (StoreName "reducer", the exact shape
// go/cmd/reducer/observed_service_wiring.go wires) wrapping a
// countingExecQueryer. WriteContainerImageIdentityDecisions
// (go/internal/reducer/container_image_identity_writer.go) filters to
// CanonicalWrites>0 decisions, then calls reducerBatchInsertFacts — the SAME
// bounded chunked bulk insert container_image_identity, ci_cd_run_correlation,
// and sbom_attestation_attachment all share
// (go/internal/reducer/reducer_fact_batch_insert.go) — so two canonical rows
// fit in one 1000-row chunk and cost exactly one ExecContext round-trip.
func newInstrumentedContainerImageIdentityWriter(t *testing.T) (
	writer reducer.PostgresContainerImageIdentityWriter,
	fake *countingExecQueryer,
	reader *sdkmetric.ManualReader,
) {
	t.Helper()

	fake = &countingExecQueryer{}
	db, manualReader := newInstrumentedReducerDB(t, fake)
	writer = reducer.PostgresContainerImageIdentityWriter{
		DB:  db,
		Now: func() time.Time { return time.Date(2026, time.July, 12, 12, 0, 0, 0, time.UTC) },
	}
	return writer, fake, manualReader
}

// TestCostBudget_ContainerImageIdentity is the positive cost-counting gate for
// the container_image_identity reducer projection (C-14 issue #4367, Tier-2
// Postgres cost slice). It drives the production
// PostgresContainerImageIdentityWriter.WriteContainerImageIdentityDecisions
// over two canonical decisions in one scope, through a real
// InstrumentedDB-backed sdkmetric.ManualReader, then asserts
// eshu_dp_postgres_query_duration_seconds's write-attributed observation count
// is within the committed budget.
//
// Instrument read: eshu_dp_postgres_query_duration_seconds{operation="write"}.
// postgres.InstrumentedDB.ExecContext (go/internal/storage/postgres/
// instrumented.go) records this once per ExecContext round-trip. The writer's
// reducerBatchInsertFacts call issues one ExecContext per ceil(N/1000) chunk;
// two rows fit one chunk, so this scenario asserts exactly one write
// observation. An N+1 write-per-decision regression would double this count.
func TestCostBudget_ContainerImageIdentity(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, containerImageIdentityBudgetRelPath)
	writer, fake, reader := newInstrumentedContainerImageIdentityWriter(t)

	result, err := writer.WriteContainerImageIdentityDecisions(context.Background(), reducer.ContainerImageIdentityWrite{
		IntentID:     containerImageIdentityCostIntentID,
		ScopeID:      "repo:team-api",
		GenerationID: "generation-container-image-identity-cost",
		SourceSystem: "git",
		Cause:        "reducer/container_image_identity",
		Decisions:    containerImageIdentityFixtureDecisions(),
	})
	if err != nil {
		t.Fatalf("WriteContainerImageIdentityDecisions() error = %v", err)
	}
	if result.CanonicalWrites != 2 {
		t.Fatalf("CanonicalWrites = %d, want 2 (both fixture decisions carry CanonicalWrites=1)", result.CanonicalWrites)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	// PRIMARY assertion: read eshu_dp_postgres_query_duration_seconds's
	// write-attributed observation count off the real otel reader. This is
	// recorded by the production postgres.InstrumentedDB.ExecContext wrapper,
	// NOT by a hand-counted call slice.
	writes := collectAttributedHistogramCount(rm, "eshu_dp_postgres_query_duration_seconds", "operation", "write")
	maxWrites, ok := budget.Budgets["eshu_dp_postgres_query_duration_seconds"]
	if !ok {
		t.Fatal("budget missing required key eshu_dp_postgres_query_duration_seconds")
	}
	if writes > uint64(maxWrites) {
		t.Fatalf(
			"eshu_dp_postgres_query_duration_seconds write observations = %d exceeds budget %d "+
				"(scenario=%s): algorithmic regression detected",
			writes, maxWrites, budget.Scenario,
		)
	}
	if writes == 0 {
		t.Fatal("eshu_dp_postgres_query_duration_seconds write observations = 0: instrument not recording (false green guard)")
	}

	// SECONDARY assertion: raw ExecContext call count from the counting fake.
	execs := fake.totalExecs()
	if maxExecs, ok := budget.Budgets["statements_executed"]; ok {
		if execs > maxExecs {
			t.Fatalf(
				"statements_executed = %d exceeds budget %d (scenario=%s): too many Postgres write operations",
				execs, maxExecs, budget.Scenario,
			)
		}
		if execs == 0 {
			t.Fatal("statements_executed = 0: fake not recording (false green guard)")
		}
	}

	t.Logf(
		"scenario=%s eshu_dp_postgres_query_duration_seconds_writes=%d (budget=%d) statements_executed=%d (budget=%d)",
		budget.Scenario, writes, maxWrites, execs, budget.Budgets["statements_executed"],
	)
}

// TestCostBudget_ContainerImageIdentity_N1_ExceedsBudget is the mandatory
// negative control, run through the SAME production batched dispatch as the
// positive test. It calls WriteContainerImageIdentityDecisions once per
// fixture decision instead of once for the whole batch — the classic N+1
// anti-pattern for a batched writer — and asserts the accumulated
// eshu_dp_postgres_query_duration_seconds write observation count EXCEEDS the
// committed budget.
func TestCostBudget_ContainerImageIdentity_N1_ExceedsBudget(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, containerImageIdentityBudgetRelPath)
	decisions := containerImageIdentityFixtureDecisions()
	if len(decisions) < 2 {
		t.Fatalf("N+1 control needs >=2 decisions to exceed the budget; fixture has %d", len(decisions))
	}

	writer, _, reader := newInstrumentedContainerImageIdentityWriter(t)

	for _, decision := range decisions {
		if _, err := writer.WriteContainerImageIdentityDecisions(context.Background(), reducer.ContainerImageIdentityWrite{
			IntentID:     containerImageIdentityCostIntentID,
			ScopeID:      "repo:team-api",
			GenerationID: "generation-container-image-identity-cost",
			SourceSystem: "git",
			Cause:        "reducer/container_image_identity",
			Decisions:    []reducer.ContainerImageIdentityDecision{decision},
		}); err != nil {
			t.Fatalf("N+1 WriteContainerImageIdentityDecisions() error = %v", err)
		}
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	writes := collectAttributedHistogramCount(rm, "eshu_dp_postgres_query_duration_seconds", "operation", "write")
	maxWrites, ok := budget.Budgets["eshu_dp_postgres_query_duration_seconds"]
	if !ok {
		t.Fatal("budget has no eshu_dp_postgres_query_duration_seconds entry")
	}

	if writes <= uint64(maxWrites) {
		t.Fatalf(
			"N+1 negative control: eshu_dp_postgres_query_duration_seconds write observations = %d did NOT "+
				"exceed budget %d — budget is too loose to catch N+1 regressions or the negative control is "+
				"generating too few writes; tighten the budget or increase the N+1 fanout",
			writes, maxWrites,
		)
	}

	t.Logf(
		"N+1 negative control passed: eshu_dp_postgres_query_duration_seconds write observations = %d > budget %d "+
			"(N=%d decisions, scenario=%s)",
		writes, maxWrites, len(decisions), budget.Scenario,
	)
}
