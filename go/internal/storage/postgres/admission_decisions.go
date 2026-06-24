package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	admissionDecisionDefaultLimit = 1
	admissionDecisionMaxLimit     = 500
)

const upsertAdmissionDecisionSQL = `
INSERT INTO admission_decisions (
    decision_id, domain, state, domain_state, scope_id, generation_id,
    anchor_kind, anchor_id, candidate_kind, candidate_id,
    confidence_score, confidence_bucket, confidence_basis,
    freshness_state, freshness_observed_at, freshness_cause,
    source_handles, redaction_state, redaction_reason,
    canonical_write, recommended_action, payload_version, decided_at, updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6,
    $7, $8, $9, $10,
    $11, $12, $13,
    $14, $15, $16,
    $17, $18, $19,
    $20, $21, $22, $23, $24
)
ON CONFLICT (decision_id) DO UPDATE
SET domain = EXCLUDED.domain,
    state = EXCLUDED.state,
    domain_state = EXCLUDED.domain_state,
    scope_id = EXCLUDED.scope_id,
    generation_id = EXCLUDED.generation_id,
    anchor_kind = EXCLUDED.anchor_kind,
    anchor_id = EXCLUDED.anchor_id,
    candidate_kind = EXCLUDED.candidate_kind,
    candidate_id = EXCLUDED.candidate_id,
    confidence_score = EXCLUDED.confidence_score,
    confidence_bucket = EXCLUDED.confidence_bucket,
    confidence_basis = EXCLUDED.confidence_basis,
    freshness_state = EXCLUDED.freshness_state,
    freshness_observed_at = EXCLUDED.freshness_observed_at,
    freshness_cause = EXCLUDED.freshness_cause,
    source_handles = EXCLUDED.source_handles,
    redaction_state = EXCLUDED.redaction_state,
    redaction_reason = EXCLUDED.redaction_reason,
    canonical_write = EXCLUDED.canonical_write,
    recommended_action = EXCLUDED.recommended_action,
    payload_version = EXCLUDED.payload_version,
    decided_at = EXCLUDED.decided_at,
    updated_at = EXCLUDED.updated_at
`

const upsertAdmissionDecisionEvidenceSQL = `
INSERT INTO admission_decision_evidence (
    evidence_id, decision_id, source_handle, evidence_kind, detail, created_at
) VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (evidence_id) DO UPDATE
SET decision_id = EXCLUDED.decision_id,
    source_handle = EXCLUDED.source_handle,
    evidence_kind = EXCLUDED.evidence_kind,
    detail = EXCLUDED.detail,
    created_at = EXCLUDED.created_at
`

const listAdmissionDecisionsSQL = `
SELECT decision_id, domain, state, domain_state, scope_id, generation_id,
       anchor_kind, anchor_id, candidate_kind, candidate_id,
       confidence_score, confidence_bucket, confidence_basis,
       freshness_state, freshness_observed_at, freshness_cause,
       source_handles, redaction_state, redaction_reason,
       canonical_write, recommended_action, payload_version, decided_at, updated_at
FROM admission_decisions
WHERE domain = $1
  AND scope_id = $2
  AND generation_id = $3
  AND ($4 = '' OR state = $4)
  AND ($5 = '' OR anchor_kind = $5)
  AND ($6 = '' OR anchor_id = $6)
ORDER BY updated_at DESC, decision_id ASC
LIMIT $7
`

const listAdmissionDecisionEvidenceSQL = `
SELECT evidence_id, decision_id, source_handle, evidence_kind, detail, created_at
FROM admission_decision_evidence
WHERE decision_id = $1
ORDER BY created_at ASC, evidence_id ASC
LIMIT $2
`

// AdmissionDecisionState is the closed reducer admission vocabulary shared
// across correlation domains.
type AdmissionDecisionState string

const (
	// AdmissionDecisionStateAdmitted means canonical graph/content writes are
	// allowed or already written for the decision.
	AdmissionDecisionStateAdmitted AdmissionDecisionState = "admitted"
	// AdmissionDecisionStateRejected means the candidate was intentionally
	// excluded from canonical truth.
	AdmissionDecisionStateRejected AdmissionDecisionState = "rejected"
	// AdmissionDecisionStateAmbiguous means multiple candidates or owners block
	// a single canonical write.
	AdmissionDecisionStateAmbiguous AdmissionDecisionState = "ambiguous"
	// AdmissionDecisionStateStale means the candidate lost freshness against the
	// active source generation.
	AdmissionDecisionStateStale AdmissionDecisionState = "stale"
	// AdmissionDecisionStateMissingEvidence means required inputs were absent.
	AdmissionDecisionStateMissingEvidence AdmissionDecisionState = "missing_evidence"
	// AdmissionDecisionStatePermissionHidden means source data exists but cannot
	// be shown or used for this viewer or tenant boundary.
	AdmissionDecisionStatePermissionHidden AdmissionDecisionState = "permission_hidden"
	// AdmissionDecisionStateUnsupported means the domain intentionally does not
	// support this candidate class.
	AdmissionDecisionStateUnsupported AdmissionDecisionState = "unsupported"
	// AdmissionDecisionStateUnsafe means the reducer found evidence that makes a
	// canonical write unsafe.
	AdmissionDecisionStateUnsafe AdmissionDecisionState = "unsafe"
)

// AdmissionDecisionStateValues returns the closed state vocabulary in stable
// schema order.
func AdmissionDecisionStateValues() []AdmissionDecisionState {
	return []AdmissionDecisionState{
		AdmissionDecisionStateAdmitted,
		AdmissionDecisionStateRejected,
		AdmissionDecisionStateAmbiguous,
		AdmissionDecisionStateStale,
		AdmissionDecisionStateMissingEvidence,
		AdmissionDecisionStatePermissionHidden,
		AdmissionDecisionStateUnsupported,
		AdmissionDecisionStateUnsafe,
	}
}

// AdmissionDecisionSourceHandle is a redaction-safe reference to source
// evidence rather than an embedded raw provider or endpoint payload.
type AdmissionDecisionSourceHandle struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	ScopeID string `json:"scope_id,omitempty"`
}

// AdmissionDecisionCanonicalWrite records whether a decision was eligible for
// canonical graph/content writes and why a write was skipped.
type AdmissionDecisionCanonicalWrite struct {
	Eligible      bool   `json:"eligible"`
	Written       bool   `json:"written"`
	TargetKind    string `json:"target_kind,omitempty"`
	TargetID      string `json:"target_id,omitempty"`
	SkippedReason string `json:"skipped_reason,omitempty"`
}

// AdmissionDecisionNextAction carries the operator-facing follow-up for a
// non-admitted or incomplete decision.
type AdmissionDecisionNextAction struct {
	Action string `json:"action"`
	Reason string `json:"reason,omitempty"`
	Owner  string `json:"owner,omitempty"`
}

// AdmissionDecision is one persisted correlation admission decision.
type AdmissionDecision struct {
	DecisionID          string
	Domain              string
	State               AdmissionDecisionState
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

// AdmissionDecisionEvidence is one detailed evidence row for a persisted
// admission decision.
type AdmissionDecisionEvidence struct {
	EvidenceID   string
	DecisionID   string
	SourceHandle string
	EvidenceKind string
	Detail       map[string]any
	CreatedAt    time.Time
}

// AdmissionDecisionFilter bounds admission decision reads to one reducer
// domain, scope, and generation.
type AdmissionDecisionFilter struct {
	Domain       string
	ScopeID      string
	GenerationID string
	State        *AdmissionDecisionState
	AnchorKind   string
	AnchorID     string
	Limit        int
}

// AdmissionDecisionStore persists shared reducer admission decisions and their
// evidence handles.
type AdmissionDecisionStore struct {
	db ExecQueryer
}

// NewAdmissionDecisionStore creates an admission decision store backed by db.
func NewAdmissionDecisionStore(db ExecQueryer) *AdmissionDecisionStore {
	return &AdmissionDecisionStore{db: db}
}

// EnsureSchema applies the admission decision DDL.
func (s *AdmissionDecisionStore) EnsureSchema(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, admissionDecisionSchemaSQL)
	return err
}

// UpsertDecision inserts or updates one admission decision.
func (s *AdmissionDecisionStore) UpsertDecision(ctx context.Context, d AdmissionDecision) error {
	if err := validateAdmissionDecision(d); err != nil {
		return err
	}
	sourceHandles, err := marshalAdmissionJSON(nonNilSourceHandles(d.SourceHandles), "source handles")
	if err != nil {
		return err
	}
	canonicalWrite, err := marshalAdmissionJSON(d.CanonicalWrite, "canonical write")
	if err != nil {
		return err
	}
	recommendedAction, err := marshalAdmissionJSON(d.RecommendedAction, "recommended action")
	if err != nil {
		return err
	}

	var observedAt any
	if d.FreshnessObservedAt != nil {
		observedAt = *d.FreshnessObservedAt
	}
	_, err = s.db.ExecContext(
		ctx, upsertAdmissionDecisionSQL,
		d.DecisionID,
		d.Domain,
		string(d.State),
		d.DomainState,
		d.ScopeID,
		d.GenerationID,
		d.AnchorKind,
		d.AnchorID,
		d.CandidateKind,
		d.CandidateID,
		d.ConfidenceScore,
		d.ConfidenceBucket,
		d.ConfidenceBasis,
		d.FreshnessState,
		observedAt,
		d.FreshnessCause,
		sourceHandles,
		d.RedactionState,
		d.RedactionReason,
		canonicalWrite,
		recommendedAction,
		d.PayloadVersion,
		d.DecidedAt,
		d.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert admission decision %q: %w", d.DecisionID, err)
	}
	return nil
}

// InsertEvidence inserts or updates evidence rows for admission decisions.
func (s *AdmissionDecisionStore) InsertEvidence(ctx context.Context, rows []AdmissionDecisionEvidence) error {
	if len(rows) == 0 {
		return nil
	}
	for _, row := range rows {
		detail, err := marshalAdmissionJSON(nonNilDetail(row.Detail), "evidence detail")
		if err != nil {
			return err
		}
		if _, err := s.db.ExecContext(
			ctx, upsertAdmissionDecisionEvidenceSQL,
			row.EvidenceID,
			row.DecisionID,
			row.SourceHandle,
			row.EvidenceKind,
			detail,
			row.CreatedAt,
		); err != nil {
			return fmt.Errorf("upsert admission decision evidence %q: %w", row.EvidenceID, err)
		}
	}
	return nil
}

// ListDecisions returns persisted admission decisions for one bounded domain,
// scope, and generation.
func (s *AdmissionDecisionStore) ListDecisions(ctx context.Context, f AdmissionDecisionFilter) ([]AdmissionDecision, error) {
	if err := validateAdmissionDecisionFilter(f); err != nil {
		return nil, err
	}
	state := ""
	if f.State != nil {
		state = string(*f.State)
	}
	rows, err := s.db.QueryContext(
		ctx, listAdmissionDecisionsSQL,
		f.Domain,
		f.ScopeID,
		f.GenerationID,
		state,
		f.AnchorKind,
		f.AnchorID,
		admissionDecisionLimit(f.Limit),
	)
	if err != nil {
		return nil, fmt.Errorf("query admission decisions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return scanAdmissionDecisionRows(rows)
}

// ListEvidence returns a bounded page of persisted evidence rows for one
// admission decision.
func (s *AdmissionDecisionStore) ListEvidence(
	ctx context.Context,
	decisionID string,
	limit int,
) ([]AdmissionDecisionEvidence, error) {
	rows, err := s.db.QueryContext(ctx, listAdmissionDecisionEvidenceSQL, decisionID, admissionDecisionLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("query admission decision evidence: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return scanAdmissionDecisionEvidenceRows(rows)
}

func scanAdmissionDecisionRows(rows Rows) ([]AdmissionDecision, error) {
	var result []AdmissionDecision
	for rows.Next() {
		var decision AdmissionDecision
		var state string
		var sourceHandles []byte
		var canonicalWrite []byte
		var recommendedAction []byte
		var observedAt sql.NullTime
		if err := rows.Scan(
			&decision.DecisionID,
			&decision.Domain,
			&state,
			&decision.DomainState,
			&decision.ScopeID,
			&decision.GenerationID,
			&decision.AnchorKind,
			&decision.AnchorID,
			&decision.CandidateKind,
			&decision.CandidateID,
			&decision.ConfidenceScore,
			&decision.ConfidenceBucket,
			&decision.ConfidenceBasis,
			&decision.FreshnessState,
			&observedAt,
			&decision.FreshnessCause,
			&sourceHandles,
			&decision.RedactionState,
			&decision.RedactionReason,
			&canonicalWrite,
			&recommendedAction,
			&decision.PayloadVersion,
			&decision.DecidedAt,
			&decision.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan admission decision: %w", err)
		}
		decision.State = AdmissionDecisionState(state)
		if observedAt.Valid {
			decision.FreshnessObservedAt = &observedAt.Time
		}
		if err := unmarshalAdmissionJSON(sourceHandles, &decision.SourceHandles, "source handles"); err != nil {
			return nil, err
		}
		if err := unmarshalAdmissionJSON(canonicalWrite, &decision.CanonicalWrite, "canonical write"); err != nil {
			return nil, err
		}
		if err := unmarshalAdmissionJSON(recommendedAction, &decision.RecommendedAction, "recommended action"); err != nil {
			return nil, err
		}
		result = append(result, decision)
	}
	return result, rows.Err()
}

func scanAdmissionDecisionEvidenceRows(rows Rows) ([]AdmissionDecisionEvidence, error) {
	var result []AdmissionDecisionEvidence
	for rows.Next() {
		var row AdmissionDecisionEvidence
		var detail []byte
		if err := rows.Scan(
			&row.EvidenceID,
			&row.DecisionID,
			&row.SourceHandle,
			&row.EvidenceKind,
			&detail,
			&row.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan admission decision evidence: %w", err)
		}
		if err := unmarshalAdmissionJSON(detail, &row.Detail, "evidence detail"); err != nil {
			return nil, err
		}
		row.Detail = nonNilDetail(row.Detail)
		result = append(result, row)
	}
	return result, rows.Err()
}

func validateAdmissionDecisionFilter(f AdmissionDecisionFilter) error {
	if strings.TrimSpace(f.Domain) == "" {
		return fmt.Errorf("admission decision domain is required")
	}
	if strings.TrimSpace(f.ScopeID) == "" {
		return fmt.Errorf("admission decision scope_id is required")
	}
	if strings.TrimSpace(f.GenerationID) == "" {
		return fmt.Errorf("admission decision generation_id is required")
	}
	return nil
}

func admissionDecisionLimit(limit int) int {
	if limit <= 0 {
		return admissionDecisionDefaultLimit
	}
	if limit > admissionDecisionMaxLimit {
		return admissionDecisionMaxLimit
	}
	return limit
}

func marshalAdmissionJSON(value any, label string) ([]byte, error) {
	bytes, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal admission decision %s: %w", label, err)
	}
	return bytes, nil
}

func unmarshalAdmissionJSON(bytes []byte, dest any, label string) error {
	if len(bytes) == 0 {
		return nil
	}
	if err := json.Unmarshal(bytes, dest); err != nil {
		return fmt.Errorf("unmarshal admission decision %s: %w", label, err)
	}
	return nil
}

func nonNilSourceHandles(handles []AdmissionDecisionSourceHandle) []AdmissionDecisionSourceHandle {
	if handles == nil {
		return []AdmissionDecisionSourceHandle{}
	}
	return handles
}

func nonNilDetail(detail map[string]any) map[string]any {
	if detail == nil {
		return map[string]any{}
	}
	return detail
}
