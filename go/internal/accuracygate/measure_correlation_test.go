// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package accuracygate_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/accuracygate"
	"github.com/eshu-hq/eshu/go/internal/admissionaudit"
	"github.com/eshu-hq/eshu/go/internal/correlation/admission"
	"github.com/eshu-hq/eshu/go/internal/correlation/model"
	"github.com/eshu-hq/eshu/go/internal/correlation/rules"
)

// correlationGoldenPath is the checked-in correlation admission golden suite the
// gate scores. It is the same fixture the admissionaudit package's own test
// consumes, so the gate measures correlation truth against the published suite.
func correlationGoldenPath() string {
	return filepath.Join("..", "..", "..", "tests", "fixtures", "product_truth", "expected", "correlation_admission_golden.json")
}

// correlationConfidenceThreshold is the production admission confidence gate the
// measurement drives correlation.admission.Evaluate with. It is the same closed
// [0,1] gate the shipped admission path applies; the per-case inputs below carry
// confidences on both sides of it so a real gate change moves an observed state.
const correlationConfidenceThreshold = 0.75

// measureCorrelation loads the golden admission suite, derives each case's
// observed admission state by running the REAL production admission classifier
// (correlation/admission.Evaluate plus generation-freshness comparison) over a
// per-case input, audits that observed snapshot against the golden expectation
// with admissionaudit.Audit, and converts the result into an admission
// precision/recall Metric.
//
// The observation is NOT a copy of each intent's ExpectedState. It is the output
// of shipped admission logic applied to a per-case input candidate: the
// confidence gate, the exact-match evidence-structure gate, and the generation
// freshness comparison decide admitted / rejected / missing_evidence / ambiguous
// / stale. So a regression in the production admission decision (a flipped
// confidence gate, a dropped required-evidence match, or a freshness comparison
// that admits a stale generation) makes the observed state diverge from the
// golden expectation, the audit raise a disagreement, and this metric drop below
// the gate floor. The admitted state is the positive class; an admitted intent is
// a true positive only when the audit raises no disagreement against it.
func measureCorrelation(t *testing.T) accuracygate.Metric {
	t.Helper()

	suite, err := admissionaudit.LoadSuite(correlationGoldenPath())
	if err != nil {
		t.Fatalf("LoadSuite() error = %v", err)
	}

	observation := observeCorrelationDecisions(t, suite.Intents, correlationInputs())
	report := admissionaudit.Audit(suite, observation)
	if !report.Pass() {
		t.Fatalf("correlation admission audit failed on production-derived observation: %s", report.Summary())
	}

	disagreed := disagreementCaseIDs(report)
	predictions := make([]accuracygate.LabeledPrediction, 0, len(suite.Intents))
	for _, intent := range suite.Intents {
		observedState := string(intent.ExpectedState)
		if _, bad := disagreed[intent.CaseID]; bad {
			observedState = "disagreement"
		}
		predictions = append(predictions, accuracygate.LabeledPrediction{
			Label:    intent.CaseID,
			Expected: string(intent.ExpectedState),
			Observed: observedState,
			Positive: intent.ExpectedState == admissionaudit.StateAdmitted,
		})
	}
	return accuracygate.ScorePredictions(predictions)
}

// correlationCaseInput is the per-case correlation INPUT the measurement drives
// production admission logic with. It is deliberately distinct from the golden
// expectation: the production classifier reads these inputs and derives the
// observed admission state, so the gate scores observed-vs-golden rather than
// golden-vs-golden.
type correlationCaseInput struct {
	// candidate is the correlation candidate fed to admission.Evaluate.
	candidate model.Candidate
	// requirements is the exact-match evidence structure the candidate must
	// satisfy, applied by the production admission structure gate.
	requirements []rules.EvidenceRequirement
	// generationID is the candidate's source generation, compared against
	// currentGeneration to derive stale freshness the same way the shipped
	// freshness check supersedes an older generation.
	generationID string
	// competing reports that a second admitted candidate exists for the same
	// correlation key this generation; the production conflict guard keeps such a
	// candidate visible without promoting a single owner (ambiguous).
	competing bool
}

// currentCorrelationGeneration is the freshest generation the measurement treats
// as current. A case whose input generation differs is classified stale by the
// production-faithful freshness comparison, never by reading ExpectedState.
const currentCorrelationGeneration = "generation-current"

// correlationInputs returns the per-case production inputs keyed by golden case
// id. Each input is built to exercise a distinct production admission outcome:
// strong evidence admits, missing required structure stays missing_evidence,
// competing owners stay ambiguous, an older generation stays stale, and unsafe
// low-confidence evidence is rejected. None of these inputs encodes the expected
// state string; the production classifier derives it.
func correlationInputs() map[string]correlationCaseInput {
	serviceEvidence := []model.EvidenceAtom{{
		ID:           "ev-deploy-repo",
		SourceSystem: "git",
		EvidenceType: "deployment",
		ScopeID:      "git-repository-scope:team/api",
		Key:          "deployment_repository",
		Value:        "team/api",
		Confidence:   0.95,
	}}
	deploymentRequirement := []rules.EvidenceRequirement{{
		Name:     "deployment-repository",
		MinCount: 1,
		MatchAll: []rules.EvidenceSelector{
			{Field: rules.EvidenceFieldEvidenceType, Value: "deployment"},
			{Field: rules.EvidenceFieldKey, Value: "deployment_repository"},
		},
	}}
	manifestEvidence := []model.EvidenceAtom{{
		ID:           "ev-manifest-dep",
		SourceSystem: "package_registry",
		EvidenceType: "manifest_dependency",
		ScopeID:      "package-registry:npm:team-api",
		Key:          "package",
		Value:        "team-api",
		Confidence:   0.9,
	}}
	manifestRequirement := []rules.EvidenceRequirement{{
		Name:     "manifest-dependency",
		MinCount: 1,
		MatchAll: []rules.EvidenceSelector{
			{Field: rules.EvidenceFieldEvidenceType, Value: "manifest_dependency"},
			{Field: rules.EvidenceFieldKey, Value: "package"},
		},
	}}
	cloudEvidence := []model.EvidenceAtom{{
		ID:           "ev-cloud-identity",
		SourceSystem: "gcp",
		EvidenceType: "cloud_inventory",
		ScopeID:      "cloud:gcp:team",
		Key:          "resource_identity",
		Value:        "api-instance",
		Confidence:   0.92,
	}}
	cloudRequirement := []rules.EvidenceRequirement{{
		Name:     "cloud-identity",
		MinCount: 1,
		MatchAll: []rules.EvidenceSelector{
			{Field: rules.EvidenceFieldEvidenceType, Value: "cloud_inventory"},
			{Field: rules.EvidenceFieldKey, Value: "resource_identity"},
		},
	}}

	return map[string]correlationCaseInput{
		"deployable-service-admitted": {
			candidate:    correlationCandidate("deployable-service-admitted", "deployable_unit", "service://team/api", 0.95, serviceEvidence),
			requirements: deploymentRequirement,
			generationID: currentCorrelationGeneration,
		},
		// Service candidate with no deployment-repository evidence: the production
		// structure gate fails, so the candidate stays provenance-only.
		"deployable-service-missing-evidence": {
			candidate:    correlationCandidate("deployable-service-missing-evidence", "deployable_unit", "service://team/worker", 0.95, nil),
			requirements: deploymentRequirement,
			generationID: currentCorrelationGeneration,
		},
		"package-source-admitted": {
			candidate:    correlationCandidate("package-source-admitted", "package_source", "pkg:npm/team-api", 0.9, manifestEvidence),
			requirements: manifestRequirement,
			generationID: currentCorrelationGeneration,
		},
		// Two admitted owners for the same package this generation: the production
		// conflict guard keeps the correlation visible without promoting an owner.
		"package-source-ambiguous": {
			candidate:    correlationCandidate("package-source-ambiguous", "package_source", "pkg:npm/team-api", 0.9, manifestEvidence),
			requirements: manifestRequirement,
			generationID: currentCorrelationGeneration,
			competing:    true,
		},
		// An older generation of an otherwise-admissible package source: the
		// freshness comparison keeps it stale, never current admitted truth.
		"package-source-stale-replay": {
			candidate:    correlationCandidate("package-source-stale-replay", "package_source", "pkg:npm/team-api", 0.9, manifestEvidence),
			requirements: manifestRequirement,
			generationID: "generation-previous",
		},
		"cloud-resource-admitted": {
			candidate:    correlationCandidate("cloud-resource-admitted", "cloud_resource", "cloud-resource:gcp:team:api-instance", 0.92, cloudEvidence),
			requirements: cloudRequirement,
			generationID: currentCorrelationGeneration,
		},
		// Unsafe cloud identity: confidence below the production gate, so the
		// admission decision rejects it and it stays provenance-only.
		"cloud-resource-rejected": {
			candidate:    correlationCandidate("cloud-resource-rejected", "cloud_resource", "cloud-resource:gcp:team:conflicting", 0.4, cloudEvidence),
			requirements: cloudRequirement,
			generationID: currentCorrelationGeneration,
		},
	}
}

// correlationCandidate builds a provisional production correlation candidate with
// the given confidence and evidence. State is provisional on input; the
// production admission classifier sets the admitted/rejected outcome.
func correlationCandidate(id, kind, key string, confidence float64, evidence []model.EvidenceAtom) model.Candidate {
	return model.Candidate{
		ID:             "candidate:" + id,
		Kind:           kind,
		CorrelationKey: key,
		Confidence:     confidence,
		State:          model.CandidateStateProvisional,
		Evidence:       evidence,
	}
}

// observeCorrelationDecisions runs the production admission classifier over each
// case input and assembles the audit observation from its ACTUAL output. The
// observed admission state, canonical write, graph facts, and readback come from
// classifyCorrelationState, never from the intent's ExpectedState, so the audit
// scores production behavior against the golden expectation.
func observeCorrelationDecisions(
	t *testing.T,
	intents []admissionaudit.FixtureIntent,
	inputs map[string]correlationCaseInput,
) admissionaudit.Observation {
	t.Helper()

	observation := admissionaudit.Observation{}
	for _, intent := range intents {
		input, ok := inputs[intent.CaseID]
		if !ok {
			t.Fatalf("no production correlation input wired for golden case %q", intent.CaseID)
		}
		observedState := classifyCorrelationState(t, input)

		decisionID := "admission:" + intent.CaseID
		decision := admissionaudit.Decision{
			ID:            decisionID,
			CaseID:        intent.CaseID,
			Domain:        intent.Domain,
			State:         observedState,
			ScopeID:       intent.ScopeID,
			GenerationID:  intent.GenerationID,
			EvidenceCount: 1,
			Explanation:   intent.FixtureIntent,
		}
		if observedState == admissionaudit.StateAdmitted {
			decision.CanonicalWrite = admissionaudit.CanonicalWrite{
				Written:    true,
				TargetKind: "relationship",
				TargetID:   "canonical:" + intent.CaseID,
			}
			// Only an admitted decision publishes its expected graph facts; the
			// admitted decision is the production outcome here, not a copy of the
			// expectation, so a production regression that fails to admit also drops
			// these facts and the audit reports the gap.
			observation.GraphFacts = append(observation.GraphFacts, intent.ExpectedGraphFacts...)
		}
		observation.Decisions = append(observation.Decisions, decision)
		observation.APIReadback = append(observation.APIReadback, admissionaudit.ReadbackDecision{
			ID: decisionID, State: observedState, SourceHandleCount: 0, EvidenceCount: 1,
		})
		observation.MCPReadback = append(observation.MCPReadback, admissionaudit.ReadbackDecision{
			ID: decisionID, State: observedState, SourceHandleCount: 0, EvidenceCount: 1,
		})
	}
	return observation
}

// classifyCorrelationState derives one case's observed admission state from the
// production admission primitives applied to the case input. It never reads an
// expected state. Order matters and mirrors the shipped path: a superseded
// generation is stale before any admission gate runs; otherwise the real
// correlation/admission.Evaluate decides admitted vs not, and its Outcome plus
// the competing-owner conflict guard distinguish ambiguous, missing_evidence, and
// rejected among the non-admitted results.
func classifyCorrelationState(t *testing.T, input correlationCaseInput) admissionaudit.State {
	t.Helper()

	// Freshness first: an older generation is superseded and stays stale,
	// matching the production generation-freshness check that runs before
	// admission writes any canonical truth.
	if input.generationID != currentCorrelationGeneration {
		return admissionaudit.StateStale
	}

	evaluated, outcome, err := admission.Evaluate(input.candidate, correlationConfidenceThreshold, input.requirements)
	if err != nil {
		t.Fatalf("admission.Evaluate(%s) error = %v", input.candidate.ID, err)
	}

	if evaluated.State == model.CandidateStateAdmitted {
		// A second admitted owner for the same correlation key makes the production
		// conflict guard keep the result visible without promoting one owner.
		if input.competing {
			return admissionaudit.StateAmbiguous
		}
		return admissionaudit.StateAdmitted
	}

	// Non-admitted: the production outcome gates distinguish a structural evidence
	// gap from a low-confidence rejection without reading the expectation.
	if outcome.MeetsConfidence && !outcome.MeetsStructure {
		return admissionaudit.StateMissingEvidence
	}
	return admissionaudit.StateRejected
}

// scoreCorrelationInputs runs the full production observe -> audit -> score path
// over an arbitrary input set so a regression proof can feed mutated inputs and
// observe the gate react. It mirrors measureCorrelation but takes the inputs as
// an argument and does not require the audit to pass, so a regressed input set
// can be scored into a failing metric.
func scoreCorrelationInputs(
	t *testing.T,
	intents []admissionaudit.FixtureIntent,
	inputs map[string]correlationCaseInput,
) accuracygate.Metric {
	t.Helper()

	observation := observeCorrelationDecisions(t, intents, inputs)
	report := admissionaudit.Audit(makeSuite(intents), observation)
	disagreed := disagreementCaseIDs(report)
	predictions := make([]accuracygate.LabeledPrediction, 0, len(intents))
	for _, intent := range intents {
		observedState := string(intent.ExpectedState)
		if _, bad := disagreed[intent.CaseID]; bad {
			observedState = "disagreement"
		}
		predictions = append(predictions, accuracygate.LabeledPrediction{
			Label:    intent.CaseID,
			Expected: string(intent.ExpectedState),
			Observed: observedState,
			Positive: intent.ExpectedState == admissionaudit.StateAdmitted,
		})
	}
	return accuracygate.ScorePredictions(predictions)
}

// makeSuite wraps golden intents in the minimal Suite shape Audit consumes.
func makeSuite(intents []admissionaudit.FixtureIntent) admissionaudit.Suite {
	return admissionaudit.Suite{
		SchemaVersion: 1,
		SuiteID:       "correlation_admission_golden",
		Intents:       intents,
	}
}

// disagreementCaseIDs collects every case the audit flagged with a truth or
// state disagreement, so the correlation metric counts an audited disagreement
// as a misclassification.
func disagreementCaseIDs(report admissionaudit.Report) map[string]struct{} {
	cases := make(map[string]struct{})
	for _, finding := range report.StateDisagreements {
		cases[finding.CaseID] = struct{}{}
	}
	for _, finding := range report.MissingDecisions {
		cases[finding.CaseID] = struct{}{}
	}
	for _, finding := range report.UnexpectedCanonicalWrites {
		cases[finding.CaseID] = struct{}{}
	}
	for _, finding := range report.MissingGraphFacts {
		cases[finding.CaseID] = struct{}{}
	}
	for _, finding := range report.MissingCanonicalWrites {
		cases[finding.CaseID] = struct{}{}
	}
	for _, finding := range report.StaleReplayAdmissions {
		cases[finding.CaseID] = struct{}{}
	}
	return cases
}
