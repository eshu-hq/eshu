// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// ClaimSelector identifies which collector actor is attempting to claim work.
type ClaimSelector struct {
	CollectorKind       scope.CollectorKind
	CollectorInstanceID string
	OwnerID             string
	ClaimID             string
	// RotationSalt seeds the fair-claim rotation draw for this call. Callers
	// that need reproducible picks (tests) pass a fixed salt; production
	// callers leave it empty and the statement draws server-side randomness
	// per head row, keeping every claim an independent shuffle.
	RotationSalt string
}

// ClaimMutation carries the fenced mutation arguments for one claim epoch.
type ClaimMutation struct {
	WorkItemID   string
	ClaimID      string
	FencingToken int64
	OwnerID      string
	// DerivedScannerImage opts into the protected source-first heartbeat path.
	// Omitting it for derived work fails closed at the generic SQL guard.
	DerivedScannerImage bool
	ObservedAt          time.Time
	LeaseDuration       time.Duration
	// Tenant boundary fields are optional for legacy shared-mode work, but
	// hosted claim-aware collectors set all four so the fact commit boundary
	// can re-check and lock the active grant before persisting source facts.
	TenantID           string
	WorkspaceID        string
	SubjectClass       string
	PolicyRevisionHash string
	FailureClass       string
	FailureMessage     string
	// FailureOperation is the bounded claim-stage or provider operation that
	// failed (for example "collect_failure"). It carries only a sanitized
	// [a-z0-9_] token: never a URL, response body, or credential. Blank
	// means the operation was not recorded (legacy rows).
	FailureOperation string
	// FailureStatusCode is the bounded provider HTTP status preserved from
	// the failure cause, or 0 when no HTTP status is known.
	FailureStatusCode int
	// FailureTerminal records whether the failure stops retries for the
	// claim. It mirrors the FailClaimTerminal (true) versus
	// FailClaimRetryable (false) route alongside the error's own terminal
	// flag, so terminal rows stay queryable without joining status.
	FailureTerminal bool
	VisibleAt       time.Time
	// Resolved* fields optionally replace a planned work-item phase identity
	// when a collector can only know the final reducer checkpoint tuple after
	// opening the source. Terraform-state work uses this to move from candidate
	// planning IDs to the real snapshot generation before run reconciliation.
	ResolvedScopeID          string
	ResolvedAcceptanceUnitID string
	ResolvedSourceRunID      string
	ResolvedGenerationID     string
}

// ClaimedWorkItem returns the currently owned work item and claim epoch.
type ClaimedWorkItem struct {
	WorkItem WorkItem
	Claim    Claim
}

// RunAdmission reports what one guarded scheduled-run admission did.
//
// The two counts are different questions and an operator needs both. Targets
// missing from EligibleTargets were dropped by the open-target guard because a
// non-terminal run already covers them, which is the benign duplicate skip that
// makes running more than one coordinator safe. Rows missing from
// InsertedWorkItems cleared that guard and were still refused by the database --
// a partial unique index covering a narrower tuple than the guard compares --
// which is planned work that never reached the queue. Collapsing the two into
// one number reports lost work as a harmless duplicate (#4586).
type RunAdmission struct {
	// EligibleTargets counts planned work items that no non-terminal run and no
	// earlier row of the same run already covered.
	EligibleTargets int
	// InsertedWorkItems counts the rows the work-item table actually accepted.
	// This is the work an operator can find in the queue.
	InsertedWorkItems int
	// AdmissionSkipped counts planned bindings the admission transaction
	// refused to prove, by skip reason and authorization domain. A skipped
	// binding earns no target row, attempt, or work item; the discovery
	// cursor still advances past it and the next cycle re-proves it from
	// scratch. Nil when every planned binding admitted.
	AdmissionSkipped map[string]map[string]int
}

// CompletenessState captures one reducer-facing completion checkpoint for a
// workflow run.
type CompletenessState struct {
	RunID         string
	CollectorKind scope.CollectorKind
	Keyspace      gpphase.Keyspace
	PhaseName     string
	Required      bool
	Status        string
	Detail        string
	ObservedAt    time.Time
	UpdatedAt     time.Time
}

// ControlStore is the durable workflow coordinator store surface.
type ControlStore interface {
	CreateRun(context.Context, Run) error
	EnqueueWorkItems(context.Context, []WorkItem) error
	ReconcileCollectorInstances(context.Context, time.Time, []DesiredCollectorInstance) error
	ListCollectorInstances(context.Context) ([]CollectorInstance, error)
	UpsertCompletenessStates(context.Context, []CompletenessState) error
	ReconcileWorkflowRuns(context.Context, time.Time) (int, error)
	ClaimNextEligible(context.Context, ClaimSelector, time.Time, time.Duration) (WorkItem, Claim, bool, error)
	HeartbeatClaim(context.Context, ClaimMutation) error
	CompleteClaim(context.Context, ClaimMutation) error
	ReleaseClaim(context.Context, ClaimMutation) error
	FailClaimRetryable(context.Context, ClaimMutation) error
	FailClaimTerminal(context.Context, ClaimMutation) error
	ReapExpiredClaims(context.Context, time.Time, int, time.Duration) ([]Claim, error)
}
