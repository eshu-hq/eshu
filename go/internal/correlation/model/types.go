package model

import (
	"fmt"
	"strings"
)

// CandidateState describes the admission lifecycle of one correlation result.
type CandidateState string

const (
	// CandidateStateRejected means the evidence group was evaluated and rejected.
	CandidateStateRejected CandidateState = "rejected"
	// CandidateStateProvisional means the evidence group is valid but not yet admitted.
	CandidateStateProvisional CandidateState = "provisional"
	// CandidateStateAdmitted means the evidence group met admission requirements.
	CandidateStateAdmitted CandidateState = "admitted"
)

// RejectionReason records why a candidate stayed queryable but was not admitted.
type RejectionReason string

const (
	// RejectionReasonLowConfidence marks candidates that missed the confidence gate.
	RejectionReasonLowConfidence RejectionReason = "low_confidence"
	// RejectionReasonStructuralMismatch marks candidates that missed structural evidence requirements.
	RejectionReasonStructuralMismatch RejectionReason = "structural_mismatch"
	// RejectionReasonLostTieBreak marks candidates that lost deterministic winner selection.
	RejectionReasonLostTieBreak RejectionReason = "lost_tie_break"
)

// EvidenceAtom is one normalized evidence record attached to a candidate.
type EvidenceAtom struct {
	ID           string
	SourceSystem string
	EvidenceType string
	ScopeID      string
	Key          string
	Value        string
	Confidence   float64
}

// Candidate describes one correlation candidate before canonical materialization.
type Candidate struct {
	ID               string
	Kind             string
	CorrelationKey   string
	Confidence       float64
	State            CandidateState
	Evidence         []EvidenceAtom
	RejectionReasons []RejectionReason
}

// Validate checks that the candidate state is known.
func (s CandidateState) Validate() error {
	switch s {
	case CandidateStateRejected, CandidateStateProvisional, CandidateStateAdmitted:
		return nil
	default:
		return fmt.Errorf("unknown candidate state %q", s)
	}
}

// Validate checks the minimal candidate contract.
func (c Candidate) Validate() error {
	if strings.TrimSpace(c.ID) == "" {
		return fmt.Errorf("candidate id must not be blank")
	}
	if strings.TrimSpace(c.Kind) == "" {
		return fmt.Errorf("candidate kind must not be blank")
	}
	if strings.TrimSpace(c.CorrelationKey) == "" {
		return fmt.Errorf("candidate correlation key must not be blank")
	}
	if err := c.State.Validate(); err != nil {
		return err
	}
	if c.Confidence < 0 || c.Confidence > 1 {
		return fmt.Errorf("candidate confidence must be within [0,1]")
	}
	for _, evidence := range c.Evidence {
		if err := evidence.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// Validate checks that the evidence atom can participate in correlation.
func (e EvidenceAtom) Validate() error {
	if strings.TrimSpace(e.ID) == "" {
		return fmt.Errorf("evidence id must not be blank")
	}
	if strings.TrimSpace(e.SourceSystem) == "" {
		return fmt.Errorf("evidence source system must not be blank")
	}
	if strings.TrimSpace(e.EvidenceType) == "" {
		return fmt.Errorf("evidence type must not be blank")
	}
	if strings.TrimSpace(e.ScopeID) == "" {
		return fmt.Errorf("evidence scope id must not be blank")
	}
	if strings.TrimSpace(e.Key) == "" {
		return fmt.Errorf("evidence key must not be blank")
	}
	if e.Confidence < 0 || e.Confidence > 1 {
		return fmt.Errorf("evidence confidence must be within [0,1]")
	}
	return nil
}
