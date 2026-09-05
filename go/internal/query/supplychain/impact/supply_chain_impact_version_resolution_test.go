// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/truth"
)

var supplyChainImpactFindingResultBenchmarkSink SupplyChainImpactFindingResult

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
	ecsARN := "arn:example:compute:::resource/bbbbbbbb"

	cases := []struct {
		name              string
		row               SupplyChainImpactFindingRow
		wantTier          truth.DeploymentTruthTier
		wantCorroboration []SupplyChainVersionResolutionCorroboration
	}{
		{
			name: "kubernetes runtime confirms the parent finding digest",
			row: SupplyChainImpactFindingRow{
				SubjectDigest: digest,
				KubernetesRuntimeWorkloadRefs: []KubernetesRuntimeWorkloadRef{{
					UID: "workload-1", ClusterID: "cluster-a", Namespace: "payments", Name: "api",
				}},
			},
			wantTier: truth.TierRuntimeConfirmed,
			wantCorroboration: []SupplyChainVersionResolutionCorroboration{{
				Tier:            string(truth.TierConfigOnly),
				DigestOrVersion: digest,
				EvidenceKind:    "config_materialization",
				Agreement:       supplyChainVersionResolutionAgrees,
			}},
		},
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
				ImageRef:     "registry.example.com/app:v1",
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
			gotTier, gotCorroboration := supplyChainVersionResolution(&tc.row)
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
	ecsARN := "arn:example:compute:::resource/cccccccc"

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
		tier, corroboration := supplyChainVersionResolution(&row)
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
// resolver is wired into BuildSupplyChainImpactFindingResult, the same
// pattern DeploymentTruthTier already uses (#5452).
func TestBuildSupplyChainImpactFindingResultSetsVersionResolution(t *testing.T) {
	t.Parallel()

	digest := "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	row := SupplyChainImpactFindingRow{
		SubjectDigest:            digest,
		CIDeclaredArtifactDigest: digest,
	}

	result := BuildSupplyChainImpactFindingResult(&row)
	if result.VersionResolutionTier != string(truth.TierProvenanceCIDeclared) {
		t.Fatalf("VersionResolutionTier = %q, want %q", result.VersionResolutionTier, truth.TierProvenanceCIDeclared)
	}
}

// TestBuildSupplyChainImpactFindingResultAllocationBudget keeps the #5469
// classifier within one allocation for a row that emits two corroboration
// entries. That one allocation is the returned corroboration slice itself;
// row classification and unchanged missing-evidence normalization must not
// add another per-finding allocation on this read path.
func TestBuildSupplyChainImpactFindingResultAllocationBudget(t *testing.T) {
	digest := "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	row := SupplyChainImpactFindingRow{
		FindingID:                "finding-allocation-budget",
		CVEID:                    "CVE-2026-00099",
		SubjectDigest:            digest,
		ImageRef:                 "registry.example.com/demo/app:v1",
		ObservedVersion:          "1.2.3",
		CloudRuntimeResourceRefs: []string{"arn:example:compute:::resource/allocation-budget"},
		CIDeclaredArtifactDigest: digest,
		CIDeclaredImageRef:       "registry.example.com/demo/app:v1",
		WorkloadIDs:              []string{"workload:example-api"},
		DeploymentIDs:            []string{"deployment:example-api"},
		ServiceIDs:               []string{"service:example-api"},
		Environments:             []string{"prod"},
		EvidencePath:             []string{cicdRunCorrelationFactKind, "reducer_platform_materialization"},
		MissingEvidence:          []string{ServiceCatalogCorrelationMissingReason},
	}

	allocations := testing.AllocsPerRun(1000, func() {
		_ = BuildSupplyChainImpactFindingResult(&row)
	})
	if allocations > 1 {
		t.Fatalf("allocations per result = %.0f, want <= 1", allocations)
	}
}

// BenchmarkBuildSupplyChainImpactFindingResult measures result-assembly cost
// per row for the #5469 performance proof: the resolver classifies fields the
// row already carries with no new graph or Postgres query. Measured on
// go1.26.5 darwin/arm64 (-benchtime=2s -count=5, five runs each), with both
// benchmarks assigning the result to a package sink: exact base
// ba2b7b80be85 was 143.3-144.5 ns/op, 16 B/op, 1 alloc/op; the finished
// #5469 path was 95.64-96.84 ns/op, 128 B/op, 1 alloc/op. CPU improves by
// about 30 percent and allocation count is maintained. The remaining 112
// B/op delta is the two corroboration records returned by the new wire
// contract, not temporary candidate or normalization storage.
//
// The exact-base companion benchmark uses the same common row fields,
// package sink, duration, and sample count. It omits the two #5469-only CI
// identity fields and calls the base value-parameter assembler; the pointer
// parameter is itself part of the measured optimization. The rest of this
// file is #5469-only and cannot compile on the base.
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
		CloudRuntimeResourceRefs: []string{"arn:example:compute:::resource/bench"},
		CIDeclaredArtifactDigest: digest,
		CIDeclaredImageRef:       "registry.example.com/demo/app:v1",
		WorkloadIDs:              []string{"workload:example-api"},
		DeploymentIDs:            []string{"deployment:example-api"},
		ServiceIDs:               []string{"service:example-api"},
		Environments:             []string{"prod"},
		EvidencePath:             []string{cicdRunCorrelationFactKind, "reducer_platform_materialization"},
		MissingEvidence:          []string{ServiceCatalogCorrelationMissingReason},
	}

	b.ReportAllocs()
	for b.Loop() {
		supplyChainImpactFindingResultBenchmarkSink = BuildSupplyChainImpactFindingResult(&row)
	}
}
