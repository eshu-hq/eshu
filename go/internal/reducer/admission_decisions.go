// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
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

func writeAdmissionDecisions(
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

func newAdmissionDecision(
	domain Domain,
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
	decisionID := stableAdmissionDecisionID(
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

func admissionDecisionEvidence(
	decision AdmissionDecision,
	sourceHandle string,
	evidenceKind string,
	detail map[string]any,
	now time.Time,
) AdmissionDecisionEvidence {
	return AdmissionDecisionEvidence{
		EvidenceID: stableAdmissionDecisionID(
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

func stableAdmissionDecisionID(parts ...string) string {
	identity := make(map[string]any, len(parts))
	for idx, part := range parts {
		identity[fmt.Sprintf("part_%02d", idx)] = strings.TrimSpace(part)
	}
	return "admission:" + facts.StableID("admission_decision", identity)
}

func admissionConfidenceBucket(confidence float64) string {
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

func admissionNow(now func() time.Time) time.Time {
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
