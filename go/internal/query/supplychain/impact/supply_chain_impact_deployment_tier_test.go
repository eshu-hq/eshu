// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/truth"
)

// TestSupplyChainDeploymentTruthTierDistinguishesRuntimeCIAndConfig proves the
// #5452 tier distinction: runtime-observed cloud evidence, CI-declared
// deployment evidence, and config-only anchors classify as three DISTINCT
// deployment_truth_tier values, instead of every deployment-anchored finding
// collapsing to config_only as it did before #5452.
func TestSupplyChainDeploymentTruthTierDistinguishesRuntimeCIAndConfig(t *testing.T) {
	t.Parallel()

	digest := "sha256:ccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	ecsARN := "arn:aws:ecs:us-east-1:123456789012:task/demo/aaaaaaaa"

	cases := []struct {
		name string
		row  SupplyChainImpactFindingRow
		want truth.DeploymentTruthTier
	}{
		{
			name: "runtime-observed cloud resource is runtime_confirmed",
			row: SupplyChainImpactFindingRow{
				SubjectDigest:            digest,
				CloudRuntimeResourceRefs: []string{ecsARN},
				EvidencePath:             []string{cicdRunCorrelationFactKind},
			},
			want: truth.TierRuntimeConfirmed,
		},
		{
			name: "runtime evidence beats a co-present CI-declared hop",
			row: SupplyChainImpactFindingRow{
				SubjectDigest:            digest,
				CloudRuntimeResourceRefs: []string{ecsARN},
			},
			want: truth.TierRuntimeConfirmed,
		},
		{
			name: "CI-declared deployment hop is provenance_ci_declared",
			row: SupplyChainImpactFindingRow{
				SubjectDigest: digest,
				DeploymentIDs: []string{"deployment:example-api"},
				Environments:  []string{"prod"},
				EvidencePath:  []string{cicdRunCorrelationFactKind},
			},
			want: truth.TierProvenanceCIDeclared,
		},
		{
			name: "config-only anchors without CI or runtime evidence stay config_only",
			row: SupplyChainImpactFindingRow{
				WorkloadIDs:  []string{"workload:example-api"},
				Environments: []string{"prod"},
				EvidencePath: []string{"reducer_platform_materialization"},
			},
			want: truth.TierConfigOnly,
		},
		{
			name: "no deployment anchor at all classifies as no tier",
			row:  SupplyChainImpactFindingRow{},
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := BuildSupplyChainImpactFindingResult(&tc.row).DeploymentTruthTier
			if got != string(tc.want) {
				t.Fatalf("DeploymentTruthTier = %q, want %q", got, tc.want)
			}
		})
	}
}
