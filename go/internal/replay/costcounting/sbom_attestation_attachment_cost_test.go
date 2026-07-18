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

// sbomAttestationAttachmentBudgetRelPath is the committed cost budget for the
// sbom_attestation_attachment scenario (C-14 issue #4367, Tier-2 Postgres cost
// slice). PostgresSBOMAttestationAttachmentWriter operates over
// []SBOMAttestationAttachmentDecision Go values, not a
// CanonicalMaterialization, so the fixture decisions live inline in this
// file, matching the container_image_identity_cost_test.go convention.
var sbomAttestationAttachmentBudgetRelPath = filepath.Join(
	"..", "..", "..", "..", "testdata", "cassettes", "replayoffline", "sbom-attestation-attachment.cost-budget.json",
)

const sbomAttestationAttachmentCostIntentID = "intent-sbom-attestation-attachment-cost"

// sbomAttestationAttachmentFixtureDecisions is the deterministic input for the
// positive and N+1 scenarios: two verified attachment decisions for distinct
// SBOM documents in one scope. WriteSBOMAttestationAttachments persists every
// attachment status regardless of outcome
// (go/internal/reducer/sbom_attestation_attachment_writer.go), so both rows
// are written.
func sbomAttestationAttachmentFixtureDecisions() []reducer.SBOMAttestationAttachmentDecision {
	row := func(id string) reducer.SBOMAttestationAttachmentDecision {
		return reducer.SBOMAttestationAttachmentDecision{
			DocumentID:         "sbom-doc-" + id,
			DocumentDigest:     "sha256:" + id,
			SubjectDigest:      "sha256:subject-" + id,
			AttachmentStatus:   reducer.SBOMAttachmentAttachedVerified,
			ParseStatus:        "parsed",
			VerificationStatus: "verified",
			VerificationPolicy: "keyless",
			ArtifactKind:       "container_image",
			Format:             "cyclonedx",
			SpecVersion:        "1.5",
			CanonicalWrites:    1,
			ComponentCount:     3,
			RepositoryIDs:      []string{"repo:team-api"},
			SourceLayerKinds:   []string{"observed_resource"},
		}
	}
	return []reducer.SBOMAttestationAttachmentDecision{row("aaaa"), row("bbbb")}
}

// newInstrumentedSBOMAttestationAttachmentWriter builds the PRODUCTION
// Postgres write dispatch for this domain:
// reducer.PostgresSBOMAttestationAttachmentWriter over a
// postgres.InstrumentedDB (StoreName "reducer") wrapping a
// countingExecQueryer. WriteSBOMAttestationAttachments calls
// reducerBatchInsertFacts — the SAME bounded chunked bulk insert
// container_image_identity and ci_cd_run_correlation share — so two rows fit
// in one 1000-row chunk and cost exactly one ExecContext round-trip.
func newInstrumentedSBOMAttestationAttachmentWriter(t *testing.T) (
	writer reducer.PostgresSBOMAttestationAttachmentWriter,
	fake *countingExecQueryer,
	reader *sdkmetric.ManualReader,
) {
	t.Helper()

	fake = &countingExecQueryer{}
	db, manualReader := newInstrumentedReducerDB(t, fake)
	writer = reducer.PostgresSBOMAttestationAttachmentWriter{
		DB:  db,
		Now: func() time.Time { return time.Date(2026, time.July, 12, 12, 0, 0, 0, time.UTC) },
	}
	return writer, fake, manualReader
}

// TestCostBudget_SBOMAttestationAttachment is the positive cost-counting gate
// for the sbom_attestation_attachment reducer projection. It drives the
// production
// PostgresSBOMAttestationAttachmentWriter.WriteSBOMAttestationAttachments over
// two decisions in one scope, through a real InstrumentedDB-backed
// sdkmetric.ManualReader, then asserts
// eshu_dp_postgres_query_duration_seconds's write-attributed observation
// count is within the committed budget.
func TestCostBudget_SBOMAttestationAttachment(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, sbomAttestationAttachmentBudgetRelPath)
	writer, fake, reader := newInstrumentedSBOMAttestationAttachmentWriter(t)

	result, err := writer.WriteSBOMAttestationAttachments(context.Background(), reducer.SBOMAttestationAttachmentWrite{
		IntentID:     sbomAttestationAttachmentCostIntentID,
		ScopeID:      "repo:team-api",
		GenerationID: "generation-sbom-attestation-attachment-cost",
		SourceSystem: "syft",
		Cause:        "reducer/sbom_attestation_attachment",
		Decisions:    sbomAttestationAttachmentFixtureDecisions(),
	})
	if err != nil {
		t.Fatalf("WriteSBOMAttestationAttachments() error = %v", err)
	}
	if result.FactsWritten != 2 {
		t.Fatalf("FactsWritten = %d, want 2", result.FactsWritten)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	// PRIMARY assertion: read eshu_dp_postgres_query_duration_seconds's
	// write-attributed observation count off the real otel reader.
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

// TestCostBudget_SBOMAttestationAttachment_N1_ExceedsBudget is the mandatory
// negative control, run through the SAME production batched dispatch as the
// positive test. It calls WriteSBOMAttestationAttachments once per fixture
// decision instead of once for the whole batch, and asserts the accumulated
// eshu_dp_postgres_query_duration_seconds write observation count EXCEEDS the
// committed budget.
func TestCostBudget_SBOMAttestationAttachment_N1_ExceedsBudget(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, sbomAttestationAttachmentBudgetRelPath)
	decisions := sbomAttestationAttachmentFixtureDecisions()
	if len(decisions) < 2 {
		t.Fatalf("N+1 control needs >=2 decisions to exceed the budget; fixture has %d", len(decisions))
	}

	writer, _, reader := newInstrumentedSBOMAttestationAttachmentWriter(t)

	for _, decision := range decisions {
		if _, err := writer.WriteSBOMAttestationAttachments(context.Background(), reducer.SBOMAttestationAttachmentWrite{
			IntentID:     sbomAttestationAttachmentCostIntentID,
			ScopeID:      "repo:team-api",
			GenerationID: "generation-sbom-attestation-attachment-cost",
			SourceSystem: "syft",
			Cause:        "reducer/sbom_attestation_attachment",
			Decisions:    []reducer.SBOMAttestationAttachmentDecision{decision},
		}); err != nil {
			t.Fatalf("N+1 WriteSBOMAttestationAttachments() error = %v", err)
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
