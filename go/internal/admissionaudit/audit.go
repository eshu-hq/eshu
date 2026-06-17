package admissionaudit

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// LoadSuite reads an independent checked-in admission audit fixture suite.
func LoadSuite(path string) (Suite, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Suite{}, fmt.Errorf("read admission audit suite %q: %w", path, err)
	}
	var suite Suite
	if err := json.Unmarshal(data, &suite); err != nil {
		return Suite{}, fmt.Errorf("parse admission audit suite %q: %w", path, err)
	}
	if err := validateSuite(suite); err != nil {
		return Suite{}, fmt.Errorf("admission audit suite %q is invalid: %w", path, err)
	}
	return suite, nil
}

// Audit compares one observed reducer/graph/readback snapshot with fixture
// intent.
func Audit(suite Suite, observed Observation) Report {
	decisions, duplicates := indexDecisions(observed.Decisions)
	graphFacts := indexGraphFacts(observed.GraphFacts)
	expectedGraph := expectedGraphFacts(suite.Intents)
	apiReadback := indexReadback(observed.APIReadback)
	mcpReadback := indexReadback(observed.MCPReadback)

	report := Report{
		DuplicateDecisions: duplicates,
		StaleReplayAdmissions: staleReplayAdmissions(
			suite.Intents,
			observed.Decisions,
		),
		UnexpectedGraphFacts: unexpectedGraphFacts(
			graphFacts,
			expectedGraph,
		),
	}
	for _, intent := range suite.Intents {
		decision, ok := decisionForIntent(intent, decisions)
		if !ok {
			report.MissingDecisions = append(report.MissingDecisions, CaseFinding{
				CaseID: intent.CaseID,
				Reason: "missing_reducer_decision",
			})
			continue
		}
		report.auditDecision(intent, decision, graphFacts, apiReadback, mcpReadback)
	}
	sortReport(&report)
	return report
}

func (r *Report) auditDecision(
	intent FixtureIntent,
	decision Decision,
	graphFacts map[string]GraphFact,
	apiReadback map[string]ReadbackDecision,
	mcpReadback map[string]ReadbackDecision,
) {
	if decision.State != intent.ExpectedState {
		r.StateDisagreements = append(r.StateDisagreements, DecisionFinding{
			CaseID:     intent.CaseID,
			DecisionID: decision.ID,
		})
	}
	if missingExplanation(decision) {
		r.MissingExplanations = append(r.MissingExplanations, DecisionFinding{
			CaseID:     intent.CaseID,
			DecisionID: decision.ID,
		})
	}
	if intent.ExpectedState == StateAdmitted {
		for _, fact := range intent.ExpectedGraphFacts {
			if _, ok := graphFacts[fact.Key()]; !ok {
				r.MissingGraphFacts = append(r.MissingGraphFacts, fact)
			}
		}
	} else {
		if decision.CanonicalWrite.Written {
			r.UnexpectedCanonicalWrites = append(r.UnexpectedCanonicalWrites, CanonicalFinding{
				CaseID:   intent.CaseID,
				TargetID: decision.CanonicalWrite.TargetID,
			})
		}
	}
	api, apiOK := apiReadback[decision.ID]
	if !apiOK {
		r.MissingAPIReadback = append(r.MissingAPIReadback, DecisionFinding{
			CaseID:     intent.CaseID,
			DecisionID: decision.ID,
		})
	}
	mcp, mcpOK := mcpReadback[decision.ID]
	if !mcpOK {
		r.MissingMCPReadback = append(r.MissingMCPReadback, DecisionFinding{
			CaseID:     intent.CaseID,
			DecisionID: decision.ID,
		})
	}
	if apiOK && mcpOK {
		r.ReadbackDisagreements = append(r.ReadbackDisagreements, compareReadback(api, mcp)...)
	}
}

func validateSuite(suite Suite) error {
	if suite.SchemaVersion != 1 {
		return fmt.Errorf("schema_version = %d, want 1", suite.SchemaVersion)
	}
	if strings.TrimSpace(suite.SuiteID) == "" {
		return fmt.Errorf("suite_id is required")
	}
	if len(suite.Intents) == 0 {
		return fmt.Errorf("fixture_intents is required")
	}
	seen := make(map[string]struct{}, len(suite.Intents))
	for i, intent := range suite.Intents {
		if strings.TrimSpace(intent.CaseID) == "" {
			return fmt.Errorf("fixture_intents[%d].case_id is required", i)
		}
		if _, ok := seen[intent.CaseID]; ok {
			return fmt.Errorf("fixture_intents[%d].case_id %q is duplicate", i, intent.CaseID)
		}
		seen[intent.CaseID] = struct{}{}
		if strings.TrimSpace(intent.Domain) == "" {
			return fmt.Errorf("fixture_intents[%d].domain is required", i)
		}
		if strings.TrimSpace(intent.ScopeID) == "" {
			return fmt.Errorf("fixture_intents[%d].scope_id is required", i)
		}
		if strings.TrimSpace(intent.GenerationID) == "" {
			return fmt.Errorf("fixture_intents[%d].generation_id is required", i)
		}
		if strings.TrimSpace(string(intent.ExpectedState)) == "" {
			return fmt.Errorf("fixture_intents[%d].expected_state is required", i)
		}
		if strings.TrimSpace(intent.FixtureIntent) == "" {
			return fmt.Errorf("fixture_intents[%d].fixture_intent is required", i)
		}
	}
	return nil
}

func missingExplanation(decision Decision) bool {
	return len(decision.SourceHandles) == 0 &&
		decision.EvidenceCount == 0 &&
		strings.TrimSpace(decision.Explanation) == ""
}

func decisionForIntent(intent FixtureIntent, decisions map[string]Decision) (Decision, bool) {
	for _, decision := range decisions {
		if decision.CaseID == intent.CaseID &&
			decision.Domain == intent.Domain &&
			decision.ScopeID == intent.ScopeID &&
			decision.GenerationID == intent.GenerationID {
			return decision, true
		}
	}
	return Decision{}, false
}

func indexDecisions(decisions []Decision) (map[string]Decision, []DuplicateFinding) {
	index := make(map[string]Decision, len(decisions))
	duplicates := make([]DuplicateFinding, 0)
	for _, decision := range decisions {
		if _, ok := index[decision.ID]; ok {
			duplicates = append(duplicates, DuplicateFinding{ID: decision.ID})
			continue
		}
		index[decision.ID] = decision
	}
	sortDuplicates(duplicates)
	return index, duplicates
}

func indexGraphFacts(facts []GraphFact) map[string]GraphFact {
	index := make(map[string]GraphFact, len(facts))
	for _, fact := range facts {
		index[fact.Key()] = fact
	}
	return index
}

func expectedGraphFacts(intents []FixtureIntent) map[string]GraphFact {
	index := make(map[string]GraphFact)
	for _, intent := range intents {
		if intent.ExpectedState != StateAdmitted {
			continue
		}
		for _, fact := range intent.ExpectedGraphFacts {
			index[fact.Key()] = fact
		}
	}
	return index
}

func unexpectedGraphFacts(observed map[string]GraphFact, expected map[string]GraphFact) []GraphFact {
	unexpected := make([]GraphFact, 0)
	for key, fact := range observed {
		if _, ok := expected[key]; !ok {
			unexpected = append(unexpected, fact)
		}
	}
	sortGraphFacts(unexpected)
	return unexpected
}

func staleReplayAdmissions(intents []FixtureIntent, decisions []Decision) []DecisionFinding {
	knownCases := make(map[string]struct{}, len(intents))
	for _, intent := range intents {
		knownCases[intent.CaseID] = struct{}{}
	}
	findingsByKey := make(map[string]DecisionFinding)
	for _, decision := range decisions {
		if _, ok := knownCases[decision.CaseID]; !ok {
			continue
		}
		if decision.State != StateAdmitted || decision.FreshnessState != string(StateStale) {
			continue
		}
		finding := DecisionFinding{CaseID: decision.CaseID, DecisionID: decision.ID}
		findingsByKey[finding.Key()] = finding
	}
	findings := make([]DecisionFinding, 0, len(findingsByKey))
	for _, finding := range findingsByKey {
		findings = append(findings, finding)
	}
	sortDecisionFindings(findings)
	return findings
}

func indexReadback(readback []ReadbackDecision) map[string]ReadbackDecision {
	index := make(map[string]ReadbackDecision, len(readback))
	for _, decision := range readback {
		index[decision.ID] = decision
	}
	return index
}

func compareReadback(api ReadbackDecision, mcp ReadbackDecision) []ReadbackFinding {
	findings := make([]ReadbackFinding, 0)
	if api.State != mcp.State {
		findings = append(findings, ReadbackFinding{DecisionID: api.ID, Field: "state"})
	}
	if api.Truncated != mcp.Truncated {
		findings = append(findings, ReadbackFinding{DecisionID: api.ID, Field: "truncated"})
	}
	if api.SourceHandleCount != mcp.SourceHandleCount {
		findings = append(findings, ReadbackFinding{DecisionID: api.ID, Field: "source_handle_count"})
	}
	if api.EvidenceCount != mcp.EvidenceCount {
		findings = append(findings, ReadbackFinding{DecisionID: api.ID, Field: "evidence_count"})
	}
	sortReadbackFindings(findings)
	return findings
}

func sortReport(report *Report) {
	sortCaseFindings(report.MissingDecisions)
	sortDecisionFindings(report.StateDisagreements)
	sortDecisionFindings(report.MissingExplanations)
	sortGraphFacts(report.MissingGraphFacts)
	sortGraphFacts(report.UnexpectedGraphFacts)
	sortCanonicalFindings(report.UnexpectedCanonicalWrites)
	sortDuplicates(report.DuplicateDecisions)
	sortDecisionFindings(report.StaleReplayAdmissions)
	sortDecisionFindings(report.MissingAPIReadback)
	sortDecisionFindings(report.MissingMCPReadback)
	sortReadbackFindings(report.ReadbackDisagreements)
}

func sortCaseFindings(findings []CaseFinding) {
	sort.Slice(findings, func(i int, j int) bool {
		return findings[i].Key() < findings[j].Key()
	})
}

func sortDecisionFindings(findings []DecisionFinding) {
	sort.Slice(findings, func(i int, j int) bool {
		return findings[i].Key() < findings[j].Key()
	})
}

func sortGraphFacts(facts []GraphFact) {
	sort.Slice(facts, func(i int, j int) bool {
		return facts[i].Key() < facts[j].Key()
	})
}

func sortCanonicalFindings(findings []CanonicalFinding) {
	sort.Slice(findings, func(i int, j int) bool {
		return findings[i].Key() < findings[j].Key()
	})
}

func sortDuplicates(findings []DuplicateFinding) {
	sort.Slice(findings, func(i int, j int) bool {
		return findings[i].Key() < findings[j].Key()
	})
}

func sortReadbackFindings(findings []ReadbackFinding) {
	sort.Slice(findings, func(i int, j int) bool {
		return findings[i].Key() < findings[j].Key()
	})
}
