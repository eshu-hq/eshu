// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// ClaimSelector identifies which collector actor is attempting to claim work.
type ClaimSelector struct {
	CollectorKind       scope.CollectorKind
	CollectorInstanceID string
	OwnerID             string
	ClaimID             string
}

// ClaimMutation carries the fenced mutation arguments for one claim epoch.
type ClaimMutation struct {
	WorkItemID    string
	ClaimID       string
	FencingToken  int64
	OwnerID       string
	ObservedAt    time.Time
	LeaseDuration time.Duration
	// Tenant boundary fields are optional for legacy shared-mode work, but
	// hosted claim-aware collectors set all four so the fact commit boundary
	// can re-check and lock the active grant before persisting source facts.
	TenantID           string
	WorkspaceID        string
	SubjectClass       string
	PolicyRevisionHash string
	FailureClass       string
	FailureMessage     string
	VisibleAt          time.Time
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
}

// CompletenessState captures one reducer-facing completion checkpoint for a
// workflow run.
type CompletenessState struct {
	RunID         string
	CollectorKind scope.CollectorKind
	Keyspace      reducer.GraphProjectionKeyspace
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
