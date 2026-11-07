// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidence

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract/packet"
)

// AdmissionDecisionReadStore reads bounded correlation admission decisions.
type AdmissionDecisionReadStore interface {
	ListAdmissionDecisions(context.Context, AdmissionDecisionReadFilter) ([]AdmissionDecisionReadRow, error)
	ListAdmissionDecisionEvidence(context.Context, string, int) ([]packet.AdmissionDecisionEvidenceRow, error)
}

// AdmissionDecisionReadFilter bounds admission decision reads to one domain,
// ingestion scope, and generation, with optional state and anchor narrowing.
type AdmissionDecisionReadFilter struct {
	Domain               string
	ScopeID              string
	GenerationID         string
	State                *string
	AnchorKind           string
	AnchorID             string
	Limit                int
	Scoped               bool
	AllowedRepositoryIDs []string
	AllowedScopeIDs      []string
}

// AdmissionDecisionReadRow is one persisted correlation admission decision.
type AdmissionDecisionReadRow struct {
	DecisionID          string
	Domain              string
	State               string
	DomainState         string
	ScopeID             string
	GenerationID        string
	AnchorKind          string
	AnchorID            string
	CandidateKind       string
	CandidateID         string
	ConfidenceScore     float64
	ConfidenceBucket    string
	ConfidenceBasis     string
	FreshnessState      string
	FreshnessObservedAt *time.Time
	FreshnessCause      string
	SourceHandles       []packet.AdmissionDecisionSourceHandle
	RedactionState      string
	RedactionReason     string
	CanonicalWrite      packet.AdmissionDecisionCanonicalWrite
	RecommendedAction   packet.AdmissionDecisionNextAction
	PayloadVersion      string
	DecidedAt           time.Time
	UpdatedAt           time.Time
}

// RecommendedNextCall describes a bounded follow-up API or MCP call.
type RecommendedNextCall struct {
	Tool   string            `json:"tool,omitempty"`
	Route  string            `json:"route"`
	Reason string            `json:"reason"`
	Args   map[string]string `json:"args,omitempty"`
}
