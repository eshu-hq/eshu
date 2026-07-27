// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Promotion behaviour for RuntimeReachability="deployed_image" (#5426). The
// evidence vocabulary itself -- normalize, record, and the envelope decode --
// lives in supply_chain_impact_environment_evidence_test.go, which also owns
// the shared fixtures these tests use.

// Test 2: a branch-3-only deployment (repository + environment match, no
// digest/image-ref agreement) whose environment_evidence is declared-only must
// not promote RuntimeReachability to deployed_image -- this is the
// over-promotion #5426 exists to close: a finding with a digest can otherwise
// reach deployed_image through a deployment that never references that
// digest.
//
// It must nonetheless still MATCH. The gate belongs on the promotion, not on
// the join: a match also carries the environment, the cicd_run_correlation
// evidence hop, and the correlation fact ID, and dropping the join would
// discard all three. The evidence hop in particular is what
// rowHasCIDeclaredDeploymentEvidence reads to hold the row at
// deployment_truth_tier=provenance_ci_declared, so refusing the match would
// silently downgrade it to config_only -- a regression for findings that never
// had the over-promotion problem at all.
func TestBuildSupplyChainImpactFindingsBranch3DeclaredOnlyDoesNotPromoteRuntimeReachability(t *testing.T) {
	t.Parallel()

	// No artifact identity at all: branches 1 and 2 are unsatisfiable and the
	// contradicting-digest check cannot fire, so the ONLY thing that can decide
	// promotion here is the declared-vs-deploy_event rule this test exists for.
	// A fixture with a contradicting digest would still pass while proving
	// nothing, because the digest check short-circuits ahead of it.
	deployment := cicdRunCorrelationImpactFactWithEvidence(
		"deploy-1",
		"",
		"",
		testImpactRepositoryID,
		testImpactEnv,
		string(CICDRunCorrelationExact),
		supplyChainEnvironmentEvidenceDeclared,
	)
	findings := BuildSupplyChainImpactFindings(branch3TestFindingFacts("CVE-2026-5426", deployment))

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-5426"]
	if got.RuntimeReachability == "deployed_image" {
		t.Fatalf("RuntimeReachability = %q, want declared-only branch-3 evidence to withhold deployed_image", got.RuntimeReachability)
	}
	// The environment survives, labelled declared rather than dropped, so a
	// consumer can tell "declared-only" from "no deployment evidence at all".
	assertContainsString(t, got.Environments, testImpactEnv)
	if got.EnvironmentEvidence[testImpactEnv] != supplyChainEnvironmentEvidenceDeclared {
		t.Fatalf(
			"EnvironmentEvidence[%q] = %q, want %q",
			testImpactEnv, got.EnvironmentEvidence[testImpactEnv], supplyChainEnvironmentEvidenceDeclared,
		)
	}
	// The evidence hop survives, so deployment_truth_tier stays
	// provenance_ci_declared instead of silently falling to config_only.
	assertContainsString(t, got.EvidencePath, cicdRunCorrelationFactKind)
}

// Test 3: the same branch-3-only deployment, now stamped
// environment_evidence=deploy_event, yields deployed_image and records the
// environment plus its evidence state.
//
// The correlation row carries NO artifact digest here, which is the case
// deploy_event is meant to rescue: the deployment asserts an environment
// without asserting a contradicting artifact identity. Test 3b covers the case
// where it does assert one.
func TestBuildSupplyChainImpactFindingsBranch3DeployEventPromotesRuntimeReachability(t *testing.T) {
	t.Parallel()

	deployment := cicdRunCorrelationImpactFactWithEvidence(
		"deploy-1",
		"",
		"",
		testImpactRepositoryID,
		testImpactEnv,
		string(CICDRunCorrelationExact),
		supplyChainEnvironmentEvidenceDeployEvent,
	)
	findings := BuildSupplyChainImpactFindings(branch3TestFindingFacts("CVE-2026-5427", deployment))

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-5427"]
	if got.RuntimeReachability != "deployed_image" {
		t.Fatalf("RuntimeReachability = %q, want deployed_image when branch-3 evidence is deploy_event", got.RuntimeReachability)
	}
	assertContainsString(t, got.Environments, testImpactEnv)
	if got.EnvironmentEvidence[testImpactEnv] != supplyChainEnvironmentEvidenceDeployEvent {
		t.Fatalf("EnvironmentEvidence[%q] = %q, want %q", testImpactEnv, got.EnvironmentEvidence[testImpactEnv], supplyChainEnvironmentEvidenceDeployEvent)
	}
}

// Test 3b: a deploy_event branch-3 deployment that names a digest CONTRADICTING
// the finding's own subject must not promote.
//
// environment_evidence corroborates the environment, not the artifact. A
// correlation row carries artifact_digest and environment_evidence together, so
// "a deployment to prod happened" and "the thing deployed was a different
// image" can both be true on one row. That is positive evidence the vulnerable
// artifact did not ship, which must outrank the environment corroboration --
// otherwise deploy_event becomes a blanket override that promotes findings
// about images the deployment explicitly says were not deployed.
//
// The environment is still recorded: the deployment matched, so it keeps
// contributing its environment and evidence hop. Only promotion is withheld.
func TestBuildSupplyChainImpactFindingsBranch3DeployEventWithContradictingDigestDoesNotPromote(t *testing.T) {
	t.Parallel()

	deployment := cicdRunCorrelationImpactFactWithEvidence(
		"deploy-1",
		testImpactOtherDigest,
		testImpactOtherImageRef,
		testImpactRepositoryID,
		testImpactEnv,
		string(CICDRunCorrelationExact),
		supplyChainEnvironmentEvidenceDeployEvent,
	)
	findings := BuildSupplyChainImpactFindings(branch3TestFindingFacts("CVE-2026-5432", deployment))

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-5432"]
	if got.RuntimeReachability == "deployed_image" {
		t.Fatalf(
			"RuntimeReachability = %q, want deploy_event corroboration to be overridden by a deployment naming a different digest",
			got.RuntimeReachability,
		)
	}
	assertContainsString(t, got.Environments, testImpactEnv)
	assertContainsString(t, got.EvidencePath, cicdRunCorrelationFactKind)
}

// Test 4 (regression guard): a digest-branch match must keep promoting
// RuntimeReachability to deployed_image even when the matching deployment's
// own environment evidence is declared-only. #5426 corrects the premise that
// every deployment match needs deploy_event corroboration -- only the
// free-text-environment branch (3) does; the digest identity branch is
// artifact-anchored and MUST NOT be tightened.
func TestBuildSupplyChainImpactFindingsDigestBranchPromotesRegardlessOfEnvironmentEvidence(t *testing.T) {
	t.Parallel()

	deployment := cicdRunCorrelationImpactFactWithEvidence(
		"deploy-1",
		testImpactSubjectDigest,
		testImpactOtherImageRef,
		testImpactRepositoryID,
		testImpactEnv,
		string(CICDRunCorrelationExact),
		supplyChainEnvironmentEvidenceDeclared,
	)
	findings := BuildSupplyChainImpactFindings(branch3TestFindingFacts("CVE-2026-5428", deployment))

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-5428"]
	if got.RuntimeReachability != "deployed_image" {
		t.Fatalf("RuntimeReachability = %q, want digest-branch match to still promote deployed_image with declared-only environment evidence", got.RuntimeReachability)
	}
	if got.EnvironmentEvidence[testImpactEnv] != supplyChainEnvironmentEvidenceDeclared {
		t.Fatalf("EnvironmentEvidence[%q] = %q, want %q recorded even though the match came from the digest branch", testImpactEnv, got.EnvironmentEvidence[testImpactEnv], supplyChainEnvironmentEvidenceDeclared)
	}
}

// Test 5 (regression guard): same as test 4 but for the image-ref identity
// branch. The deployment carries no artifact digest, so the image-ref branch is
// the only thing that can promote it -- test 5b covers what happens when the
// row also names a digest that contradicts the finding.
func TestBuildSupplyChainImpactFindingsImageRefBranchPromotesRegardlessOfEnvironmentEvidence(t *testing.T) {
	t.Parallel()

	imageRef := "registry.example/api@" + testImpactSubjectDigest
	deployment := cicdRunCorrelationImpactFactWithEvidence(
		"deploy-1",
		"",
		imageRef,
		testImpactRepositoryID,
		testImpactEnv,
		string(CICDRunCorrelationExact),
		supplyChainEnvironmentEvidenceDeclared,
	)
	findings := BuildSupplyChainImpactFindings(branch3TestFindingFacts("CVE-2026-5429", deployment))

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-5429"]
	if got.RuntimeReachability != "deployed_image" {
		t.Fatalf("RuntimeReachability = %q, want image-ref-branch match to still promote deployed_image with declared-only environment evidence", got.RuntimeReachability)
	}
	if got.EnvironmentEvidence[testImpactEnv] != supplyChainEnvironmentEvidenceDeclared {
		t.Fatalf("EnvironmentEvidence[%q] = %q, want %q recorded even though the match came from the image-ref branch", testImpactEnv, got.EnvironmentEvidence[testImpactEnv], supplyChainEnvironmentEvidenceDeclared)
	}
}

// Test 5b: a matching image reference does NOT rescue a deployment whose digest
// contradicts the finding.
//
// An image reference is a mutable, registry-prefixed tag: the same
// registry/app:v1 can be retagged from digest A to digest B. So a row whose ref
// matches while its digest names a different image is reporting that the tag has
// moved, and the digest is the identity worth believing. The contradicting-digest
// check therefore runs ahead of the image-ref branch, not after it.
func TestBuildSupplyChainImpactFindingsImageRefMatchWithContradictingDigestDoesNotPromote(t *testing.T) {
	t.Parallel()

	imageRef := "registry.example/api@" + testImpactSubjectDigest
	deployment := cicdRunCorrelationImpactFactWithEvidence(
		"deploy-1",
		testImpactOtherDigest,
		imageRef,
		testImpactRepositoryID,
		testImpactEnv,
		string(CICDRunCorrelationExact),
		supplyChainEnvironmentEvidenceDeployEvent,
	)
	findings := BuildSupplyChainImpactFindings(branch3TestFindingFacts("CVE-2026-5433", deployment))

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-5433"]
	if got.RuntimeReachability == "deployed_image" {
		t.Fatalf(
			"RuntimeReachability = %q, want a contradicting digest to outrank a matching mutable image ref",
			got.RuntimeReachability,
		)
	}
	assertContainsString(t, got.EvidencePath, cicdRunCorrelationFactKind)
}

// Test 12: when two matched deployments report the SAME environment with
// different evidence, deploy_event wins regardless of fact arrival order.
//
// This is ordinary production data -- one repository, several prod CI runs,
// only some with an observed deployment event -- and both rows match branch 3.
// Test 7 covers recordSupplyChainEnvironmentEvidence directly, but a helper
// test does not prove the production path calls it: replacing the call in
// applySupplyChainRuntimeContext with naive last-write-wins passes every
// package. Without the rule, the persisted environment_evidence[prod] would
// flap with fact load order across re-projections.
func TestBuildSupplyChainImpactFindingsDeployEventWinsAcrossDeploymentsInEitherOrder(t *testing.T) {
	t.Parallel()

	declared := func(id string) facts.Envelope {
		return cicdRunCorrelationImpactFactWithEvidence(
			id, "", "", testImpactRepositoryID, testImpactEnv,
			string(CICDRunCorrelationExact), supplyChainEnvironmentEvidenceDeclared,
		)
	}
	deployEvent := func(id string) facts.Envelope {
		return cicdRunCorrelationImpactFactWithEvidence(
			id, "", "", testImpactRepositoryID, testImpactEnv,
			string(CICDRunCorrelationExact), supplyChainEnvironmentEvidenceDeployEvent,
		)
	}

	for _, tc := range []struct {
		name  string
		cveID string
		facts []facts.Envelope
	}{
		{
			name:  "declared first",
			cveID: "CVE-2026-5434",
			facts: []facts.Envelope{declared("deploy-1"), deployEvent("deploy-2")},
		},
		{
			name:  "deploy_event first",
			cveID: "CVE-2026-5435",
			facts: []facts.Envelope{deployEvent("deploy-1"), declared("deploy-2")},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			findings := BuildSupplyChainImpactFindings(
				branch3TestFindingFactsWithDeployments(tc.cveID, tc.facts...),
			)
			got := supplyChainImpactFindingsByCVE(findings)[tc.cveID]
			assertContainsString(t, got.Environments, testImpactEnv)
			// Promotion is ANY-of, not ALL-of: one qualifying deployment
			// carries the finding even though its sibling is declared-only.
			// Without this assertion an ANY->ALL inversion passes silently and
			// findings quietly lose deployed_image.
			if got.RuntimeReachability != "deployed_image" {
				t.Fatalf(
					"RuntimeReachability = %q, want deployed_image -- one qualifying deployment is enough, even alongside a declared-only sibling",
					got.RuntimeReachability,
				)
			}
			if got.EnvironmentEvidence[testImpactEnv] != supplyChainEnvironmentEvidenceDeployEvent {
				t.Fatalf(
					"EnvironmentEvidence[%q] = %q, want %q -- deploy_event must not be downgraded by a sibling declared-only deployment",
					testImpactEnv,
					got.EnvironmentEvidence[testImpactEnv],
					supplyChainEnvironmentEvidenceDeployEvent,
				)
			}
		})
	}
}

// A finding with no artifact identity must not reach deployed_image, even from
// a deploy_event-corroborated deployment. supplyChainDeploymentPromotesRuntimeReachability
// returns true for that input by design -- its PRECONDITION is that the caller
// has already established a non-empty SubjectDigest -- so this pins the caller
// guard rather than the predicate. Without it, deleting that guard leaves the
// package green while digest-less findings claim to be deployed.
func TestBuildSupplyChainImpactFindingsWithoutSubjectDigestDoesNotPromote(t *testing.T) {
	t.Parallel()

	deployment := cicdRunCorrelationImpactFactWithEvidence(
		"deploy-1", "", "", testImpactRepositoryID, testImpactEnv,
		string(CICDRunCorrelationExact), supplyChainEnvironmentEvidenceDeployEvent,
	)
	base := branch3TestFindingFacts("CVE-2026-5436", deployment)
	// Drop the SBOM attachment and image identity, the two facts that give the
	// finding a subject digest.
	var kept []facts.Envelope
	for _, envelope := range base {
		if envelope.FactID == "attachment-1" || envelope.FactID == "image-1" {
			continue
		}
		kept = append(kept, envelope)
	}

	findings := BuildSupplyChainImpactFindings(kept)
	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-5436"]
	if got.SubjectDigest != "" {
		t.Fatalf("SubjectDigest = %q, want empty -- fixture no longer exercises the digest-less case", got.SubjectDigest)
	}
	if got.RuntimeReachability == "deployed_image" {
		t.Fatalf("RuntimeReachability = %q, want a finding with no artifact identity to stay unpromoted", got.RuntimeReachability)
	}
}
