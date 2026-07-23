// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestAWSCloudImageMaterializationSkipsUnmaterializedTarget is the
// fail-before/pass-after proof for the #5450 P1 follow-up (telemetry-truth):
// ExtractAWSCloudImageEdgeRows counts a row "resolved" purely from
// ref-parseability, before knowing whether the two-MATCH-MERGE write will
// actually find the target :ContainerImage node. Without the
// ContainerImageExistence filter, a digest whose OCI registry was never
// scanned would still increment eshu_dp_aws_cloud_image_edges_total and
// report a nonzero CanonicalWrites/edge_count, even though the graph has NO
// edge (the write is a silent no-op). This test wires an existence lookup
// that reports the fixture's target uid as ABSENT and asserts the row is
// reclassified as a target-not-materialized skip: zero writes, zero
// CanonicalWrites, the metric does not increment, and the skip is counted.
//
// See TestAWSCloudImageMaterializationProjectsLambdaImageEdgeAndSkipsECSTagOnly
// (aws_cloud_image_materialization_test.go) for the counterpart golden-path
// proof: the same fixture with the target marked EXISTING still increments
// the metric, so this filter does not regress the already-materializing
// case.
func TestAWSCloudImageMaterializationSkipsUnmaterializedTarget(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}

	writer := &recordingCloudResourceContainerImageEdgeWriter{}
	// No uids marked existing: the fixture's Lambda target is absent, exactly
	// like a digest the OCI registry has never scanned.
	existence := &fakeContainerImageExistenceLookup{existing: map[string]struct{}{}}
	handler := AWSCloudImageMaterializationHandler{
		FactLoader:              &stubFactLoader{envelopes: awsCloudImageFixture()},
		EdgeWriter:              writer,
		ReadinessLookup:         readyLookup(true, true),
		PriorGenerationCheck:    func(context.Context, string, string) (bool, error) { return true, nil },
		ContainerImageExistence: existence,
		Instruments:             inst,
	}

	result, err := handler.Handle(context.Background(), awsCloudImageIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if writer.writeCalls != 0 {
		t.Fatalf("writeCalls = %d, want 0: an unmaterialized target must never be written", writer.writeCalls)
	}
	if result.CanonicalWrites != 0 {
		t.Fatalf("CanonicalWrites = %d, want 0: an unmaterialized target must not be counted as a canonical write", result.CanonicalWrites)
	}
	if existence.calls != 1 {
		t.Fatalf("ContainerImageExistence calls = %d, want 1", existence.calls)
	}
	if len(existence.lastUIDs) != 1 || existence.lastUIDs[0] != awsCloudImageFixtureTargetUID {
		t.Fatalf("ContainerImageExistence queried uids = %v, want [%q]", existence.lastUIDs, awsCloudImageFixtureTargetUID)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if metricHasAttrs(rm, "eshu_dp_aws_cloud_image_edges_total", map[string]string{
		telemetry.MetricDimensionResolutionMode: awsCloudImageResolutionMode,
	}) {
		t.Fatal("eshu_dp_aws_cloud_image_edges_total must NOT increment for an unmaterialized target (over-reporting an edge the graph does not have)")
	}
}

// TestFilterRowsToExistingContainerImageTargets is the direct unit proof on
// the reclassification helper: a row whose target uid is confirmed absent
// moves from tally.resolved to tally.skipped[awsCloudImageSkipTargetNotMaterialized],
// and is dropped from the returned rows; a row whose target uid is confirmed
// present passes through unchanged.
func TestFilterRowsToExistingContainerImageTargets(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{
		{"source_uid": "cr-1", "target_uid": "oci-descriptor://exists@sha256:aa"},
		{"source_uid": "cr-2", "target_uid": "oci-descriptor://missing@sha256:bb"},
	}
	tally := newAWSCloudImageEdgeTally()
	tally.resolved = 2

	handler := AWSCloudImageMaterializationHandler{
		ContainerImageExistence: &fakeContainerImageExistenceLookup{
			existing: map[string]struct{}{"oci-descriptor://exists@sha256:aa": {}},
		},
	}

	filtered, gotTally, err := handler.filterRowsToExistingContainerImageTargets(context.Background(), rows, tally)
	if err != nil {
		t.Fatalf("filterRowsToExistingContainerImageTargets() error = %v", err)
	}
	if len(filtered) != 1 || filtered[0]["target_uid"] != "oci-descriptor://exists@sha256:aa" {
		t.Fatalf("filtered rows = %#v, want only the existing-target row", filtered)
	}
	if gotTally.resolved != 1 {
		t.Fatalf("tally.resolved = %d, want 1", gotTally.resolved)
	}
	if got := gotTally.skipped[awsCloudImageSkipTargetNotMaterialized]; got != 1 {
		t.Fatalf("tally.skipped[target_not_materialized] = %d, want 1", got)
	}
}

// TestFilterRowsToExistingContainerImageTargetsNilLookupIsPassthrough proves
// a nil ContainerImageExistence (test wiring convention, matching
// ReadinessLookup/PriorGenerationCheck) is a no-op passthrough, not a filter
// that silently drops everything.
func TestFilterRowsToExistingContainerImageTargetsNilLookupIsPassthrough(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{{"source_uid": "cr-1", "target_uid": "oci-descriptor://x@sha256:aa"}}
	tally := newAWSCloudImageEdgeTally()
	tally.resolved = 1

	handler := AWSCloudImageMaterializationHandler{}
	filtered, gotTally, err := handler.filterRowsToExistingContainerImageTargets(context.Background(), rows, tally)
	if err != nil {
		t.Fatalf("filterRowsToExistingContainerImageTargets() error = %v", err)
	}
	if len(filtered) != 1 {
		t.Fatalf("filtered rows = %d, want 1 (nil lookup must not filter)", len(filtered))
	}
	if gotTally.resolved != 1 {
		t.Fatalf("tally.resolved = %d, want 1 (nil lookup must not adjust tally)", gotTally.resolved)
	}
}
