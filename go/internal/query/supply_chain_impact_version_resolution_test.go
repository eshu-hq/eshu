// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/truth"
)

// TestSupplyChainVersionResolutionTier proves the #5469 tiered version
// resolution: the judged version/digest for a finding comes from the
// strongest available deployment-truth-tier evidence, weaker present tiers
// are preserved as corroboration (including disagreement), declared_ref
// never fires absent an evidence producer, and a finding with no deployment
// evidence at all reports no tier.
func TestSupplyChainVersionResolutionTier(t *testing.T) {
	t.Parallel()

	digest := "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	ecsARN := "arn:aws:ecs:us-east-1:123456789012:task/demo/bbbbbbbb"

	cases := []struct {
		name              string
		row               SupplyChainImpactFindingRow
		wantTier          truth.DeploymentTruthTier
		wantCorroboration []SupplyChainVersionResolutionCorroboration
	}{
		{
			name: "runtime confirms the same digest CI and config both corroborate and agree",
			row: SupplyChainImpactFindingRow{
				SubjectDigest:            digest,
				CloudRuntimeResourceRefs: []string{ecsARN},
				EvidencePath:             []string{cicdRunCorrelationFactKind},
			},
			wantTier: truth.TierRuntimeConfirmed,
			// config_only also corroborates here (falling back to
			// SubjectDigest since ObservedVersion is blank): a finding with
			// any version/digest at all gets at least a config_only claim,
			// matching deployment_truth_tier's own hasDeploymentAnchor rule
			// (SubjectDigest alone qualifies).
			wantCorroboration: []SupplyChainVersionResolutionCorroboration{
				{
					Tier:            string(truth.TierProvenanceCIDeclared),
					DigestOrVersion: digest,
					EvidenceKind:    "cicd_run_correlation",
					Agrees:          true,
				},
				{
					Tier:            string(truth.TierConfigOnly),
					DigestOrVersion: digest,
					EvidenceKind:    "config_materialization",
					Agrees:          true,
				},
			},
		},
		{
			name: "CI-declared wins over a disagreeing config-only version",
			row: SupplyChainImpactFindingRow{
				SubjectDigest:   digest,
				ObservedVersion: "1.2.3",
				WorkloadIDs:     []string{"workload:example-api"},
				Environments:    []string{"prod"},
				EvidencePath:    []string{cicdRunCorrelationFactKind},
			},
			wantTier: truth.TierProvenanceCIDeclared,
			wantCorroboration: []SupplyChainVersionResolutionCorroboration{
				{
					Tier:            string(truth.TierConfigOnly),
					DigestOrVersion: "1.2.3",
					EvidenceKind:    "config_materialization",
					Agrees:          false,
				},
			},
		},
		{
			name: "config-only is the floor when only a version anchor exists",
			row: SupplyChainImpactFindingRow{
				ObservedVersion: "4.5.6",
				WorkloadIDs:     []string{"workload:example-api"},
				Environments:    []string{"prod"},
			},
			wantTier:          truth.TierConfigOnly,
			wantCorroboration: nil,
		},
		{
			name: "CI evidence hop without artifact identity contributes no version claim",
			row: SupplyChainImpactFindingRow{
				ObservedVersion: "7.8.9",
				DeploymentIDs:   []string{"deployment:example-api"},
				Environments:    []string{"prod"},
				EvidencePath:    []string{cicdRunCorrelationFactKind},
			},
			// deployment_truth_tier would report provenance_ci_declared here
			// (the evidence hop alone is enough, see
			// rowHasCIDeclaredDeploymentEvidence), but version resolution
			// requires a concrete digest/version claim, and this CI hop
			// matched only via repository+environment+operational anchor
			// with no artifact identity (#5426's weak branch) -- so it makes
			// no claim and the resolver falls through to the config floor
			// instead of fabricating one.
			wantTier:          truth.TierConfigOnly,
			wantCorroboration: nil,
		},
		{
			name:              "no deployment evidence at all reports no tier",
			row:               SupplyChainImpactFindingRow{},
			wantTier:          "",
			wantCorroboration: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotTier, gotCorroboration := supplyChainVersionResolution(tc.row)
			if gotTier != string(tc.wantTier) {
				t.Fatalf("tier = %q, want %q", gotTier, tc.wantTier)
			}
			if len(gotCorroboration) != len(tc.wantCorroboration) {
				t.Fatalf("corroboration = %+v, want %+v", gotCorroboration, tc.wantCorroboration)
			}
			for i, want := range tc.wantCorroboration {
				if gotCorroboration[i] != want {
					t.Fatalf("corroboration[%d] = %+v, want %+v", i, gotCorroboration[i], want)
				}
			}
		})
	}
}

// TestSupplyChainVersionResolutionDeclaredRefNeverEmitted proves the #5393
// fail-closed rule: declared_ref has no evidence producer today, so no row
// shape -- however many other tiers' evidence it carries -- may cause the
// resolver to report declared_ref as the winning tier or as a corroboration
// entry. Ownership of wiring real DEPLOYS_REF evidence belongs to #5393; this
// resolver must never invent it in the meantime.
func TestSupplyChainVersionResolutionDeclaredRefNeverEmitted(t *testing.T) {
	t.Parallel()

	digest := "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	ecsARN := "arn:aws:ecs:us-east-1:123456789012:task/demo/cccccccc"

	rows := []SupplyChainImpactFindingRow{
		{},
		{ObservedVersion: "1.0.0", Environments: []string{"prod"}},
		{SubjectDigest: digest, EvidencePath: []string{cicdRunCorrelationFactKind}},
		{
			SubjectDigest:            digest,
			CloudRuntimeResourceRefs: []string{ecsARN},
			EvidencePath:             []string{cicdRunCorrelationFactKind},
			ObservedVersion:          "2.0.0",
			WorkloadIDs:              []string{"workload:example-api"},
			Environments:             []string{"prod"},
		},
	}

	for i, row := range rows {
		tier, corroboration := supplyChainVersionResolution(row)
		if tier == string(truth.TierDeclaredRef) {
			t.Fatalf("row[%d]: version_resolution_tier fired declared_ref with no evidence producer", i)
		}
		for _, entry := range corroboration {
			if entry.Tier == string(truth.TierDeclaredRef) {
				t.Fatalf("row[%d]: corroboration entry fired declared_ref with no evidence producer: %+v", i, entry)
			}
		}
	}
}

// TestBuildSupplyChainImpactFindingResultSetsVersionResolution proves the
// resolver is wired into buildSupplyChainImpactFindingResult, the same
// pattern DeploymentTruthTier already uses (#5452).
func TestBuildSupplyChainImpactFindingResultSetsVersionResolution(t *testing.T) {
	t.Parallel()

	digest := "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	row := SupplyChainImpactFindingRow{
		SubjectDigest: digest,
		EvidencePath:  []string{cicdRunCorrelationFactKind},
	}

	result := buildSupplyChainImpactFindingResult(row)
	if result.VersionResolutionTier != string(truth.TierProvenanceCIDeclared) {
		t.Fatalf("VersionResolutionTier = %q, want %q", result.VersionResolutionTier, truth.TierProvenanceCIDeclared)
	}
}

// BenchmarkBuildSupplyChainImpactFindingResult measures result-assembly cost
// per row for the #5469 performance proof: the resolver classifies fields the
// row already carries with no new graph or Postgres query, so per-row cost
// should stay a small, roughly constant addition over the pre-#5469 baseline
// (see the same benchmark run against the parent commit for the before
// number). The row exercises every field the resolver reads, including
// enough tiers present at once to walk the full candidate/corroboration loop.
func BenchmarkBuildSupplyChainImpactFindingResult(b *testing.B) {
	digest := "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	row := SupplyChainImpactFindingRow{
		FindingID:                "finding-bench",
		CVEID:                    "CVE-2026-00099",
		SubjectDigest:            digest,
		ImageRef:                 "registry.example.com/demo/app:v1",
		ObservedVersion:          "1.2.3",
		CloudRuntimeResourceRefs: []string{"arn:aws:ecs:us-east-1:123456789012:task/demo/bench"},
		WorkloadIDs:              []string{"workload:example-api"},
		DeploymentIDs:            []string{"deployment:example-api"},
		ServiceIDs:               []string{"service:example-api"},
		Environments:             []string{"prod"},
		EvidencePath:             []string{cicdRunCorrelationFactKind, "reducer_platform_materialization"},
		MissingEvidence:          []string{serviceCatalogCorrelationMissingReason},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = buildSupplyChainImpactFindingResult(row)
	}
}
