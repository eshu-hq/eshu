// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// gcpRelationshipEnvelope builds a gcp_cloud_relationship fact envelope for
// tests from the bounded set of payload fields the edge projection reads.
func gcpRelationshipEnvelope(payload map[string]any) facts.Envelope {
	return facts.Envelope{
		FactKind: facts.GCPCloudRelationshipFactKind,
		Payload:  payload,
	}
}

func gcpRelationshipIntent() Intent {
	return Intent{
		IntentID:     "intent-gcp-edges-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainGCPRelationshipMaterialization,
		EntityKeys:   []string{"gcp_resource_materialization:scope-1"},
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	}
}

const (
	gcpInstanceFullName = "//compute.googleapis.com/projects/demo-proj/zones/us-central1-a/instances/app"
	gcpDiskFullName     = "//compute.googleapis.com/projects/demo-proj/zones/us-central1-a/disks/data"
)

func gcpInstanceResource() facts.Envelope {
	return gcpResourceEnvelope(map[string]any{
		"full_resource_name": gcpInstanceFullName,
		"asset_type":         "compute.googleapis.com/Instance",
		"project_id":         "demo-proj",
		"location":           "us-central1-a",
	})
}

func gcpDiskResource() facts.Envelope {
	return gcpResourceEnvelope(map[string]any{
		"full_resource_name": gcpDiskFullName,
		"asset_type":         "compute.googleapis.com/Disk",
		"project_id":         "demo-proj",
		"location":           "us-central1-a",
	})
}

func gcpInstanceToDisk(supportState string) facts.Envelope {
	return gcpRelationshipEnvelope(map[string]any{
		"source_full_resource_name": gcpInstanceFullName,
		"target_full_resource_name": gcpDiskFullName,
		"relationship_type":         "INSTANCE_TO_DISK",
		"target_asset_type":         "compute.googleapis.com/Disk",
		"support_state":             supportState,
	})
}

func TestGCPRelationshipMaterializationRejectsMismatchedDomain(t *testing.T) {
	t.Parallel()

	handler := GCPRelationshipMaterializationHandler{
		FactLoader:      &stubFactLoader{},
		EdgeWriter:      &recordingCloudResourceEdgeWriter{},
		ReadinessLookup: readyLookup(true, true),
	}
	intent := gcpRelationshipIntent()
	intent.Domain = DomainAWSRelationshipMaterialization
	if _, err := handler.Handle(context.Background(), intent); err == nil {
		t.Fatal("expected error for mismatched domain")
	}
}

func TestGCPRelationshipMaterializationRequiresFactLoader(t *testing.T) {
	t.Parallel()

	handler := GCPRelationshipMaterializationHandler{
		EdgeWriter:      &recordingCloudResourceEdgeWriter{},
		ReadinessLookup: readyLookup(true, true),
	}
	if _, err := handler.Handle(context.Background(), gcpRelationshipIntent()); err == nil {
		t.Fatal("expected error when fact loader is nil")
	}
}

func TestGCPRelationshipMaterializationRequiresEdgeWriter(t *testing.T) {
	t.Parallel()

	handler := GCPRelationshipMaterializationHandler{
		FactLoader:      &stubFactLoader{},
		ReadinessLookup: readyLookup(true, true),
	}
	if _, err := handler.Handle(context.Background(), gcpRelationshipIntent()); err == nil {
		t.Fatal("expected error when edge writer is nil")
	}
}

func TestGCPRelationshipMaterializationGatesOnCanonicalNodesPhase(t *testing.T) {
	t.Parallel()

	writer := &recordingCloudResourceEdgeWriter{}
	handler := GCPRelationshipMaterializationHandler{
		FactLoader:      &stubFactLoader{},
		EdgeWriter:      writer,
		ReadinessLookup: readyLookup(false, false), // GCP nodes phase not yet committed
	}

	_, err := handler.Handle(context.Background(), gcpRelationshipIntent())
	if err == nil {
		t.Fatal("expected a retryable error while canonical nodes phase is not ready")
	}
	if !IsRetryable(err) {
		t.Fatalf("error must be retryable so the intent re-enters the queue, got %v", err)
	}
	if writer.writeCalls != 0 || writer.retractCalls != 0 {
		t.Fatalf("no graph writes allowed before nodes commit: write=%d retract=%d", writer.writeCalls, writer.retractCalls)
	}
}

func TestGCPRelationshipMaterializationProjectsResolvedEdges(t *testing.T) {
	t.Parallel()

	writer := &recordingCloudResourceEdgeWriter{}
	handler := GCPRelationshipMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			gcpInstanceResource(), gcpDiskResource(), gcpInstanceToDisk("supported"),
		}},
		EdgeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	result, err := handler.Handle(context.Background(), gcpRelationshipIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", result.Status)
	}
	if writer.writeCalls != 1 {
		t.Fatalf("writeCalls = %d, want 1", writer.writeCalls)
	}
	if len(writer.writtenRows) != 1 {
		t.Fatalf("written edge rows = %d, want 1", len(writer.writtenRows))
	}
	if writer.writeEvidence != gcpRelationshipEvidenceSource {
		t.Fatalf("write evidence = %q, want %q", writer.writeEvidence, gcpRelationshipEvidenceSource)
	}
	if writer.writeScopeID != "scope-1" || writer.writeGenerationID != "gen-1" {
		t.Fatalf("write scope/gen = %q/%q, want scope-1/gen-1", writer.writeScopeID, writer.writeGenerationID)
	}
	if got := anyToString(writer.writtenRows[0]["relationship_type"]); got != "INSTANCE_TO_DISK" {
		t.Fatalf("relationship_type = %q", got)
	}
	if got := anyToString(writer.writtenRows[0]["support_state"]); got != "supported" {
		t.Fatalf("support_state = %q, want supported", got)
	}
	if result.CanonicalWrites != 1 {
		t.Fatalf("CanonicalWrites = %d, want 1", result.CanonicalWrites)
	}
}

func TestGCPRelationshipMaterializationUnresolvedTargetNotWritten(t *testing.T) {
	t.Parallel()

	// Disk resource is not scanned in this generation, so the target is unresolved.
	writer := &recordingCloudResourceEdgeWriter{}
	handler := GCPRelationshipMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			gcpInstanceResource(), gcpInstanceToDisk("supported"),
		}},
		EdgeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	result, err := handler.Handle(context.Background(), gcpRelationshipIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if writer.writeCalls != 0 {
		t.Fatalf("writeCalls = %d, want 0 — unresolved target must not write", writer.writeCalls)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("status = %q, want succeeded (graceful degrade, not failure)", result.Status)
	}
}

func TestGCPRelationshipMaterializationPartialTargetNotWritten(t *testing.T) {
	t.Parallel()

	// Both endpoints scanned, but the provider marked the relationship partial,
	// so the collector contract says the target must be treated as unresolved.
	writer := &recordingCloudResourceEdgeWriter{}
	handler := GCPRelationshipMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			gcpInstanceResource(), gcpDiskResource(), gcpInstanceToDisk("partial"),
		}},
		EdgeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	if _, err := handler.Handle(context.Background(), gcpRelationshipIntent()); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if writer.writeCalls != 0 {
		t.Fatalf("writeCalls = %d, want 0 — partial relationships must not materialize", writer.writeCalls)
	}
}

func TestGCPRelationshipMaterializationUnsupportedNotWritten(t *testing.T) {
	t.Parallel()

	writer := &recordingCloudResourceEdgeWriter{}
	handler := GCPRelationshipMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			gcpInstanceResource(), gcpDiskResource(), gcpInstanceToDisk("unsupported"),
		}},
		EdgeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	if _, err := handler.Handle(context.Background(), gcpRelationshipIntent()); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if writer.writeCalls != 0 {
		t.Fatalf("writeCalls = %d, want 0 — unsupported relationships are provenance only", writer.writeCalls)
	}
}

func TestGCPRelationshipMaterializationRetractsPriorGenerationEdges(t *testing.T) {
	t.Parallel()

	writer := &recordingCloudResourceEdgeWriter{}
	handler := GCPRelationshipMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			gcpInstanceResource(), gcpDiskResource(), gcpInstanceToDisk("supported"),
		}},
		EdgeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	if _, err := handler.Handle(context.Background(), gcpRelationshipIntent()); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if writer.retractCalls != 1 {
		t.Fatalf("retractCalls = %d, want 1 when a prior generation exists", writer.retractCalls)
	}
	if writer.retractEvidence != gcpRelationshipEvidenceSource {
		t.Fatalf("retract evidence = %q, want %q", writer.retractEvidence, gcpRelationshipEvidenceSource)
	}
	if len(writer.retractScopeIDs) != 1 || writer.retractScopeIDs[0] != "scope-1" {
		t.Fatalf("retract scope ids = %v, want [scope-1]", writer.retractScopeIDs)
	}
}

func TestGCPRelationshipMaterializationSkipsFirstGenerationRetract(t *testing.T) {
	t.Parallel()

	writer := &recordingCloudResourceEdgeWriter{}
	handler := GCPRelationshipMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			gcpInstanceResource(), gcpDiskResource(), gcpInstanceToDisk("supported"),
		}},
		EdgeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return false, nil },
	}

	if _, err := handler.Handle(context.Background(), gcpRelationshipIntent()); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if writer.retractCalls != 0 {
		t.Fatalf("retractCalls = %d, want 0 on the first generation", writer.retractCalls)
	}
	if writer.writeCalls != 1 {
		t.Fatalf("writeCalls = %d, want 1", writer.writeCalls)
	}
}

func TestGCPRelationshipMaterializationEmptyGenerationIsNoOp(t *testing.T) {
	t.Parallel()

	writer := &recordingCloudResourceEdgeWriter{}
	handler := GCPRelationshipMaterializationHandler{
		FactLoader:           &stubFactLoader{},
		EdgeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	result, err := handler.Handle(context.Background(), gcpRelationshipIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if writer.writeCalls != 0 {
		t.Fatalf("writeCalls = %d, want 0 for empty generation", writer.writeCalls)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", result.Status)
	}
}

func TestGCPRelationshipMaterializationPropagatesWriteError(t *testing.T) {
	t.Parallel()

	writer := &recordingCloudResourceEdgeWriter{writeErr: errors.New("boom")}
	handler := GCPRelationshipMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			gcpInstanceResource(), gcpDiskResource(), gcpInstanceToDisk("supported"),
		}},
		EdgeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	if _, err := handler.Handle(context.Background(), gcpRelationshipIntent()); err == nil {
		t.Fatal("expected the write error to propagate")
	}
}

func TestExtractGCPRelationshipEdgeRowsEmptyIsNil(t *testing.T) {
	t.Parallel()

	rows, tally := ExtractGCPRelationshipEdgeRows(nil, nil)
	if rows != nil {
		t.Fatalf("rows = %v, want nil", rows)
	}
	if tally.resolvedCount() != 0 || tally.skippedCount() != 0 {
		t.Fatalf("tally = %+v, want empty", tally)
	}
}

func TestExtractGCPRelationshipEdgeRowsDeduplicatesAndSkipsSelfLoop(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{gcpInstanceResource(), gcpDiskResource()}
	rels := []facts.Envelope{
		gcpInstanceToDisk("supported"),
		gcpInstanceToDisk("supported"), // duplicate
		gcpRelationshipEnvelope(map[string]any{ // self-loop: instance -> instance
			"source_full_resource_name": gcpInstanceFullName,
			"target_full_resource_name": gcpInstanceFullName,
			"relationship_type":         "SELF",
			"target_asset_type":         "compute.googleapis.com/Instance",
			"support_state":             "supported",
		}),
	}

	rows, tally := ExtractGCPRelationshipEdgeRows(resources, rels)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 (duplicate + self-loop collapse to one edge)", len(rows))
	}
	if tally.resolvedCount() != 1 {
		t.Fatalf("resolvedCount = %d, want 1", tally.resolvedCount())
	}
}

// TestGCPRelationshipMaterializationMetricCarriesRelationshipTypeAndJoinMode
// pins the eshu_dp_gcp_relationship_edges_total contract: every data point is
// labeled by BOTH the real relationship_type and the join_mode. The
// relationship_type label must never carry target_asset_type values.
func TestGCPRelationshipMaterializationMetricCarriesRelationshipTypeAndJoinMode(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}

	// Resolved instance->disk edge; an unresolved network target (not scanned);
	// and a partial relationship the provider marked opaque.
	unresolvedNetwork := gcpRelationshipEnvelope(map[string]any{
		"source_full_resource_name": gcpInstanceFullName,
		"target_full_resource_name": "//compute.googleapis.com/projects/demo-proj/global/networks/not-scanned",
		"relationship_type":         "USES_NETWORK",
		"target_asset_type":         "compute.googleapis.com/Network",
		"support_state":             "supported",
	})

	handler := GCPRelationshipMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			gcpInstanceResource(), gcpDiskResource(),
			gcpInstanceToDisk("supported"), unresolvedNetwork, gcpInstanceToDisk("partial"),
		}},
		EdgeWriter:           &recordingCloudResourceEdgeWriter{},
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
		Instruments:          inst,
	}

	if _, err := handler.Handle(context.Background(), gcpRelationshipIntent()); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	const counter = "eshu_dp_gcp_relationship_edges_total"
	if !metricHasAttrs(rm, counter, map[string]string{
		telemetry.MetricDimensionRelationshipType: "INSTANCE_TO_DISK",
		telemetry.MetricDimensionJoinMode:         "full_resource_name",
	}) {
		t.Fatal("resolved edge must emit (relationship_type=INSTANCE_TO_DISK, join_mode=full_resource_name)")
	}
	if !metricHasAttrs(rm, counter, map[string]string{
		telemetry.MetricDimensionRelationshipType: "USES_NETWORK",
		telemetry.MetricDimensionJoinMode:         "unresolved",
	}) {
		t.Fatal("unresolved relationship must emit (relationship_type=USES_NETWORK, join_mode=unresolved)")
	}
	if !metricHasAttrs(rm, counter, map[string]string{
		telemetry.MetricDimensionRelationshipType: "INSTANCE_TO_DISK",
		telemetry.MetricDimensionJoinMode:         "partial",
	}) {
		t.Fatal("partial relationship must emit (relationship_type=INSTANCE_TO_DISK, join_mode=partial)")
	}
	for _, leaked := range []string{"compute.googleapis.com/Disk", "compute.googleapis.com/Network"} {
		if metricHasAttrs(rm, counter, map[string]string{telemetry.MetricDimensionRelationshipType: leaked}) {
			t.Fatalf("target_asset_type %q leaked into the relationship_type label", leaked)
		}
	}
}

func TestExtractGCPRelationshipEdgeRowsSkipsInvalidRelationshipType(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{gcpInstanceResource(), gcpDiskResource()}
	rels := []facts.Envelope{
		gcpRelationshipEnvelope(map[string]any{
			"source_full_resource_name": gcpInstanceFullName,
			"target_full_resource_name": gcpDiskFullName,
			"relationship_type":         "bad type`)//",
			"target_asset_type":         "compute.googleapis.com/Disk",
			"support_state":             "supported",
		}),
	}

	rows, tally := ExtractGCPRelationshipEdgeRows(resources, rels)
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0 — unsafe relationship type must be skipped", len(rows))
	}
	if tally.byMode[gcpJoinModeInvalidType] != 1 {
		t.Fatalf("invalid_type tally = %d, want 1", tally.byMode[gcpJoinModeInvalidType])
	}
}
