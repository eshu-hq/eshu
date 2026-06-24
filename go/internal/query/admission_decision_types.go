// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"time"
)

// AdmissionDecisionReadStore reads bounded correlation admission decisions.
type AdmissionDecisionReadStore interface {
	ListAdmissionDecisions(context.Context, AdmissionDecisionReadFilter) ([]AdmissionDecisionReadRow, error)
	ListAdmissionDecisionEvidence(context.Context, string, int) ([]AdmissionDecisionEvidenceRow, error)
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

// AdmissionDecisionSourceHandle is a redaction-safe reference to source
// evidence rather than embedded raw provider payloads.
type AdmissionDecisionSourceHandle struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	ScopeID string `json:"scope_id,omitempty"`
}

// AdmissionDecisionCanonicalWrite records whether canonical graph/content truth
// was eligible and written for an admission decision.
type AdmissionDecisionCanonicalWrite struct {
	Eligible      bool   `json:"eligible"`
	Written       bool   `json:"written"`
	TargetKind    string `json:"target_kind,omitempty"`
	TargetID      string `json:"target_id,omitempty"`
	SkippedReason string `json:"skipped_reason,omitempty"`
}

// AdmissionDecisionNextAction carries the operator-facing follow-up for a
// non-admitted or incomplete admission decision.
type AdmissionDecisionNextAction struct {
	Action string `json:"action"`
	Reason string `json:"reason,omitempty"`
	Owner  string `json:"owner,omitempty"`
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
	SourceHandles       []AdmissionDecisionSourceHandle
	RedactionState      string
	RedactionReason     string
	CanonicalWrite      AdmissionDecisionCanonicalWrite
	RecommendedAction   AdmissionDecisionNextAction
	PayloadVersion      string
	DecidedAt           time.Time
	UpdatedAt           time.Time
}

// AdmissionDecisionEvidenceRow is one detailed evidence row for an admission
// decision.
type AdmissionDecisionEvidenceRow struct {
	EvidenceID   string         `json:"evidence_id"`
	DecisionID   string         `json:"decision_id"`
	SourceHandle string         `json:"source_handle"`
	EvidenceKind string         `json:"evidence_kind"`
	Detail       map[string]any `json:"detail"`
	CreatedAt    time.Time      `json:"created_at"`
}

// AdmissionDecisionResult is the HTTP/MCP read shape for one admission
// decision.
type AdmissionDecisionResult struct {
	DecisionID          string                          `json:"decision_id"`
	Domain              string                          `json:"domain"`
	State               string                          `json:"state"`
	DomainState         string                          `json:"domain_state,omitempty"`
	ScopeID             string                          `json:"scope_id"`
	GenerationID        string                          `json:"generation_id"`
	AnchorKind          string                          `json:"anchor_kind"`
	AnchorID            string                          `json:"anchor_id"`
	CandidateKind       string                          `json:"candidate_kind,omitempty"`
	CandidateID         string                          `json:"candidate_id,omitempty"`
	ConfidenceScore     float64                         `json:"confidence_score,omitempty"`
	ConfidenceBucket    string                          `json:"confidence_bucket,omitempty"`
	ConfidenceBasis     string                          `json:"confidence_basis,omitempty"`
	FreshnessState      string                          `json:"freshness_state,omitempty"`
	FreshnessObservedAt *time.Time                      `json:"freshness_observed_at,omitempty"`
	FreshnessCause      string                          `json:"freshness_cause,omitempty"`
	SourceHandles       []AdmissionDecisionSourceHandle `json:"source_handles"`
	RedactionState      string                          `json:"redaction_state,omitempty"`
	RedactionReason     string                          `json:"redaction_reason,omitempty"`
	CanonicalWrite      AdmissionDecisionCanonicalWrite `json:"canonical_write"`
	RecommendedAction   AdmissionDecisionNextAction     `json:"recommended_action"`
	PayloadVersion      string                          `json:"payload_version,omitempty"`
	DecidedAt           time.Time                       `json:"decided_at"`
	UpdatedAt           time.Time                       `json:"updated_at"`
	Evidence            []AdmissionDecisionEvidenceRow  `json:"evidence,omitempty"`
	EvidenceLimit       int                             `json:"evidence_limit,omitempty"`
	EvidenceTruncated   *bool                           `json:"evidence_truncated,omitempty"`
}

// RecommendedNextCall describes a bounded follow-up API or MCP call.
type RecommendedNextCall struct {
	Tool   string            `json:"tool,omitempty"`
	Route  string            `json:"route"`
	Reason string            `json:"reason"`
	Args   map[string]string `json:"args,omitempty"`
}
