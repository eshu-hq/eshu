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

// branch3TestFindingFactsWithDeployments is branch3TestFindingFacts for the
// multi-deployment case, where several correlations report the SAME environment
// with different evidence states.
func branch3TestFindingFactsWithDeployments(cveID string, deployments ...facts.Envelope) []facts.Envelope {
	base := branch3TestFindingFacts(cveID, deployments[0])
	return append(base, deployments[1:]...)
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
		// Padding is already stripped by payloadStr before normalize sees it,
		// so this case pins the envelope path end to end rather than
		// normalize's own trim -- that one is pinned directly in
		// TestNormalizeSupplyChainEnvironmentEvidenceTrimsPadding.
		{name: "padded deploy_event", raw: "  deploy_event  ", want: supplyChainEnvironmentEvidenceDeployEvent},
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

// Test 7b: a blank or whitespace-only environment records nothing.
//
// Reachable in production: a run correlation carries Environment == "" when it
// has no environment evidence, and the digest and image-ref match branches
// impose no environment condition -- so an environment-less correlation reaches
// the recorder. Without the skip the map gains a "" key, while
// uniqueSortedStrings drops blanks from Environments, leaving the two
// structures disagreeing about their own keys.
func TestRecordSupplyChainEnvironmentEvidenceSkipsBlankEnvironment(t *testing.T) {
	t.Parallel()

	for _, environment := range []string{"", "   ", "\t"} {
		state := recordSupplyChainEnvironmentEvidence(
			nil, environment, supplyChainEnvironmentEvidenceDeployEvent,
		)
		if len(state) != 0 {
			t.Fatalf("recordSupplyChainEnvironmentEvidence(%q) = %#v, want no entry recorded", environment, state)
		}
	}

	existing := map[string]string{testImpactEnv: supplyChainEnvironmentEvidenceDeclared}
	if got := recordSupplyChainEnvironmentEvidence(existing, " ", supplyChainEnvironmentEvidenceDeployEvent); len(got) != 1 {
		t.Fatalf("recordSupplyChainEnvironmentEvidence(blank) = %#v, want the existing state untouched", got)
	}
}

// normalize does its own trimming even though payloadStr already trims on the
// only production path that reaches it. Called directly, because going through
// the envelope cannot distinguish a trimming normalize from a non-trimming one:
// the value arrives pre-trimmed either way.
func TestNormalizeSupplyChainEnvironmentEvidenceTrimsPadding(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"  deploy_event", "deploy_event  ", "\tdeploy_event\n"} {
		if got := normalizeSupplyChainEnvironmentEvidence(raw); got != supplyChainEnvironmentEvidenceDeployEvent {
			t.Fatalf("normalizeSupplyChainEnvironmentEvidence(%q) = %q, want %q", raw, got, supplyChainEnvironmentEvidenceDeployEvent)
		}
	}
	if got := normalizeSupplyChainEnvironmentEvidence("  deploy event  "); got != supplyChainEnvironmentEvidenceDeclared {
		t.Fatalf("normalizeSupplyChainEnvironmentEvidence(near-miss) = %q, want %q", got, supplyChainEnvironmentEvidenceDeclared)
	}
}
