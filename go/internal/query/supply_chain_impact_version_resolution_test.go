// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/truth"
)

// TestSupplyChainVersionResolutionTier proves the #5469 tiered version
// resolution: the judged version/digest for a finding comes from the
// strongest available deployment-truth-tier evidence that is ELIGIBLE to
// win, weaker (or ineligible) present tiers are preserved as corroboration
// classified agrees/disagrees/not_comparable, declared_ref never fires
// absent an evidence producer, and a finding with no deployment evidence at
// all reports no tier.
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
//
// A CI-declared digest that CONTRADICTS the finding's own SubjectDigest is
// real evidence, but it is INELIGIBLE to win (review finding R1): crediting
// a foreign artifact's digest as the judged version/digest would put
// version_resolution_tier in direct conflict with config_only/
// runtime_confirmed, which still report the finding's own identity. And
// agreement is tri-state (review finding R6): a cross-axis comparison (a
// config-materialized version against a digest-based winner) is
// not_comparable, never a guaranteed-false "disagrees".
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
					Agreement:       supplyChainVersionResolutionAgrees,
				},
				{
					Tier:            string(truth.TierConfigOnly),
					DigestOrVersion: digest,
					EvidenceKind:    "config_materialization",
					Agreement:       supplyChainVersionResolutionAgrees,
				},
			},
		},
		{
			name: "runtime-observed digest disagrees with a contradicting CI-declared digest; runtime still wins since it never needed CI eligibility",
			row: SupplyChainImpactFindingRow{
				SubjectDigest:            digest,
				CloudRuntimeResourceRefs: []string{ecsARN},
				// The CI-declared deployment's OWN digest genuinely differs
				// from the runtime-observed one -- the reducer bakes this
				// only for a strong image-ref match whose artifact_digest
				// contradicted the finding's SubjectDigest
				// (bakeSupplyChainCIDeclaredArtifactIdentity), so this is a
				// real disagreement the evidence actually asserts, not a
				// fabricated one. CI is ineligible to win here (review
				// finding R1), but runtime_confirmed was always going to win
				// regardless -- see the next case for a row where
				// ineligibility actually changes the winner.
				CIDeclaredArtifactDigest: otherDigest,
			},
			wantTier: truth.TierRuntimeConfirmed,
			wantCorroboration: []SupplyChainVersionResolutionCorroboration{
				{
					Tier:            string(truth.TierProvenanceCIDeclared),
					DigestOrVersion: otherDigest,
					EvidenceKind:    "cicd_run_correlation",
					Agreement:       supplyChainVersionResolutionDisagrees,
				},
				{
					Tier:            string(truth.TierConfigOnly),
					DigestOrVersion: digest,
					EvidenceKind:    "config_materialization",
					Agreement:       supplyChainVersionResolutionAgrees,
				},
			},
		},
		{
			name: "a contradicting CI-declared digest with no runtime evidence is ineligible to win; config_only's own SubjectDigest wins instead (review finding R1's required case)",
			row: SupplyChainImpactFindingRow{
				SubjectDigest:            digest,
				CIDeclaredArtifactDigest: otherDigest,
			},
			wantTier: truth.TierConfigOnly,
			wantCorroboration: []SupplyChainVersionResolutionCorroboration{
				{
					Tier:            string(truth.TierProvenanceCIDeclared),
					DigestOrVersion: otherDigest,
					EvidenceKind:    "cicd_run_correlation",
					Agreement:       supplyChainVersionResolutionDisagrees,
				},
			},
		},
		{
			name: "CI-declared wins over a config-only version it cannot be compared against (cross-axis, not_comparable)",
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
					Agreement:       supplyChainVersionResolutionNotComparable,
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
			// Owner review (round 9): the config_only claim falls through
			// ObservedVersion -> SubjectDigest -> ImageRef
			// (supplyChainVersionResolutionClaim, ImageRef branch), but no
			// prior case in this table exercised a row carrying ONLY
			// ImageRef -- no ObservedVersion, no SubjectDigest. That third
			// fallback was untested even though config_only is the floor
			// every finding degrades to.
			name: "config-only falls back to image_ref when no version or digest exists",
			row: SupplyChainImpactFindingRow{
				ImageRef:     "registry.example/app:v1",
				WorkloadIDs:  []string{"workload:example-api"},
				Environments: []string{"prod"},
			},
			wantTier: truth.TierConfigOnly,
			// No other tier makes any claim for this row: runtime_confirmed
			// requires SubjectDigest (absent), provenance_ci_declared
			// requires CIDeclaredArtifactDigest/CIDeclaredImageRef (neither
			// baked here), and declared_ref never fires (#5393). config_only
			// is the only candidate, so there is nothing to corroborate
			// against.
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
// shape -- however many other tiers' evidence it carries, including a
// contradicting CI-declared digest -- may cause the resolver to report
// declared_ref as the winning tier or as a corroboration entry. Ownership of
// wiring real DEPLOYS_REF evidence belongs to #5393; this resolver must
// never invent it in the meantime.
func TestSupplyChainVersionResolutionDeclaredRefNeverEmitted(t *testing.T) {
	t.Parallel()

	digest := "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	otherDigest := "sha256:88888888888888888888888888888888888888888888888888888888888888"
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
		// A contradicting (ineligible) CI-declared digest, review finding R1.
		{SubjectDigest: digest, CIDeclaredArtifactDigest: otherDigest},
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
// row already carries with no new graph or Postgres query. Measured on
// go1.26.5 darwin/arm64 (-benchtime=2s -count=5, 5 runs each): before #5469
// (origin/main parent commit, this benchmark function copied alone into the
// package with CIDeclaredArtifactDigest/CIDeclaredImageRef deleted from the
// row literal, since those fields do not exist yet) 136.8-143.9 ns/op, 16
// B/op, 1 allocs/op; after #5469 (this commit) 292.3-314.3 ns/op, 208 B/op, 2
// allocs/op. That is roughly +166 ns/op (more than double, ~2.2x), 16->208
// B/op, 1->2 allocs/op per row -- bounded at 4 tiers, so it stays roughly
// constant per row rather than growing with corpus size, but it is not small
// in relative terms. At the 200-row page-size limit that is roughly 61
// microseconds and 42 KB of added per-page cost.
//
// This benchmark function alone compiles and runs unmodified at the parent
// commit with the two row fields above deleted. The rest of this file does
// not: it is entirely new in #5469, and its sibling tests in this file
// reference #5469-only symbols (SupplyChainVersionResolutionCorroboration,
// supplyChainVersionResolution, truth.TierProvenanceCIDeclared, and others),
// so copying the whole file to the parent commit fails to compile. Copy only
// this benchmark function into the package there.
//
// The row exercises every field the resolver reads, including enough tiers
// present at once to walk the full candidate/corroboration loop.
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
