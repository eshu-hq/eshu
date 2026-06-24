// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admissionaudit

import (
	"fmt"
	"strings"
)

// State is the shared admission-decision state vocabulary used by the audit.
type State string

const (
	// StateAdmitted means a candidate was allowed to publish canonical truth.
	StateAdmitted State = "admitted"
	// StateRejected means a candidate was explicitly excluded.
	StateRejected State = "rejected"
	// StateAmbiguous means candidate conflict prevented a single canonical truth.
	StateAmbiguous State = "ambiguous"
	// StateStale means fresher source evidence superseded the candidate.
	StateStale State = "stale"
	// StateMissingEvidence means required admission evidence was absent.
	StateMissingEvidence State = "missing_evidence"
	// StatePermissionHidden means source data is not usable for the viewer.
	StatePermissionHidden State = "permission_hidden"
	// StateUnsupported means the domain does not support the candidate class.
	StateUnsupported State = "unsupported"
	// StateUnsafe means evidence made canonical publication unsafe.
	StateUnsafe State = "unsafe"
)

// validState reports whether s is one of the known admission-decision states.
// A fixture expected_state outside this closed vocabulary is a fixture error,
// not a reducer disagreement, so LoadSuite rejects it before any audit runs.
func validState(s State) bool {
	switch s {
	case StateAdmitted,
		StateRejected,
		StateAmbiguous,
		StateStale,
		StateMissingEvidence,
		StatePermissionHidden,
		StateUnsupported,
		StateUnsafe:
		return true
	default:
		return false
	}
}

// Suite is an independent fixture contract for admission truth audits.
type Suite struct {
	SchemaVersion        int               `json:"schema_version"`
	SuiteID              string            `json:"suite_id"`
	FixtureRoot          string            `json:"fixture_root,omitempty"`
	CapabilityAssertions []CapabilityClaim `json:"capability_assertions,omitempty"`
	Intents              []FixtureIntent   `json:"fixture_intents"`
}

// CapabilityClaim records the public capability covered by a fixture suite.
type CapabilityClaim struct {
	Capability string   `json:"capability"`
	Surfaces   []string `json:"surfaces,omitempty"`
	Positive   string   `json:"positive,omitempty"`
	Negative   string   `json:"negative,omitempty"`
	Ambiguous  string   `json:"ambiguous,omitempty"`
}

// FixtureIntent names the independent expected truth for one audit case.
type FixtureIntent struct {
	CaseID             string      `json:"case_id"`
	Domain             string      `json:"domain"`
	ScopeID            string      `json:"scope_id"`
	GenerationID       string      `json:"generation_id"`
	ExpectedState      State       `json:"expected_state"`
	ExpectedGraphFacts []GraphFact `json:"expected_graph_facts,omitempty"`
	FixtureIntent      string      `json:"fixture_intent"`
}

// Observation is one observed audit snapshot from reducer, graph, API, and MCP
// surfaces. It is intentionally plain data so tests and dogfood scripts can
// collect it without giving this package live service dependencies.
type Observation struct {
	Decisions   []Decision         `json:"decisions"`
	GraphFacts  []GraphFact        `json:"graph_facts"`
	APIReadback []ReadbackDecision `json:"api_readback"`
	MCPReadback []ReadbackDecision `json:"mcp_readback"`
}

// Decision is the reducer-owned admission decision projection used for audit
// comparison.
type Decision struct {
	ID             string         `json:"id"`
	CaseID         string         `json:"case_id"`
	Domain         string         `json:"domain"`
	State          State          `json:"state"`
	ScopeID        string         `json:"scope_id"`
	GenerationID   string         `json:"generation_id"`
	FreshnessState string         `json:"freshness_state,omitempty"`
	SourceHandles  []SourceHandle `json:"source_handles,omitempty"`
	EvidenceCount  int            `json:"evidence_count,omitempty"`
	Explanation    string         `json:"explanation,omitempty"`
	CanonicalWrite CanonicalWrite `json:"canonical_write,omitempty"`
}

// SourceHandle is a redaction-safe evidence pointer.
type SourceHandle struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// CanonicalWrite records a decision's canonical graph/content publication.
type CanonicalWrite struct {
	Written    bool   `json:"written"`
	TargetKind string `json:"target_kind,omitempty"`
	TargetID   string `json:"target_id,omitempty"`
}

// GraphFact is one canonical graph observation for a fixture case.
type GraphFact struct {
	CaseID string `json:"case_id"`
	Kind   string `json:"kind"`
	ID     string `json:"id"`
}

// Key returns the stable graph fact identity used in audit output.
func (f GraphFact) Key() string {
	return f.CaseID + "|" + f.Kind + "|" + f.ID
}

// ReadbackDecision is the bounded API or MCP admission-decision readback shape
// used to prove surface parity without embedding full response payloads.
type ReadbackDecision struct {
	ID                string `json:"id"`
	State             State  `json:"state"`
	Truncated         bool   `json:"truncated"`
	SourceHandleCount int    `json:"source_handle_count"`
	EvidenceCount     int    `json:"evidence_count"`
}

// Report records deterministic audit failures across reducer, graph, API, and
// MCP truth.
type Report struct {
	MissingDecisions           []CaseFinding      `json:"missing_decisions,omitempty"`
	StateDisagreements         []DecisionFinding  `json:"state_disagreements,omitempty"`
	MissingExplanations        []DecisionFinding  `json:"missing_explanations,omitempty"`
	MissingGraphFacts          []GraphFact        `json:"missing_graph_facts,omitempty"`
	MissingCanonicalWrites     []DecisionFinding  `json:"missing_canonical_writes,omitempty"`
	UnexpectedGraphFacts       []GraphFact        `json:"unexpected_graph_facts,omitempty"`
	UnexpectedCanonicalWrites  []CanonicalFinding `json:"unexpected_canonical_writes,omitempty"`
	DuplicateDecisions         []DuplicateFinding `json:"duplicate_decisions,omitempty"`
	LogicalDuplicateDecisions  []DecisionFinding  `json:"logical_duplicate_decisions,omitempty"`
	StaleReplayAdmissions      []DecisionFinding  `json:"stale_replay_admissions,omitempty"`
	MissingAPIReadback         []DecisionFinding  `json:"missing_api_readback,omitempty"`
	MissingMCPReadback         []DecisionFinding  `json:"missing_mcp_readback,omitempty"`
	ReadbackDisagreements      []ReadbackFinding  `json:"readback_disagreements,omitempty"`
	ReadbackTruthDisagreements []ReadbackFinding  `json:"readback_truth_disagreements,omitempty"`
}

// CaseFinding identifies a fixture case-level failure.
type CaseFinding struct {
	CaseID string `json:"case_id"`
	Reason string `json:"reason"`
}

// Key returns a stable case finding identity.
func (f CaseFinding) Key() string {
	return f.CaseID + "|" + f.Reason
}

// DecisionFinding identifies a case and admission decision.
type DecisionFinding struct {
	CaseID     string `json:"case_id"`
	DecisionID string `json:"decision_id"`
}

// Key returns a stable decision finding identity.
func (f DecisionFinding) Key() string {
	return f.CaseID + "|" + f.DecisionID
}

// CanonicalFinding identifies an unexpected canonical publication.
type CanonicalFinding struct {
	CaseID   string `json:"case_id"`
	TargetID string `json:"target_id"`
}

// Key returns a stable canonical finding identity.
func (f CanonicalFinding) Key() string {
	return f.CaseID + "|" + f.TargetID
}

// DuplicateFinding identifies a duplicate row key.
type DuplicateFinding struct {
	ID string `json:"id"`
}

// Key returns the duplicate key.
func (f DuplicateFinding) Key() string {
	return f.ID
}

// ReadbackFinding identifies one field-level API/MCP disagreement.
type ReadbackFinding struct {
	DecisionID string `json:"decision_id"`
	Field      string `json:"field"`
}

// Key returns a stable readback disagreement identity.
func (f ReadbackFinding) Key() string {
	return f.DecisionID + "|" + f.Field
}

// Pass reports whether reducer, graph, API, and MCP observations match the
// fixture intent.
func (r Report) Pass() bool {
	return len(r.MissingDecisions) == 0 &&
		len(r.StateDisagreements) == 0 &&
		len(r.MissingExplanations) == 0 &&
		len(r.MissingGraphFacts) == 0 &&
		len(r.MissingCanonicalWrites) == 0 &&
		len(r.UnexpectedGraphFacts) == 0 &&
		len(r.UnexpectedCanonicalWrites) == 0 &&
		len(r.DuplicateDecisions) == 0 &&
		len(r.LogicalDuplicateDecisions) == 0 &&
		len(r.StaleReplayAdmissions) == 0 &&
		len(r.MissingAPIReadback) == 0 &&
		len(r.MissingMCPReadback) == 0 &&
		len(r.ReadbackDisagreements) == 0 &&
		len(r.ReadbackTruthDisagreements) == 0
}

// Summary returns a stable one-line count summary for test failures.
func (r Report) Summary() string {
	return strings.Join([]string{
		fmt.Sprintf("missing_decisions=%d", len(r.MissingDecisions)),
		fmt.Sprintf("state_disagreements=%d", len(r.StateDisagreements)),
		fmt.Sprintf("missing_explanations=%d", len(r.MissingExplanations)),
		fmt.Sprintf("missing_graph_facts=%d", len(r.MissingGraphFacts)),
		fmt.Sprintf("missing_canonical_writes=%d", len(r.MissingCanonicalWrites)),
		fmt.Sprintf("unexpected_graph_facts=%d", len(r.UnexpectedGraphFacts)),
		fmt.Sprintf("unexpected_canonical_writes=%d", len(r.UnexpectedCanonicalWrites)),
		fmt.Sprintf("duplicate_decisions=%d", len(r.DuplicateDecisions)),
		fmt.Sprintf("logical_duplicate_decisions=%d", len(r.LogicalDuplicateDecisions)),
		fmt.Sprintf("stale_replay_admissions=%d", len(r.StaleReplayAdmissions)),
		fmt.Sprintf("missing_api_readback=%d", len(r.MissingAPIReadback)),
		fmt.Sprintf("missing_mcp_readback=%d", len(r.MissingMCPReadback)),
		fmt.Sprintf("readback_disagreements=%d", len(r.ReadbackDisagreements)),
		fmt.Sprintf("readback_truth_disagreements=%d", len(r.ReadbackTruthDisagreements)),
	}, " ")
}
