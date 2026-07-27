// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// testImpactOtherDigest and testImpactOtherImageRef are deployment-evidence
// identity values that deliberately do NOT match testImpactSubjectDigest or
// the SBOM-derived ImageRef, so a cicd_run_correlation fact built from them
// can only reach a finding through supplyChainDeploymentMatchesFinding's
// third (repository+environment+operational-anchor) branch, never through the
// digest or image-ref identity branches.
const (
	testImpactOtherDigest   = "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	testImpactOtherImageRef = "registry.example/api@sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
)

// cicdRunCorrelationImpactFactWithEvidence extends cicdRunCorrelationImpactFact
// (supply_chain_impact_runtime_test.go) with an explicit environment_evidence
// value, the #5425 corroboration signal #5426 reads. It never mutates the
// existing helper's signature so every pre-#5426 caller keeps compiling
// unchanged.
func cicdRunCorrelationImpactFactWithEvidence(
	factID string,
	artifactDigest string,
	imageRef string,
	repositoryID string,
	environment string,
	outcome string,
	environmentEvidence string,
) facts.Envelope {
	envelope := cicdRunCorrelationImpactFact(factID, artifactDigest, imageRef, repositoryID, environment, outcome)
	envelope.Payload["environment_evidence"] = environmentEvidence
	return envelope
}

// branch3TestFindingFacts builds the fixture common to the branch-3
// over-promotion tests: a finding that resolves SubjectDigest and ImageRef
// through SBOM/image evidence (so RuntimeReachability promotion is even
// possible), a workload identity anchor (the operational anchor branch 3
// requires), and a deployment fact whose own digest/image-ref deliberately
// disagree with the finding's -- so the deployment can only be considered a
// match via branch 3 (repository + environment + operational anchor), never
// via digest or image-ref identity equality.
func branch3TestFindingFacts(cveID string, deployment facts.Envelope) []facts.Envelope {
	return []facts.Envelope{
		vulnerabilityCVEFact("cve-1", cveID, 9.1),
		vulnerabilityAffectedPackageFact("affected-1", cveID, testImpactPackageID, "npm", "example", "1.2.3", "1.3.0"),
		packageConsumptionFactWithChain("consume-1", testImpactPackageID, testImpactRepositoryID, "1.2.3", []string{"api", "example"}, 2, false),
		sbomComponentImpactFact("component-1", "doc-1", testImpactPURL),
		sbomAttachmentImpactFact("attachment-1", "doc-1", testImpactSubjectDigest),
		containerImageIdentityImpactFactWithOutcome(
			"image-1",
			testImpactSubjectDigest,
			testImpactRepositoryID,
			"registry.example/api@"+testImpactSubjectDigest,
			string(ContainerImageIdentityExactDigest),
		),
		workloadIdentityImpactFact("workload-1", testImpactRepositoryID, testImpactWorkloadID),
		deployment,
	}
}

// Test 1: supplyChainDeploymentContextFromEnvelope decodes environment_evidence,
// mapping an absent key to "declared" and preserving the "deploy_event" value
// #5425 publishes. An unrecognized value also maps to "declared": a producer
// disagreement about the vocabulary must never be read as corroboration.
func TestSupplyChainDeploymentContextFromEnvelopeDecodesEnvironmentEvidence(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  any
		want string
	}{
		{name: "absent", raw: nil, want: supplyChainEnvironmentEvidenceDeclared},
		{name: "deploy_event", raw: "deploy_event", want: supplyChainEnvironmentEvidenceDeployEvent},
		{name: "declared", raw: "declared", want: supplyChainEnvironmentEvidenceDeclared},
		{name: "unrecognized", raw: "something_else", want: supplyChainEnvironmentEvidenceDeclared},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			payload := map[string]any{
				"repository_id": testImpactRepositoryID,
				"environment":   testImpactEnv,
			}
			if tc.raw != nil {
				payload["environment_evidence"] = tc.raw
			}
			got := supplyChainDeploymentContextFromEnvelope(facts.Envelope{
				FactID:   "deploy-1",
				FactKind: cicdRunCorrelationFactKind,
				Payload:  payload,
			})
			if got.environmentEvidence != tc.want {
				t.Fatalf("environmentEvidence = %q, want %q", got.environmentEvidence, tc.want)
			}
		})
	}
}

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

// Test 6: the persisted typed payload carries per-environment evidence state,
// including an absent producer value mapping to "declared" -- exercised
// end-to-end through BuildSupplyChainImpactFindings and
// supplyChainImpactTypedPayload rather than by constructing the map by hand,
// so the test proves the whole decode -> record -> persist path, not just one
// link.
func TestSupplyChainImpactTypedPayloadPersistsEnvironmentEvidence(t *testing.T) {
	t.Parallel()

	deployment := cicdRunCorrelationImpactFact(
		"deploy-1",
		testImpactSubjectDigest,
		"registry.example/api@"+testImpactSubjectDigest,
		testImpactRepositoryID,
		testImpactEnv,
		string(CICDRunCorrelationExact),
	)
	findings := BuildSupplyChainImpactFindings(branch3TestFindingFacts("CVE-2026-5430", deployment))
	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-5430"]

	payload := supplyChainImpactTypedPayload(SupplyChainImpactWrite{
		ScopeID:      "scope-1",
		GenerationID: "generation-1",
	}, got)
	if payload.EnvironmentEvidence == nil {
		t.Fatal("EnvironmentEvidence = nil, want the recorded evidence map")
	}
	if got, want := payload.EnvironmentEvidence[testImpactEnv], supplyChainEnvironmentEvidenceDeclared; got != want {
		t.Fatalf("payload.EnvironmentEvidence[%q] = %q, want %q for a deployment fact predating #5425", testImpactEnv, got, want)
	}
}

// Test 7: recordSupplyChainEnvironmentEvidence never downgrades deploy_event
// back to declared for the same environment, regardless of which evidence
// state arrives first.
func TestRecordSupplyChainEnvironmentEvidenceDeployEventWinsOverDeclared(t *testing.T) {
	t.Parallel()

	t.Run("deploy_event_then_declared", func(t *testing.T) {
		t.Parallel()
		state := recordSupplyChainEnvironmentEvidence(nil, testImpactEnv, supplyChainEnvironmentEvidenceDeployEvent)
		state = recordSupplyChainEnvironmentEvidence(state, testImpactEnv, supplyChainEnvironmentEvidenceDeclared)
		if got, want := state[testImpactEnv], supplyChainEnvironmentEvidenceDeployEvent; got != want {
			t.Fatalf("state[%q] = %q, want %q (deploy_event must not be downgraded)", testImpactEnv, got, want)
		}
	})

	t.Run("declared_then_deploy_event", func(t *testing.T) {
		t.Parallel()
		state := recordSupplyChainEnvironmentEvidence(nil, testImpactEnv, supplyChainEnvironmentEvidenceDeclared)
		state = recordSupplyChainEnvironmentEvidence(state, testImpactEnv, supplyChainEnvironmentEvidenceDeployEvent)
		if got, want := state[testImpactEnv], supplyChainEnvironmentEvidenceDeployEvent; got != want {
			t.Fatalf("state[%q] = %q, want %q (deploy_event must win on arrival)", testImpactEnv, got, want)
		}
	})
}
