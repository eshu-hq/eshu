// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
	data, err := os.ReadFile(path) // #nosec G304 -- reads a checked-in admission audit fixture suite at a path supplied by the test/CLI caller, not an HTTP/MCP request param
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
// intent and returns a deterministic Report.
//
// Decisions are matched to a fixture intent by logical identity (case, domain,
// scope, generation), not by row ID. Decisions that repeat a row ID are reported
// under DuplicateDecisions and only the first is retained; decisions that repeat
// a logical identity under different row IDs are reported under
// LogicalDuplicateDecisions, and the lowest row ID is audited so the output does
// not depend on input order.
func Audit(suite Suite, observed Observation) Report {
	decisions, duplicates := indexDecisions(observed.Decisions)
	logicalIndex := indexDecisionsByIdentity(decisions)
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
		decision, ok := decisionForIntent(intent, logicalIndex)
		if !ok {
			report.MissingDecisions = append(report.MissingDecisions, CaseFinding{
				CaseID: intent.CaseID,
				Reason: "missing_reducer_decision",
			})
			continue
		}
		if len(logicalIndex[identityKey(intent.CaseID, intent.Domain, intent.ScopeID, intent.GenerationID)]) > 1 {
			report.LogicalDuplicateDecisions = append(report.LogicalDuplicateDecisions, DecisionFinding{
				CaseID:     intent.CaseID,
				DecisionID: decision.ID,
			})
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
		// An admitted decision must itself record the canonical write. Trusting
		// only the graph snapshot would let a reducer that left
		// CanonicalWrite.Written=false pass whenever a stale or externally
		// supplied graph fact happens to be present, hiding the exact
		// reducer-vs-graph disagreement this suite must catch.
		if !decision.CanonicalWrite.Written {
			r.MissingCanonicalWrites = append(r.MissingCanonicalWrites, DecisionFinding{
				CaseID:     intent.CaseID,
				DecisionID: decision.ID,
			})
		}
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
	// Each public surface must also agree with the audited reducer decision, not
	// only with the other surface. Without this, API and MCP returning the same
	// wrong state (for example both `rejected` for an `admitted` decision) would
	// agree with each other and slip past compareReadback.
	if apiOK && api.State != decision.State {
		r.ReadbackTruthDisagreements = append(r.ReadbackTruthDisagreements, ReadbackFinding{
			DecisionID: decision.ID,
			Field:      "api_state",
		})
	}
	if mcpOK && mcp.State != decision.State {
		r.ReadbackTruthDisagreements = append(r.ReadbackTruthDisagreements, ReadbackFinding{
			DecisionID: decision.ID,
			Field:      "mcp_state",
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
		if !validState(intent.ExpectedState) {
			return fmt.Errorf(
				"fixture_intents[%d].expected_state %q is not a known admission state",
				i,
				intent.ExpectedState,
			)
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

// identityKey builds the logical admission-decision identity used to match a
// reducer decision to a fixture intent independently of the decision row ID.
func identityKey(caseID string, domain string, scopeID string, generationID string) string {
	return caseID + "|" + domain + "|" + scopeID + "|" + generationID
}

// indexDecisionsByIdentity groups decisions by their logical identity so the
// match against a fixture intent is deterministic regardless of map iteration
// order, and so logical duplicates (same identity, different row ID) are
// observable. Each group is sorted by decision ID.
func indexDecisionsByIdentity(decisions map[string]Decision) map[string][]Decision {
	index := make(map[string][]Decision, len(decisions))
	for _, decision := range decisions {
		key := identityKey(decision.CaseID, decision.Domain, decision.ScopeID, decision.GenerationID)
		index[key] = append(index[key], decision)
	}
	for key := range index {
		group := index[key]
		sort.Slice(group, func(i int, j int) bool {
			return group[i].ID < group[j].ID
		})
	}
	return index
}

// decisionForIntent returns the lowest-ID decision matching the intent identity.
// Selecting the sorted-first decision keeps audit output deterministic when more
// than one decision shares the same logical identity.
func decisionForIntent(intent FixtureIntent, logicalIndex map[string][]Decision) (Decision, bool) {
	group := logicalIndex[identityKey(intent.CaseID, intent.Domain, intent.ScopeID, intent.GenerationID)]
	if len(group) == 0 {
		return Decision{}, false
	}
	return group[0], true
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
	sortDecisionFindings(report.MissingCanonicalWrites)
	sortGraphFacts(report.UnexpectedGraphFacts)
	sortCanonicalFindings(report.UnexpectedCanonicalWrites)
	sortDuplicates(report.DuplicateDecisions)
	sortDecisionFindings(report.LogicalDuplicateDecisions)
	sortDecisionFindings(report.StaleReplayAdmissions)
	sortDecisionFindings(report.MissingAPIReadback)
	sortDecisionFindings(report.MissingMCPReadback)
	sortReadbackFindings(report.ReadbackDisagreements)
	sortReadbackFindings(report.ReadbackTruthDisagreements)
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
