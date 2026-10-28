// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"reflect"
	"slices"
	"testing"

	"go.opentelemetry.io/otel/attribute"
)

// TestAttrHelpersCarryValue proves every uncovered Attr helper passes its
// argument through under its dimension key.
func TestAttrHelpersCarryValue(t *testing.T) {
	t.Parallel()

	strings := []struct {
		name string
		got  attribute.KeyValue
		key  attribute.Key
	}{
		{"field_class", AttrFieldClass("code"), MetricDimensionFieldClass},
		{"collector_instance", AttrCollectorInstanceID("i-1"), MetricDimensionCollectorInstanceID},
		{"fairness_key", AttrFairnessKey("k"), LogKeyFairnessKey},
		{"pacing_signal", AttrPacingSignal("s"), MetricDimensionPacingSignal},
		{"execution_mode", AttrExecutionMode("m"), MetricDimensionExecutionMode},
		{"repo_size_tier", AttrRepoSizeTier("t"), MetricDimensionRepoSizeTier},
		{"skip_reason", AttrSkipReason("r"), MetricDimensionSkipReason},
		{"node_type", AttrNodeType("n"), MetricDimensionNodeType},
		{"edge_type", AttrEdgeType("e"), MetricDimensionEdgeType},
		{"relationship_type", AttrRelationshipType("r"), MetricDimensionRelationshipType},
		{"join_mode", AttrJoinMode("j"), MetricDimensionJoinMode},
		{"ownership_family", AttrOwnershipFamily("o"), MetricDimensionOwnershipFamily},
		{"coverage_signal", AttrCoverageSignal("c"), MetricDimensionCoverageSignal},
		{"resolution_mode", AttrResolutionMode("r"), MetricDimensionResolutionMode},
		{"principal_kind", AttrPrincipalKind("p"), MetricDimensionPrincipalKind},
		{"endpoint_kind", AttrEndpointKind("e"), MetricDimensionEndpointKind},
		{"drift_kind", AttrDriftKind("d"), MetricDimensionDriftKind},
		{"table", AttrTable("t"), MetricDimensionTable},
		{"read", AttrRead("r"), MetricDimensionRead},
		{"node_label", AttrNodeLabel("n"), MetricDimensionNodeLabel},
		{"property", AttrProperty("p"), MetricDimensionProperty},
		{"branch", AttrBranch("b"), MetricDimensionBranch},
		{"quota_kind", AttrQuotaKind("q"), MetricDimensionQuotaKind},
		{"auth_path", AttrAuthPath("a"), MetricDimensionAuthPath},
		{"gate", AttrGate("g"), MetricDimensionGate},
		{"resource_scope", AttrResourceScope("s"), MetricDimensionResourceScope},
		{"fact_kind", AttrFactKind("f"), MetricDimensionFactKind},
		{"route", AttrRoute("r"), MetricDimensionRoute},
		{"media_family", AttrMediaFamily("m"), MetricDimensionMediaFamily},
		{"artifact_family", AttrArtifactFamily("a"), MetricDimensionArtifactFamily},
		{"composite_skip", AttrCompositeSkipReason("c"), MetricDimensionCompositeSkipReason},
		{"phase", AttrPhase("preflight"), MetricDimensionPhase},
		{"stage", AttrStage("parse"), "stage"},
		{"resolved_by", AttrResolvedBy("r"), MetricDimensionResolvedBy},
		{"search_path", AttrSearchPath("p"), MetricDimensionSearchPath},
		{"metadata_path", AttrMetadataPath("p"), MetricDimensionMetadataPath},
		{"classification", AttrClassification("c"), MetricDimensionClassification},
		{"writer", AttrWriter("w"), MetricDimensionWriter},
		{"direction", AttrDirection("d"), MetricDimensionDirection},
		{"edge_class", AttrEdgeClass("e"), MetricDimensionEdgeClass},
		{"model_key", AttrModelKey("m"), MetricDimensionModelKey},
		{"summary_source", AttrSummarySource("s"), MetricDimensionSource},
		{"summary_reason", AttrSummaryReason("r"), MetricDimensionReason},
		{"coverage_state", AttrCoverageState("c"), MetricDimensionCoverageState},
	}
	for _, tc := range strings {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.got.Key != tc.key {
				t.Fatalf("attr key = %q, want %q", tc.got.Key, tc.key)
			}
			if tc.got.Value.AsString() == "" {
				t.Fatalf("attr %q dropped its value: %#v", tc.name, tc.got.Value)
			}
		})
	}
	if got := AttrClaimPriority(3); got.Key != MetricDimensionClaimPriority || got.Value.AsInt64() != 3 {
		t.Fatalf("AttrClaimPriority = %#v, want claim_priority=3", got)
	}
	if kinds := SourceFileKinds(); !reflect.DeepEqual(kinds, []string{"code", "package_manifest", "config", "other"}) {
		t.Fatalf("SourceFileKinds = %#v, want the bounded ordered set", kinds)
	}
}

// TestAttrKeysAreRegistered proves every uncovered Attr helper's key is a
// member of the frozen dimension or log-key set, so dashboards and log
// queries can resolve what the helpers emit.
func TestAttrKeysAreRegistered(t *testing.T) {
	t.Parallel()

	dimensions := MetricDimensionKeys()
	logs := LogKeys()
	metricAttrs := []attribute.KeyValue{
		AttrFieldClass("x"), AttrCollectorInstanceID("x"), AttrPacingSignal("x"),
		AttrExecutionMode("x"), AttrRepoSizeTier("x"), AttrSkipReason("x"),
		AttrNodeType("x"), AttrEdgeType("x"), AttrRelationshipType("x"),
		AttrJoinMode("x"), AttrOwnershipFamily("x"), AttrCoverageSignal("x"),
		AttrResolutionMode("x"), AttrPrincipalKind("x"), AttrEndpointKind("x"),
		AttrDriftKind("x"), AttrTable("x"), AttrRead("x"), AttrNodeLabel("x"),
		AttrProperty("x"), AttrBranch("x"), AttrQuotaKind("x"), AttrAuthPath("x"),
		AttrGate("x"), AttrResourceScope("x"), AttrFactKind("x"), AttrRoute("x"),
		AttrMediaFamily("x"), AttrArtifactFamily("x"), AttrCompositeSkipReason("x"),
		AttrPhase("x"), AttrResolvedBy("x"), AttrSearchPath("x"), AttrMetadataPath("x"),
		AttrClassification("x"), AttrWriter("x"), AttrDirection("x"), AttrEdgeClass("x"),
		AttrModelKey("x"), AttrSummarySource("x"), AttrSummaryReason("x"), AttrCoverageState("x"),
	}
	for _, attr := range metricAttrs {
		if !slices.Contains(dimensions, string(attr.Key)) {
			t.Errorf("dimension key %q has no registry member", attr.Key)
		}
	}
	if got := AttrClaimPriority(1); !slices.Contains(dimensions, string(got.Key)) {
		t.Errorf("dimension key %q has no registry member", got.Key)
	}
	if got := AttrFairnessKey("x"); !slices.Contains(logs, string(got.Key)) {
		t.Errorf("log key %q has no registry member", got.Key)
	}
}
