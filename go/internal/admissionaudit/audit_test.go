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
