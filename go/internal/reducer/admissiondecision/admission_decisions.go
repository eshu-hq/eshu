// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package admissiondecision holds the shared reducer admission-decision
// vocabulary and helpers (issue #6061). See doc.go for the package contract.
package admissiondecision

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

const admissionDecisionPayloadVersion = "v1"

// AdmissionState is the reducer-local mirror of the shared admission decision
// vocabulary persisted by the data plane.
type AdmissionState string

const (
	// AdmissionStateAdmitted means canonical graph or content truth was eligible
	// for publication for this decision.
	AdmissionStateAdmitted AdmissionState = "admitted"
	// AdmissionStateRejected means the reducer intentionally excluded the
	// candidate from canonical truth.
	AdmissionStateRejected AdmissionState = "rejected"
	// AdmissionStateAmbiguous means multiple candidates blocked a single
	// canonical write.
	AdmissionStateAmbiguous AdmissionState = "ambiguous"
	// AdmissionStateStale means the candidate was superseded by fresher source
	// evidence.
	AdmissionStateStale AdmissionState = "stale"
	// AdmissionStateMissingEvidence means required evidence was absent.
	AdmissionStateMissingEvidence AdmissionState = "missing_evidence"
	// AdmissionStatePermissionHidden means source data exists but cannot be used
	// for this tenant or viewer boundary.
	AdmissionStatePermissionHidden AdmissionState = "permission_hidden"
	// AdmissionStateUnsupported means the domain intentionally does not support
	// the candidate class.
	AdmissionStateUnsupported AdmissionState = "unsupported"
	// AdmissionStateUnsafe means the reducer found evidence that makes a
	// canonical write unsafe.
	AdmissionStateUnsafe AdmissionState = "unsafe"
)

// AdmissionDecisionSourceHandle is a redaction-safe reference to source
// evidence rather than embedded raw provider payload.
type AdmissionDecisionSourceHandle struct {
	Kind    string
	ID      string
	ScopeID string
}

// AdmissionCanonicalWrite records whether a decision was eligible for and
// written to canonical graph or content truth.
type AdmissionCanonicalWrite struct {
	Eligible      bool
	Written       bool
	TargetKind    string
	TargetID      string
	SkippedReason string
}

// AdmissionNextAction carries the operator-facing follow-up for incomplete
// decisions.
type AdmissionNextAction struct {
	Action string
	Reason string
	Owner  string
}

// AdmissionDecision is one shared reducer admission decision.
type AdmissionDecision struct {
	DecisionID          string
	Domain              string
	State               AdmissionState
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
	CanonicalWrite      AdmissionCanonicalWrite
	RecommendedAction   AdmissionNextAction
	PayloadVersion      string
	DecidedAt           time.Time
	UpdatedAt           time.Time
}

// AdmissionDecisionEvidence is one bounded evidence row for a shared admission
// decision.
type AdmissionDecisionEvidence struct {
	EvidenceID   string
	DecisionID   string
	SourceHandle string
	EvidenceKind string
	Detail       map[string]any
	CreatedAt    time.Time
}

// AdmissionDecisionWrite groups one shared decision with its bounded evidence
// rows so writers can upsert both idempotently.
type AdmissionDecisionWrite struct {
	Decision AdmissionDecision
	Evidence []AdmissionDecisionEvidence
}

// AdmissionDecisionWriter persists shared reducer admission decisions.
type AdmissionDecisionWriter interface {
	WriteAdmissionDecisions(context.Context, []AdmissionDecisionWrite) error
}

// WriteAdmissionDecisions persists decisions through writer, no-op when the
// writer is nil or decisions is empty so callers can wire an optional
// AdmissionDecisionWriter without a nil check at every call site.
func WriteAdmissionDecisions(
	ctx context.Context,
	writer AdmissionDecisionWriter,
	decisions []AdmissionDecisionWrite,
) error {
	if writer == nil || len(decisions) == 0 {
		return nil
	}
	if err := writer.WriteAdmissionDecisions(ctx, decisions); err != nil {
		return fmt.Errorf("write admission decisions: %w", err)
	}
	return nil
}

// NewAdmissionDecision builds one AdmissionDecision with a stable decision id
// derived from domain, scope, generation, anchor, and candidate identity.
func NewAdmissionDecision(
	domain reducercontract.Domain,
	state AdmissionState,
	domainState string,
	scopeID string,
	generationID string,
	anchorKind string,
	anchorID string,
	candidateKind string,
	candidateID string,
	now time.Time,
) AdmissionDecision {
	decisionID := StableAdmissionDecisionID(
		string(domain),
		scopeID,
		generationID,
		anchorKind,
		anchorID,
		candidateKind,
		candidateID,
		domainState,
	)
	return AdmissionDecision{
		DecisionID:     decisionID,
		Domain:         string(domain),
		State:          state,
		DomainState:    strings.TrimSpace(domainState),
		ScopeID:        strings.TrimSpace(scopeID),
		GenerationID:   strings.TrimSpace(generationID),
		AnchorKind:     strings.TrimSpace(anchorKind),
		AnchorID:       strings.TrimSpace(anchorID),
		CandidateKind:  strings.TrimSpace(candidateKind),
		CandidateID:    strings.TrimSpace(candidateID),
		FreshnessState: "current",
		RedactionState: "redacted",
		PayloadVersion: admissionDecisionPayloadVersion,
		DecidedAt:      now,
		UpdatedAt:      now,
	}
}

// NewAdmissionDecisionEvidence builds one bounded evidence row for decision.
// Named New-prefixed rather than a bare AdmissionDecisionEvidence to avoid
// colliding with the AdmissionDecisionEvidence type of the same name once
// exported (issue #6061).
func NewAdmissionDecisionEvidence(
	decision AdmissionDecision,
	sourceHandle string,
	evidenceKind string,
	detail map[string]any,
	now time.Time,
) AdmissionDecisionEvidence {
	return AdmissionDecisionEvidence{
		EvidenceID: StableAdmissionDecisionID(
			decision.DecisionID,
			sourceHandle,
			evidenceKind,
		),
		DecisionID:   decision.DecisionID,
		SourceHandle: strings.TrimSpace(sourceHandle),
		EvidenceKind: strings.TrimSpace(evidenceKind),
		Detail:       nonNilAdmissionDetail(detail),
		CreatedAt:    now,
	}
}

// StableAdmissionDecisionID derives a stable, prefixed identity hash from
// parts. Callers use it for both decision and evidence identity.
func StableAdmissionDecisionID(parts ...string) string {
	identity := make(map[string]any, len(parts))
	for idx, part := range parts {
		identity[fmt.Sprintf("part_%02d", idx)] = strings.TrimSpace(part)
	}
	return "admission:" + facts.StableID("admission_decision", identity)
}

// AdmissionConfidenceBucket buckets a raw confidence score into the
// coarse-grained "high"/"medium"/"low"/"unknown" vocabulary the read model
// exposes.
func AdmissionConfidenceBucket(confidence float64) string {
	switch {
	case confidence >= 0.90:
		return "high"
	case confidence >= 0.70:
		return "medium"
	case confidence > 0:
		return "low"
	default:
		return "unknown"
	}
}

// AdmissionNow resolves the timestamp an admission decision or correlation
// row is stamped with. It is the seam callers use to inject a deterministic
// clock in tests: pass a handler's own optional now field (e.g.
// AdmissionDecisionNow), and AdmissionNow falls back to time.Now().UTC() when
// that field is nil. AdmissionNow itself holds no state -- it is a pure
// resolver, not a package-level clock var -- so callers remain free to pass
// distinct clocks (or nil) per call site.
func AdmissionNow(now func() time.Time) time.Time {
	if now != nil {
		return now().UTC()
	}
	return time.Now().UTC()
}

func nonNilAdmissionDetail(detail map[string]any) map[string]any {
	if detail != nil {
		return detail
	}
	return map[string]any{}
}
