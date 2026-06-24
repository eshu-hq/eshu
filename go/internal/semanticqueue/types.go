// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package semanticqueue builds deterministic semantic extraction work records.
package semanticqueue

import (
	"time"

	"github.com/eshu-hq/eshu/go/internal/semanticguard"
	"github.com/eshu-hq/eshu/go/internal/semanticpolicy"
)

// ProviderState captures whether a provider can receive semantic work.
type ProviderState string

const (
	// ProviderStateReady means provider jobs may be planned.
	ProviderStateReady ProviderState = "ready"
	// ProviderStateNotConfigured means no provider is configured.
	ProviderStateNotConfigured ProviderState = "not_configured"
	// ProviderStateUnavailable means a configured provider is temporarily unavailable.
	ProviderStateUnavailable ProviderState = "unavailable"
)

// Status captures the semantic extraction queue lifecycle.
type Status string

const (
	// StatusPending means the chunk is ready for provider execution.
	StatusPending Status = "pending"
	// StatusClaimed means one worker owns the job lease.
	StatusClaimed Status = "claimed"
	// StatusRetrying means a retryable failure will reappear later.
	StatusRetrying Status = "retrying"
	// StatusSucceeded means the provider output passed response guard checks.
	StatusSucceeded Status = "succeeded"
	// StatusDeadLetter means retries are exhausted or the job is quarantined.
	StatusDeadLetter Status = "dead_letter"
	// StatusSkippedUnchanged means the current chunk fingerprint was already handled.
	StatusSkippedUnchanged Status = "skipped_unchanged"
	// StatusSkippedNoProvider means no provider was configured, so no job was created.
	StatusSkippedNoProvider Status = "skipped_no_provider"
	// StatusSkippedPolicy means policy denied semantic extraction.
	StatusSkippedPolicy Status = "skipped_policy"
	// StatusSkippedBudget means the semantic budget denied extraction.
	StatusSkippedBudget Status = "skipped_budget"
	// StatusUnsafePayload means the security guard denied provider egress.
	StatusUnsafePayload Status = "unsafe_payload"
	// StatusProviderUnavailable means a configured provider cannot run now.
	StatusProviderUnavailable Status = "provider_unavailable"
	// StatusStale means an older record no longer matches current source chunks.
	StatusStale Status = "stale"
)

const (
	// BudgetStateAllowed means budget remains available.
	BudgetStateAllowed = "allowed"
	// BudgetStateExhausted means the budget window is exhausted.
	BudgetStateExhausted = "exhausted"
)

const (
	// BudgetReasonAllowed marks an allowed budget decision.
	BudgetReasonAllowed = "allowed"
	// BudgetReasonDailyLimit marks a daily token or cost limit denial.
	BudgetReasonDailyLimit = "daily_limit"
)

const (
	// StaleReasonSourceChanged marks a prior record superseded by a new fingerprint.
	StaleReasonSourceChanged = "source_changed"
	// StaleReasonSourceDeleted marks a prior record whose source chunk disappeared.
	StaleReasonSourceDeleted = "source_deleted"
)

const (
	// FailureClassProviderUnavailable marks retryable provider unavailability.
	FailureClassProviderUnavailable = "provider_unavailable"
	// FailureClassRetryExhausted marks terminal retry exhaustion.
	FailureClassRetryExhausted = "retry_exhausted"
)

// Provider identifies the provider profile used for semantic extraction.
type Provider struct {
	State             ProviderState
	ProviderKind      string
	ProviderProfileID string
	ProfileClass      string
}

// BudgetDecision records audit-safe budget state for one chunk.
type BudgetDecision struct {
	Allowed               bool   `json:"allowed"`
	State                 string `json:"state"`
	Reason                string `json:"reason"`
	EstimatedInputTokens  int64  `json:"estimated_input_tokens"`
	EstimatedOutputTokens int64  `json:"estimated_output_tokens"`
	EstimatedCostMicros   int64  `json:"estimated_cost_micros"`
	ActualInputTokens     int64  `json:"actual_input_tokens"`
	ActualOutputTokens    int64  `json:"actual_output_tokens"`
	ActualCostMicros      int64  `json:"actual_cost_micros"`
	BudgetUnit            string `json:"budget_unit"`
	BudgetWindow          string `json:"budget_window"`
	RemainingTokens       int64  `json:"remaining_tokens"`
	RemainingCostMicros   int64  `json:"remaining_cost_micros"`
}

// SourceChunk is one preflight-approved semantic extraction candidate.
type SourceChunk struct {
	SourceID              string
	SourceClass           string
	SourceHash            string
	SourceVersion         string
	ChunkID               string
	ChunkHash             string
	NormalizedContentHash string
	PromptVersion         string
	RedactionVersion      string
	ExtractorVersion      string
	ExtractionMode        string
	Policy                PolicyDecision
	Guard                 GuardDecision
	Budget                BudgetDecision
}

// PolicyDecision is the semantic policy decision attached to a queue record.
type PolicyDecision = semanticpolicy.Decision

// GuardDecision is the security guard decision attached to a queue record.
type GuardDecision = semanticguard.Decision

// PlanRequest is the complete side-effect-free queue planning input.
type PlanRequest struct {
	ScopeID         string
	GenerationID    string
	Provider        Provider
	Chunks          []SourceChunk
	PreviousRecords []Record
	Now             time.Time
}

// Record is an audit-safe semantic extraction queue row.
type Record struct {
	JobID                string
	WorkItemID           string
	Fingerprint          string
	ScopeID              string
	GenerationID         string
	SourceClass          string
	SourceIDHash         string
	ChunkIDHash          string
	SourceHash           string
	ChunkHash            string
	SourceVersion        string
	PromptVersion        string
	RedactionVersion     string
	ExtractorVersion     string
	ExtractionMode       string
	ProviderKind         string
	ProviderProfileID    string
	ProviderProfileClass string
	PolicyID             string
	RuleID               string
	PolicyState          string
	PolicyReason         string
	GuardState           string
	GuardReason          string
	ActorClass           string
	ACLState             string
	ClassifierVersion    string
	Budget               BudgetDecision
	Status               Status
	ProviderJob          bool
	Retryable            bool
	AttemptCount         int
	Failure              Failure
	StaleReason          string
	ResponseHash         string
	CreatedAt            time.Time
	UpdatedAt            time.Time
	LastAttemptAt        *time.Time
	NextAttemptAt        *time.Time
	StaleAt              *time.Time
}

// Failure records retry-safe provider or lifecycle failure metadata.
type Failure struct {
	Class   string
	Message string
	Detail  string
}

// Plan is the deterministic result of one semantic queue planning pass.
type Plan struct {
	Jobs    []Record
	Skipped []Record
	Stale   []Record
	Summary Summary
}

// Summary captures aggregate queue status without source identifiers.
type Summary struct {
	Planned             int
	Succeeded           int
	DeadLetter          int
	Unchanged           int
	Changed             int
	Deleted             int
	NoProvider          int
	PolicyDenied        int
	BudgetDenied        int
	Unsafe              int
	ProviderUnavailable int
	Stale               int
}
