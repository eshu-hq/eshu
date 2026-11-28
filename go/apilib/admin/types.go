// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package admin is the slim proof-read extraction of the core admin-handler
// family (Refs eshu-hq/eshu-api#456): the seven bounded inspection reads,
// the static tuning report, and the work-items query. It is stdlib-only: it
// never imports internal/query/admin or querycontract, so the satellite can
// vendor it without the core handler tree.
package admin

import (
	"context"
	"time"
)

// ReadStore provides bounded inspection without admin mutation methods. It
// mirrors the core admin ReadStore method set so a core store satisfies it
// once the core row types alias these.
type ReadStore interface {
	ListWorkItems(ctx context.Context, f WorkItemFilter) ([]WorkItem, error)
	ListDeadLetterWorkItems(ctx context.Context, f DeadLetterListFilter) ([]DeadLetterWorkItem, error)
	ListReducerInputInvalidFacts(ctx context.Context, f InputInvalidFactListFilter) ([]InputInvalidFact, error)
	ListChangedSincePoisonedLinks(ctx context.Context, f ChangedSincePoisonedLinkFilter) ([]ChangedSincePoisonedLink, error)
	ListReplayEvents(ctx context.Context, f ReplayEventFilter) ([]ReplayEvent, error)
	ListDecisions(ctx context.Context, f DecisionQueryFilter) ([]DecisionRow, error)
	ListEvidence(ctx context.Context, decisionID string) ([]EvidenceRow, error)
}

// WorkItem is an admin-friendly view of a fact_work_items row.
type WorkItem struct {
	WorkItemID   string `json:"work_item_id"`
	ScopeID      string `json:"scope_id"`
	GenerationID string `json:"generation_id"`
	Stage        string `json:"stage"`
	Domain       string `json:"domain"`
	Status       string `json:"status"`
	AttemptCount int    `json:"attempt_count"`
	// ReplayAttemptBaseline is the attempt_count a projector row had when an
	// operator replay last moved it back to pending, or 0 when it was never
	// replayed (#11182). The projection retry budget counts claims after it:
	// attempt_count - admission_wait_attempts - replay_attempt_baseline.
	ReplayAttemptBaseline int     `json:"replay_attempt_baseline"`
	LeaseOwner            *string `json:"lease_owner"`
	FailureClass          *string `json:"failure_class"`
	FailureMessage        *string `json:"failure_message"`
	OperatorNote          *string `json:"operator_note"`
	// PriorFailure is the failure the row carried before a supersede, a
	// stale-scope reclaim or an operator note rewrote its failure evidence
	// (#7320, #7388, #7385). Nil when
	// failure_details carries none. Its details text is not exposed.
	PriorFailure *PriorFailure `json:"prior_failure,omitempty"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
	VisibleAt    *time.Time    `json:"visible_at"`
}

// PriorFailure is the kept failure evidence of a work item: its status, class
// and message before the overwrite, and when the row was last updated then.
type PriorFailure struct {
	Status         string `json:"status,omitempty"`
	FailureClass   string `json:"failure_class,omitempty"`
	FailureMessage string `json:"failure_message,omitempty"`
	UpdatedAt      string `json:"updated_at,omitempty"`
}

// DeadLetterWorkItem is a bounded operator-facing view of one durable
// fact_work_items dead-letter row.
type DeadLetterWorkItem struct {
	WorkItemID    string     `json:"work_item_id"`
	ScopeID       string     `json:"scope_id"`
	GenerationID  string     `json:"generation_id"`
	Stage         string     `json:"stage"`
	Domain        string     `json:"domain"`
	CollectorKind string     `json:"collector_kind"`
	AttemptCount  int        `json:"attempt_count"`
	FailureClass  *string    `json:"failure_class"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	VisibleAt     *time.Time `json:"visible_at"`
}

// InputInvalidFact is a bounded operator-facing view of one durable
// reducer_input_invalid_facts row (issue #4630): a fact the reducer
// quarantined during typed-payload decode because a required field was
// missing or null.
type InputInvalidFact struct {
	FactID       string    `json:"fact_id"`
	FactKind     string    `json:"fact_kind"`
	MissingField string    `json:"missing_field"`
	FailureClass string    `json:"failure_class"`
	Domain       string    `json:"domain"`
	ScopeID      string    `json:"scope_id"`
	GenerationID string    `json:"generation_id"`
	DecidedAt    time.Time `json:"decided_at"`
}

// InputInvalidFactListFilter constrains bounded reducer_input_invalid_facts
// read queries. ScopeID and GenerationID are required: the table is always
// read scoped to one ingestion scope generation (mirrors
// AdmissionDecisionReadFilter and DeadLetterListFilter's scope requirement).
// AllowedRepositoryIDs and AllowedScopeIDs carry a scoped token's grants so
// the store query can authorize the requested ScopeID against
// ingestion_scopes (mirroring DeadLetterListFilter): a repository-scoped
// token that grants a repository but not the raw ingestion scope_id must
// still be authorized when the requested ScopeID belongs to that repository
// (codex review on PR #5252, issue #4630).
type InputInvalidFactListFilter struct {
	ScopeID              string
	GenerationID         string
	Domain               string
	FactKind             string
	AllowedRepositoryIDs []string
	AllowedScopeIDs      []string
	Limit                int
	Timeout              time.Duration
}

// ReplayEvent is an admin-friendly view of a fact_replay_events row.
type ReplayEvent struct {
	ReplayEventID string    `json:"replay_event_id"`
	WorkItemID    string    `json:"work_item_id"`
	ScopeID       string    `json:"scope_id"`
	GenerationID  string    `json:"generation_id"`
	FailureClass  *string   `json:"failure_class"`
	OperatorNote  *string   `json:"operator_note"`
	CreatedAt     time.Time `json:"created_at"`
	// ReplayAttemptBaseline is the baseline the replay set on a projector
	// row, and PriorAdmissionWaitAttempts the admission waits the row had
	// spent before the replay reset them (#11182). Both are nil for reducer
	// rows and for events written before migration 334.
	ReplayAttemptBaseline      *int64 `json:"replay_attempt_baseline"`
	PriorAdmissionWaitAttempts *int64 `json:"prior_admission_wait_attempts"`
}

// WorkItemFilter constrains admin work-item queries.
type WorkItemFilter struct {
	Statuses     []string
	ScopeID      string
	Stage        string
	FailureClass string
	Limit        int
}

// DeadLetterListFilter constrains bounded dead-letter read queries.
type DeadLetterListFilter struct {
	FailureClass         string
	Domain               string
	ScopeID              string
	CollectorKind        string
	UpdatedAfter         *time.Time
	UpdatedBefore        *time.Time
	AllowedRepositoryIDs []string
	AllowedScopeIDs      []string
	Limit                int
	Timeout              time.Duration
}

// ReplayEventFilter constrains replay-event audit queries.
type ReplayEventFilter struct {
	ScopeID      string
	WorkItemID   string
	FailureClass string
	Limit        int
}

// DecisionQueryFilter constrains projection-decision queries.
type DecisionQueryFilter struct {
	RepositoryID    string
	SourceRunID     string
	DecisionType    *string
	IncludeEvidence bool
	Limit           int
}

// DecisionRow is a query-layer view of a projection decision row,
// avoiding a direct dependency on the projector package.
type DecisionRow struct {
	DecisionID        string
	DecisionType      string
	RepositoryID      string
	SourceRunID       string
	WorkItemID        string
	Subject           string
	ConfidenceScore   float64
	ConfidenceReason  string
	ProvenanceSummary map[string]any
	CreatedAt         time.Time
}

// EvidenceRow is a query-layer view of a projection decision evidence row.
type EvidenceRow struct {
	EvidenceID   string
	DecisionID   string
	FactID       *string
	EvidenceKind string
	Detail       map[string]any
	CreatedAt    time.Time
}

// ChangedSincePoisonedLink is a bounded operator-facing view of one durable
// changed_since_scope_cursor row that is either poisoned (a link that hit its
// counting-failure limit, BreakLinkPoisoned) or retrying (a counted failure
// is pending on the scope's head activation, backing off toward its next
// attempt).
type ChangedSincePoisonedLink struct {
	ScopeID string `json:"scope_id"`
	// Status is poisoned or retrying.
	Status string `json:"status"`
	// ActivationSeq is the head activation the scope is stuck on: the
	// poisoned activation when Status is poisoned, otherwise the activation
	// currently backing off.
	ActivationSeq    int64      `json:"activation_seq"`
	AttemptCount     int        `json:"attempt_count"`
	LastFailureClass *string    `json:"last_failure_class,omitempty"`
	NextAttemptAt    *time.Time `json:"next_attempt_at,omitempty"`
	PoisonedAt       *time.Time `json:"poisoned_at,omitempty"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

// ChangedSincePoisonedLinkFilter constrains the bounded, deterministic
// changed-since poisoned/retrying link read (#7290). With no ScopeID or
// Cursor it is a fleet-wide page ordered by scope_id; Cursor carries the
// last scope_id of a prior page for forward keyset pagination.
type ChangedSincePoisonedLinkFilter struct {
	// Status narrows to "poisoned" or "retrying"; empty returns both.
	Status               string
	ScopeID              string
	Cursor               string
	AllowedRepositoryIDs []string
	AllowedScopeIDs      []string
	Limit                int
	Timeout              time.Duration
}
