// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func TestIAMCanPerformConditionedIdentityGrantIsProvenanceOnly(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		attackerNode(),
		canPerformNode(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN),
	}
	perms := []facts.Envelope{
		escalationPermissionEnvelope(
			attackerUserARN,
			"Allow",
			[]string{"s3:getobject"},
			[]string{canPerformBucketARN},
			withConditionSummary([]string{"aws:SourceIp"}, []string{"IpAddress"}),
		),
	}

	result := ExtractIAMCanPerformEdges(resources, perms)
	if len(result.Edges) != 0 {
		t.Fatalf("conditioned grant must remain provenance-only, got edges %v", result.Edges)
	}
	if result.Tally.skippedConditioned != 1 {
		t.Fatalf("skippedConditioned = %d, want 1 (tally=%+v)", result.Tally.skippedConditioned, result.Tally)
	}
	if result.Tally.conditionedProvenanceOnly != 1 {
		t.Fatalf("conditionedProvenanceOnly = %d, want 1 (tally=%+v)", result.Tally.conditionedProvenanceOnly, result.Tally)
	}
}

func TestIAMCanPerformConditionedResourcePolicyGrantIsProvenanceOnly(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		attackerNode(),
		canPerformNode(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN),
	}
	resourcePolicies := []facts.Envelope{
		canPerformResourcePolicyEnvelope(
			canPerformBucketARN,
			iamCanPerformResourceTypeS3Bucket,
			"Allow",
			[]string{"s3:getobject"},
			[]string{attackerUserARN},
			withConditionSummary([]string{"aws:SourceVpc"}, []string{"UnknownFutureOperator"}),
		),
	}

	result := ExtractIAMCanPerformEdges(resources, nil, resourcePolicies)
	if len(result.Edges) != 0 {
		t.Fatalf("conditioned resource-policy grant must remain provenance-only, got edges %v", result.Edges)
	}
	if result.Tally.skippedConditioned != 1 {
		t.Fatalf("skippedConditioned = %d, want 1 (tally=%+v)", result.Tally.skippedConditioned, result.Tally)
	}
	if result.Tally.conditionedProvenanceOnly != 1 {
		t.Fatalf("conditionedProvenanceOnly = %d, want 1 (tally=%+v)", result.Tally.conditionedProvenanceOnly, result.Tally)
	}
}

func TestIAMCanPerformConditionedGrantRecordsProvenanceOnlyMetric(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}

	envs := []facts.Envelope{
		attackerNode(),
		canPerformNode(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN),
		escalationPermissionEnvelope(
			attackerUserARN,
			"Allow",
			[]string{"s3:getobject"},
			[]string{canPerformBucketARN},
			withConditionSummary([]string{"aws:PrincipalTag/team"}, []string{"StringEquals"}),
		),
	}
	handler := IAMCanPerformMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: envs},
		Writer:               &recordingIAMCanPerformWriter{},
		ReadinessLookup:      allKeyspacesReady(),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return false, nil },
		Instruments:          instruments,
	}
	if _, err := handler.Handle(context.Background(), iamCanPerformIntent()); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	if !metricHasAttrs(rm, "eshu_dp_iam_can_perform_conditioned_total", map[string]string{
		telemetry.MetricDimensionConfidence: iamCanPerformConditionConfidenceProvenanceOnly,
	}) {
		t.Fatal("expected eshu_dp_iam_can_perform_conditioned_total{confidence=provenance_only}")
	}
}

func withConditionSummary(keys, operators []string) func(map[string]any) {
	return func(p map[string]any) {
		p["condition_keys"] = toAnySlice(keys)
		p["condition_operators"] = toAnySlice(operators)
		p["condition_operator_count"] = len(operators)
		p["has_conditions"] = len(keys) > 0 || len(operators) > 0
	}
}
