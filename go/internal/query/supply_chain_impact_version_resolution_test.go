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
//
// The provenance_ci_declared claim is sourced from CIDeclaredArtifactDigest/
// CIDeclaredImageRef (issue #5469 review finding F1) -- the row fields the
// reducer bakes only for a STRONG-branch cicd_run_correlation match (digest
// or image-ref identity equality), never row.SubjectDigest/ImageRef
// directly. Borrowing the finding's own identity was the F1 bug: it let a
// weak-branch (repository+environment+operational-anchor) CI hop claim a
// digest it never actually asserted, and it made a real CI-vs-runtime digest
// disagreement architecturally impossible to express, since both would read
// the same field.
func TestSupplyChainVersionResolutionTier(t *testing.T) {
	t.Parallel()

	digest := "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	otherDigest := "sha256:99999999999999999999999999999999999999999999999999999999999999"
	ecsARN := "arn:aws:ecs:us-east-1:123456789012:task/demo/bbbbbbbb"

	cases := []struct {
		name              string
		row               SupplyChainImpactFindingRow
		wantTier          truth.DeploymentTruthTier
		wantCorroboration []SupplyChainVersionResolutionCorroboration
	}{
		{
			name: "runtime confirms the same digest a strong-branch CI match baked, both corroborate and agree",
			row: SupplyChainImpactFindingRow{
				SubjectDigest:            digest,
				CloudRuntimeResourceRefs: []string{ecsARN},
				CIDeclaredArtifactDigest: digest,
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
			name: "runtime-observed digest disagrees with a strong-branch CI-declared digest (real same-axis disagreement)",
			row: SupplyChainImpactFindingRow{
				SubjectDigest:            digest,
				CloudRuntimeResourceRefs: []string{ecsARN},
				// The CI-declared deployment's OWN digest genuinely differs
				// from the runtime-observed one -- the reducer bakes this
				// only for a strong image-ref match whose artifact_digest
				// contradicted the finding's SubjectDigest
				// (bakeSupplyChainCIDeclaredArtifactIdentity), so this is a
				// real disagreement the evidence actually asserts, not a
				// fabricated one.
				CIDeclaredArtifactDigest: otherDigest,
			},
			wantTier: truth.TierRuntimeConfirmed,
			wantCorroboration: []SupplyChainVersionResolutionCorroboration{
				{
					Tier:            string(truth.TierProvenanceCIDeclared),
					DigestOrVersion: otherDigest,
					EvidenceKind:    "cicd_run_correlation",
					Agrees:          false,
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
				SubjectDigest:            digest,
				CIDeclaredArtifactDigest: digest,
				ObservedVersion:          "1.2.3",
				WorkloadIDs:              []string{"workload:example-api"},
				Environments:             []string{"prod"},
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
			name: "weak-branch CI hop with no baked digest contributes no version claim, even with a digest-bearing finding",
			row: SupplyChainImpactFindingRow{
				// SubjectDigest is present (a digest-bearing finding) and
				// EvidencePath carries the CI hop -- deployment_truth_tier
				// would report provenance_ci_declared for this row (see
				// rowHasCIDeclaredDeploymentEvidence,
				// supply_chain_impact_result.go), but the reducer's weak
				// repository+environment+operational-anchor branch (#5426)
				// never confirmed digest or image-ref identity, so it baked
				// neither CIDeclaredArtifactDigest nor CIDeclaredImageRef
				// (bakeSupplyChainCIDeclaredArtifactIdentity). Version
				// resolution requires a concrete claim, so it falls through
				// to the config floor instead of fabricating one from
				// SubjectDigest (#5469 review finding F1).
				SubjectDigest: digest,
				DeploymentIDs: []string{"deployment:example-api"},
				Environments:  []string{"prod"},
				EvidencePath:  []string{cicdRunCorrelationFactKind},
			},
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
			CIDeclaredArtifactDigest: digest,
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
		SubjectDigest:            digest,
		CIDeclaredArtifactDigest: digest,
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
		CIDeclaredArtifactDigest: digest,
		CIDeclaredImageRef:       "registry.example.com/demo/app:v1",
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
