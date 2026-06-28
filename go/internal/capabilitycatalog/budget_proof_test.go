// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capabilitycatalog

import "testing"

func testBudgetMatrix() Matrix {
	p95 := 800
	return Matrix{Capabilities: []MatrixCapability{{
		Capability: "code_search.exact_symbol",
		Tools:      []string{"find_code"},
		Profiles: map[string]MatrixProfile{
			"production": {
				Status:          "supported",
				MaxTruthLevel:   "exact",
				RequiredRuntime: "deployed_services",
				P95LatencyMS:    &p95,
				MaxScopeSize:    "multi_repo_platform",
			},
		},
	}}}
}

func validBudgetProofArtifact() BudgetProofArtifact {
	return BudgetProofArtifact{
		SchemaVersion: "capability-budget-proof/v1",
		Status:        "pass",
		Run: BudgetProofRun{
			Issue:  4062,
			Commit: "0123456789abcdef0123456789abcdef01234567",
			Backend: BudgetProofBackend{
				Kind:    "nornicdb",
				Version: "fixture-v1",
			},
		},
		Measurements: []BudgetMeasurement{{
			Capability:     "code_search.exact_symbol",
			Profile:        "production",
			MCPTools:       []string{"find_code"},
			CorpusSlot:     "medium/representative_20_50",
			ArtifactHandle: "capability-budget-code-search",
			Commit:         "0123456789abcdef0123456789abcdef01234567",
			Backend: BudgetProofBackend{
				Kind:    "nornicdb",
				Version: "fixture-v1",
			},
			Latency: BudgetLatency{
				P50MS: 120,
				P95MS: 700,
				P99MS: 760,
			},
			Scope: BudgetScopeProof{
				DeclaredMaxScopeSize: "multi_repo_platform",
				ResultScope:          "multi_repo_platform",
				LimitEnforced:        true,
				TruncationProof:      "limit-plus-one",
				TruncationInvariant:  "pass",
			},
			Freshness: BudgetFreshness{
				MeasuredAt: "2026-06-28T00:00:00Z",
				ExpiresAt:  "2026-07-28T00:00:00Z",
			},
			SurfaceParity: BudgetSurfaceParity{Status: "pass"},
			RetryCount:    0,
			DeadLetters:   0,
			Status:        "pass",
		}},
	}
}

func requireBudgetFinding(t *testing.T, findings []BudgetFinding, kind BudgetFindingKind) {
	t.Helper()
	for _, finding := range findings {
		if finding.Kind == kind {
			return
		}
	}
	t.Fatalf("missing budget finding %q in %+v", kind, findings)
}

func TestCheckBudgetProofPassesValidArtifact(t *testing.T) {
	t.Parallel()

	findings := CheckBudgetProof(testBudgetMatrix(), validBudgetProofArtifact())
	if len(findings) != 0 {
		t.Fatalf("unexpected findings: %+v", findings)
	}
}

func TestCheckBudgetProofFlagsMissingMeasurement(t *testing.T) {
	t.Parallel()

	artifact := validBudgetProofArtifact()
	artifact.Measurements = nil

	findings := CheckBudgetProof(testBudgetMatrix(), artifact)
	requireBudgetFinding(t, findings, BudgetFindingMissingMeasurement)
}

func TestCheckBudgetProofFlagsMissingLatencyPercentiles(t *testing.T) {
	t.Parallel()

	artifact := validBudgetProofArtifact()
	artifact.Measurements[0].Latency.P50MS = 0
	artifact.Measurements[0].Latency.P95MS = 0
	artifact.Measurements[0].Latency.P99MS = 0

	findings := CheckBudgetProof(testBudgetMatrix(), artifact)
	requireBudgetFinding(t, findings, BudgetFindingMissingMeasurement)
}

func TestCheckBudgetProofFlagsOverBudgetWithoutIssue(t *testing.T) {
	t.Parallel()

	artifact := validBudgetProofArtifact()
	artifact.Measurements[0].Latency.P95MS = 801

	findings := CheckBudgetProof(testBudgetMatrix(), artifact)
	requireBudgetFinding(t, findings, BudgetFindingP95OverBudget)
}

func TestCheckBudgetProofAllowsOverBudgetWithIssue(t *testing.T) {
	t.Parallel()

	artifact := validBudgetProofArtifact()
	artifact.Measurements[0].Latency.P95MS = 801
	artifact.Measurements[0].LinkedIssue = 3798

	findings := CheckBudgetProof(testBudgetMatrix(), artifact)
	if len(findings) != 0 {
		t.Fatalf("unexpected findings: %+v", findings)
	}
}

func TestCheckBudgetProofFlagsScopeAndTruncationGaps(t *testing.T) {
	t.Parallel()

	artifact := validBudgetProofArtifact()
	artifact.Measurements[0].Scope.LimitEnforced = false
	artifact.Measurements[0].Scope.TruncationProof = ""

	findings := CheckBudgetProof(testBudgetMatrix(), artifact)
	requireBudgetFinding(t, findings, BudgetFindingScopeNotProven)
}

func TestCheckBudgetProofFlagsSurfaceParityFailure(t *testing.T) {
	t.Parallel()

	artifact := validBudgetProofArtifact()
	artifact.Measurements[0].APIRoutes = []string{"GET /api/v0/search"}
	artifact.Measurements[0].SurfaceParity.Status = "fail"

	findings := CheckBudgetProof(testBudgetMatrix(), artifact)
	requireBudgetFinding(t, findings, BudgetFindingSurfaceParityFailed)
}

func TestCheckBudgetProofFlagsSurfaceParityDisagreement(t *testing.T) {
	t.Parallel()

	artifact := validBudgetProofArtifact()
	artifact.Measurements[0].APIRoutes = []string{"GET /api/v0/search"}
	artifact.Measurements[0].SurfaceParity = BudgetSurfaceParity{
		Status:      "pass",
		APIP95MS:    700,
		MCPP95MS:    760,
		MaxDeltaMS:  20,
		ProofHandle: "capability-budget-code-search-parity",
	}

	findings := CheckBudgetProof(testBudgetMatrix(), artifact)
	requireBudgetFinding(t, findings, BudgetFindingSurfaceParityFailed)
}

func TestCheckBudgetProofFlagsPassWithRetryOrDeadLetter(t *testing.T) {
	t.Parallel()

	artifact := validBudgetProofArtifact()
	artifact.Measurements[0].RetryCount = 1

	findings := CheckBudgetProof(testBudgetMatrix(), artifact)
	requireBudgetFinding(t, findings, BudgetFindingRuntimeInvariantFailed)
}

func TestCheckBudgetProofFlagsPassWithFailedMeasurementStatus(t *testing.T) {
	t.Parallel()

	for _, status := range []string{"", "fail", "partial"} {
		t.Run("status_"+status, func(t *testing.T) {
			t.Parallel()

			artifact := validBudgetProofArtifact()
			artifact.Measurements[0].Status = status
			artifact.Measurements[0].RetryCount = 1
			artifact.Measurements[0].DeadLetters = 1
			artifact.Measurements[0].Scope.TruncationInvariant = "fail"

			findings := CheckBudgetProof(testBudgetMatrix(), artifact)
			requireBudgetFinding(t, findings, BudgetFindingRuntimeInvariantFailed)
		})
	}
}

func TestCheckBudgetProofDoesNotTreatCommitDigitsAsPrivateData(t *testing.T) {
	t.Parallel()

	artifact := validBudgetProofArtifact()
	artifact.Run.Commit = "aaaaaaaaaa123456789012bbbbbbbbbbbbbbbbbb"
	artifact.Measurements[0].Commit = "aaaaaaaaaa123456789012bbbbbbbbbbbbbbbbbb"

	findings := CheckBudgetProof(testBudgetMatrix(), artifact)
	if len(findings) != 0 {
		t.Fatalf("unexpected findings: %+v", findings)
	}
}

func TestCheckBudgetProofFlagsPrivateData(t *testing.T) {
	t.Parallel()

	artifact := validBudgetProofArtifact()
	artifact.Measurements[0].ArtifactHandle = "https://private.example.invalid/raw"

	findings := CheckBudgetProof(testBudgetMatrix(), artifact)
	requireBudgetFinding(t, findings, BudgetFindingPrivateData)
}
