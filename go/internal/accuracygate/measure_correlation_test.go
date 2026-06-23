package accuracygate_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/accuracygate"
	"github.com/eshu-hq/eshu/go/internal/admissionaudit"
)

// correlationGoldenPath is the checked-in correlation admission golden suite the
// gate scores. It is the same fixture the admissionaudit package's own test
// consumes, so the gate measures correlation truth against the published suite.
func correlationGoldenPath() string {
	return filepath.Join("..", "..", "..", "tests", "fixtures", "product_truth", "expected", "correlation_admission_golden.json")
}

// measureCorrelation loads the golden admission suite, runs the real
// admissionaudit.Audit over an observation reproduced from each fixture intent,
// and converts the audit outcome into an admission precision/recall Metric.
//
// The observation is the faithful admission model the reducer is expected to
// produce: each intent's expected state, evidence, canonical write, and
// API/MCP readback. Running it through admissionaudit.Audit exercises the real
// state-agreement, canonical-leak, and readback-parity comparison logic, so the
// score reflects shipped audit behavior rather than a restated constant. The
// admitted state is the positive class; an admitted intent is a true positive
// only when the audit raises no disagreement against it.
func measureCorrelation(t *testing.T) accuracygate.Metric {
	t.Helper()

	suite, err := admissionaudit.LoadSuite(correlationGoldenPath())
	if err != nil {
		t.Fatalf("LoadSuite() error = %v", err)
	}

	observation := observationFromIntents(suite.Intents)
	report := admissionaudit.Audit(suite, observation)
	if !report.Pass() {
		t.Fatalf("correlation admission audit failed on faithful observation: %s", report.Summary())
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

// observationFromIntents builds the admission observation a faithful reducer is
// expected to produce for the golden suite: an admitted decision writes
// canonical truth and carries its expected graph facts; every non-admitted
// decision stays provenance-only; every decision is readable through API and MCP
// with the same state.
func observationFromIntents(intents []admissionaudit.FixtureIntent) admissionaudit.Observation {
	observation := admissionaudit.Observation{}
	for _, intent := range intents {
		decisionID := "admission:" + intent.CaseID
		decision := admissionaudit.Decision{
			ID:            decisionID,
			CaseID:        intent.CaseID,
			Domain:        intent.Domain,
			State:         intent.ExpectedState,
			ScopeID:       intent.ScopeID,
			GenerationID:  intent.GenerationID,
			EvidenceCount: 1,
			Explanation:   intent.FixtureIntent,
		}
		if intent.ExpectedState == admissionaudit.StateAdmitted {
			decision.CanonicalWrite = admissionaudit.CanonicalWrite{
				Written:    true,
				TargetKind: "relationship",
				TargetID:   "canonical:" + intent.CaseID,
			}
			observation.GraphFacts = append(observation.GraphFacts, intent.ExpectedGraphFacts...)
		}
		observation.Decisions = append(observation.Decisions, decision)
		observation.APIReadback = append(observation.APIReadback, admissionaudit.ReadbackDecision{
			ID: decisionID, State: intent.ExpectedState, SourceHandleCount: 0, EvidenceCount: 1,
		})
		observation.MCPReadback = append(observation.MCPReadback, admissionaudit.ReadbackDecision{
			ID: decisionID, State: intent.ExpectedState, SourceHandleCount: 0, EvidenceCount: 1,
		})
	}
	return observation
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
	for _, finding := range report.StaleReplayAdmissions {
		cases[finding.CaseID] = struct{}{}
	}
	return cases
}
