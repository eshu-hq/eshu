// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admissionaudit

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestAuditPassesWhenFixtureDecisionGraphAndReadbackAgree(t *testing.T) {
	t.Parallel()

	suite := fixtureSuite()
	observed := Observation{
		Decisions: []Decision{
			admittedDecision("deployable-service-admitted", "deployable-unit:api"),
			nonAdmittedDecision("package-source-ambiguous", StateAmbiguous),
			nonAdmittedDecision("cloud-resource-rejected", StateRejected),
		},
		GraphFacts: []GraphFact{{
			CaseID: "deployable-service-admitted",
			Kind:   "relationship",
			ID:     "deployable-unit:api",
		}},
		APIReadback: []ReadbackDecision{
			readback("admission:deployable-service-admitted", StateAdmitted, false, 1, 1),
			readback("admission:package-source-ambiguous", StateAmbiguous, false, 1, 1),
			readback("admission:cloud-resource-rejected", StateRejected, false, 1, 1),
		},
		MCPReadback: []ReadbackDecision{
			readback("admission:deployable-service-admitted", StateAdmitted, false, 1, 1),
			readback("admission:package-source-ambiguous", StateAmbiguous, false, 1, 1),
			readback("admission:cloud-resource-rejected", StateRejected, false, 1, 1),
		},
	}

	report := Audit(suite, observed)
	if !report.Pass() {
		t.Fatalf("Audit() failed matching truth: %s", report.Summary())
	}
}

func TestAuditReportsDecisionStateDisagreement(t *testing.T) {
	t.Parallel()

	suite := fixtureSuite()
	observed := passingObservation()
	observed.Decisions[1].State = StateAdmitted

	report := Audit(suite, observed)
	assertAuditKeys(t, "state disagreements", report.StateDisagreements, []string{
		"package-source-ambiguous|admission:package-source-ambiguous",
	})
	if report.Pass() {
		t.Fatal("Audit() passed with fixture/decision state disagreement")
	}
}

func TestAuditReportsCanonicalTruthForNonAdmittedCases(t *testing.T) {
	t.Parallel()

	suite := fixtureSuite()
	observed := passingObservation()
	observed.Decisions[1].CanonicalWrite = CanonicalWrite{
		Written:    true,
		TargetKind: "package_source",
		TargetID:   "package-source:team-api",
	}
	observed.GraphFacts = append(observed.GraphFacts, GraphFact{
		CaseID: "package-source-ambiguous",
		Kind:   "relationship",
		ID:     "package-source:team-api",
	})

	report := Audit(suite, observed)
	assertAuditKeys(t, "unexpected canonical writes", report.UnexpectedCanonicalWrites, []string{
		"package-source-ambiguous|package-source:team-api",
	})
	assertAuditKeys(t, "unexpected graph facts", report.UnexpectedGraphFacts, []string{
		"package-source-ambiguous|relationship|package-source:team-api",
	})
}

func TestAuditReportsMissingDecisionExplanation(t *testing.T) {
	t.Parallel()

	suite := fixtureSuite()
	observed := passingObservation()
	observed.Decisions[0].SourceHandles = nil
	observed.Decisions[0].EvidenceCount = 0
	observed.Decisions[0].Explanation = ""

	report := Audit(suite, observed)
	assertAuditKeys(t, "missing explanations", report.MissingExplanations, []string{
		"deployable-service-admitted|admission:deployable-service-admitted",
	})
}

func TestAuditReportsAPIAndMCPReadbackDisagreement(t *testing.T) {
	t.Parallel()

	suite := fixtureSuite()
	observed := passingObservation()
	observed.MCPReadback[0].State = StateRejected
	observed.MCPReadback[0].Truncated = true
	observed.MCPReadback[0].SourceHandleCount = 0
	observed.MCPReadback[0].EvidenceCount = 0

	report := Audit(suite, observed)
	assertAuditKeys(t, "readback disagreements", report.ReadbackDisagreements, []string{
		"admission:deployable-service-admitted|evidence_count",
		"admission:deployable-service-admitted|source_handle_count",
		"admission:deployable-service-admitted|state",
		"admission:deployable-service-admitted|truncated",
	})
}

func TestAuditReportsDuplicateDecisionsAndStaleReplayAdmissions(t *testing.T) {
	t.Parallel()

	suite := fixtureSuite()
	observed := passingObservation()
	duplicate := observed.Decisions[0]
	duplicate.FreshnessState = "stale"
	observed.Decisions = append(observed.Decisions, duplicate)

	report := Audit(suite, observed)
	assertAuditKeys(t, "duplicate decisions", report.DuplicateDecisions, []string{
		"admission:deployable-service-admitted",
	})
	assertAuditKeys(t, "stale replay admissions", report.StaleReplayAdmissions, []string{
		"deployable-service-admitted|admission:deployable-service-admitted",
	})
}

func TestLoadSuiteReadsProductTruthFixture(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "..", "..", "tests", "fixtures", "product_truth", "expected", "correlation_admission_golden.json")
	suite, err := LoadSuite(path)
	if err != nil {
		t.Fatalf("LoadSuite() error = %v", err)
	}
	if suite.SuiteID != "correlation_admission_golden" {
		t.Fatalf("SuiteID = %q, want correlation_admission_golden", suite.SuiteID)
	}
	if len(suite.Intents) < 6 {
		t.Fatalf("intents = %d, want at least 6", len(suite.Intents))
	}
	for _, intent := range suite.Intents {
		if !strings.Contains(intent.FixtureIntent, intent.CaseID) {
			t.Fatalf("fixture intent %q does not cite case id %q", intent.FixtureIntent, intent.CaseID)
		}
	}
}

func TestValidateSuiteRejectsUnknownExpectedState(t *testing.T) {
	t.Parallel()

	suite := fixtureSuite()
	suite.Intents[0].ExpectedState = "rejectd"

	err := validateSuite(suite)
	if err == nil {
		t.Fatal("validateSuite() accepted an unknown expected_state")
	}
	if !strings.Contains(err.Error(), "not a known admission state") {
		t.Fatalf("validateSuite() error = %v, want unknown-state message", err)
	}
}

func TestAuditReportsLogicalDuplicateDecisionsDeterministically(t *testing.T) {
	t.Parallel()

	suite := fixtureSuite()
	observed := passingObservation()
	// A second decision with the same logical identity but a different row ID
	// must be detected, and the audited decision must be the lowest ID so the
	// report is stable across map iteration order.
	twin := observed.Decisions[0]
	twin.ID = "admission:deployable-service-admitted:dup"
	twin.State = StateRejected
	observed.Decisions = append(observed.Decisions, twin)

	report := Audit(suite, observed)
	assertAuditKeys(t, "logical duplicate decisions", report.LogicalDuplicateDecisions, []string{
		"deployable-service-admitted|admission:deployable-service-admitted",
	})
	if report.Pass() {
		t.Fatal("Audit() passed despite a logical duplicate decision")
	}
	// The lowest-ID decision (the admitted one) is selected, so no spurious
	// state disagreement is reported for the twin.
	assertAuditKeys(t, "state disagreements", report.StateDisagreements, nil)
}

func TestAuditReportsCanonicalLeakForStaleIntent(t *testing.T) {
	t.Parallel()

	suite := Suite{
		SchemaVersion: 1,
		SuiteID:       "stale-intent",
		Intents: []FixtureIntent{{
			CaseID:        "package-stale",
			Domain:        "package_source_correlation",
			ScopeID:       "package-registry:npm:team-api",
			GenerationID:  "generation-previous",
			ExpectedState: StateStale,
			FixtureIntent: "package-stale expects an older generation to stay stale and never publish current truth",
		}},
	}
	decision := nonAdmittedDecision("package-stale", StateAdmitted)
	decision.Domain = "package_source_correlation"
	decision.ScopeID = "package-registry:npm:team-api"
	decision.GenerationID = "generation-previous"
	decision.CanonicalWrite = CanonicalWrite{Written: true, TargetID: "package-source:team-api"}
	observed := Observation{
		Decisions:   []Decision{decision},
		APIReadback: []ReadbackDecision{readback(decision.ID, StateAdmitted, false, 1, 1)},
		MCPReadback: []ReadbackDecision{readback(decision.ID, StateAdmitted, false, 1, 1)},
	}

	report := Audit(suite, observed)
	assertAuditKeys(t, "state disagreements", report.StateDisagreements, []string{
		"package-stale|admission:package-stale",
	})
	assertAuditKeys(t, "unexpected canonical writes", report.UnexpectedCanonicalWrites, []string{
		"package-stale|package-source:team-api",
	})
}

func TestAuditFlagsNonAdmittedDecisionMissingExplanation(t *testing.T) {
	t.Parallel()

	// Even a non-admitted decision must explain itself; an operator must be able
	// to ask why a candidate was rejected. A decision with no source handles,
	// no evidence count, and no explanation text is an explainability failure.
	suite := fixtureSuite()
	observed := passingObservation()
	observed.Decisions[2].SourceHandles = nil
	observed.Decisions[2].EvidenceCount = 0
	observed.Decisions[2].Explanation = ""

	report := Audit(suite, observed)
	assertAuditKeys(t, "missing explanations", report.MissingExplanations, []string{
		"cloud-resource-rejected|admission:cloud-resource-rejected",
	})
}

func TestAuditReportsReadbackTruthDisagreement(t *testing.T) {
	t.Parallel()

	// Both public surfaces returning the same wrong state must still fail: they
	// agree with each other but disagree with the audited reducer decision.
	suite := fixtureSuite()
	observed := passingObservation()
	observed.APIReadback[0].State = StateRejected
	observed.MCPReadback[0].State = StateRejected

	report := Audit(suite, observed)
	if len(report.ReadbackDisagreements) != 0 {
		t.Fatalf("expected no api-vs-mcp disagreement, got %v", report.ReadbackDisagreements)
	}
	assertAuditKeys(t, "readback truth disagreements", report.ReadbackTruthDisagreements, []string{
		"admission:deployable-service-admitted|api_state",
		"admission:deployable-service-admitted|mcp_state",
	})
	if report.Pass() {
		t.Fatal("Audit() passed while both readbacks disagreed with reducer truth")
	}
}

func TestAuditReportsMissingCanonicalWriteForAdmittedDecision(t *testing.T) {
	t.Parallel()

	// An admitted decision whose reducer payload did not record the canonical
	// write must fail even when a graph fact is present, because that is the
	// reducer-vs-graph disagreement the suite must catch.
	suite := fixtureSuite()
	observed := passingObservation()
	observed.Decisions[0].CanonicalWrite.Written = false

	report := Audit(suite, observed)
	assertAuditKeys(t, "missing canonical writes", report.MissingCanonicalWrites, []string{
		"deployable-service-admitted|admission:deployable-service-admitted",
	})
	if report.Pass() {
		t.Fatal("Audit() passed for an admitted decision with no canonical write")
	}
}

func fixtureSuite() Suite {
	return Suite{
		SchemaVersion: 1,
		SuiteID:       "correlation_admission_golden",
		Intents: []FixtureIntent{
			{
				CaseID:        "deployable-service-admitted",
				Domain:        "deployable_unit_correlation",
				ScopeID:       "git-repository-scope:team/api",
				GenerationID:  "generation-current",
				ExpectedState: StateAdmitted,
				ExpectedGraphFacts: []GraphFact{{
					CaseID: "deployable-service-admitted",
					Kind:   "relationship",
					ID:     "deployable-unit:api",
				}},
				FixtureIntent: "deployable-service-admitted expects one service/deployment graph relationship",
			},
			{
				CaseID:        "package-source-ambiguous",
				Domain:        "package_source_correlation",
				ScopeID:       "package-registry:npm:team-api",
				GenerationID:  "generation-current",
				ExpectedState: StateAmbiguous,
				FixtureIntent: "package-source-ambiguous expects no ownership graph write",
			},
			{
				CaseID:        "cloud-resource-rejected",
				Domain:        "cloud_inventory_admission",
				ScopeID:       "cloud:gcp:team",
				GenerationID:  "generation-current",
				ExpectedState: StateRejected,
				FixtureIntent: "cloud-resource-rejected expects provider evidence to stay provenance-only",
			},
		},
	}
}

func passingObservation() Observation {
	return Observation{
		Decisions: []Decision{
			admittedDecision("deployable-service-admitted", "deployable-unit:api"),
			nonAdmittedDecision("package-source-ambiguous", StateAmbiguous),
			nonAdmittedDecision("cloud-resource-rejected", StateRejected),
		},
		GraphFacts: []GraphFact{{
			CaseID: "deployable-service-admitted",
			Kind:   "relationship",
			ID:     "deployable-unit:api",
		}},
		APIReadback: []ReadbackDecision{
			readback("admission:deployable-service-admitted", StateAdmitted, false, 1, 1),
			readback("admission:package-source-ambiguous", StateAmbiguous, false, 1, 1),
			readback("admission:cloud-resource-rejected", StateRejected, false, 1, 1),
		},
		MCPReadback: []ReadbackDecision{
			readback("admission:deployable-service-admitted", StateAdmitted, false, 1, 1),
			readback("admission:package-source-ambiguous", StateAmbiguous, false, 1, 1),
			readback("admission:cloud-resource-rejected", StateRejected, false, 1, 1),
		},
	}
}

func admittedDecision(caseID string, targetID string) Decision {
	decision := nonAdmittedDecision(caseID, StateAdmitted)
	decision.CanonicalWrite = CanonicalWrite{
		Written:    true,
		TargetKind: "relationship",
		TargetID:   targetID,
	}
	return decision
}

func nonAdmittedDecision(caseID string, state State) Decision {
	return Decision{
		ID:             "admission:" + caseID,
		CaseID:         caseID,
		Domain:         domainForCase(caseID),
		State:          state,
		ScopeID:        scopeForCase(caseID),
		GenerationID:   "generation-current",
		FreshnessState: "current",
		SourceHandles: []SourceHandle{{
			Kind: "fact",
			ID:   "fact:" + caseID,
		}},
		EvidenceCount: 1,
		Explanation:   caseID + " fixture evidence",
	}
}

func readback(id string, state State, truncated bool, sourceHandles int, evidence int) ReadbackDecision {
	return ReadbackDecision{
		ID:                id,
		State:             state,
		Truncated:         truncated,
		SourceHandleCount: sourceHandles,
		EvidenceCount:     evidence,
	}
}

func domainForCase(caseID string) string {
	switch caseID {
	case "deployable-service-admitted":
		return "deployable_unit_correlation"
	case "package-source-ambiguous":
		return "package_source_correlation"
	case "cloud-resource-rejected":
		return "cloud_inventory_admission"
	default:
		return ""
	}
}

func scopeForCase(caseID string) string {
	switch caseID {
	case "deployable-service-admitted":
		return "git-repository-scope:team/api"
	case "package-source-ambiguous":
		return "package-registry:npm:team-api"
	case "cloud-resource-rejected":
		return "cloud:gcp:team"
	default:
		return ""
	}
}

func assertAuditKeys[T interface{ Key() string }](t *testing.T, label string, got []T, want []string) {
	t.Helper()

	keys := make([]string, 0, len(got))
	for _, item := range got {
		keys = append(keys, item.Key())
	}
	if len(keys) != len(want) {
		t.Fatalf("%s len = %d (%v), want %d (%v)", label, len(keys), keys, len(want), want)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Fatalf("%s[%d] = %q, want %q; got all %v", label, i, keys[i], want[i], keys)
		}
	}
}
