// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parity

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// ErrPinNotSupported reports a commit pin on a backend that knows no pins.
// The in-tree store has no commit-pin column; only the satellite honors
// ValidateAndPinClaimForCommit. Scenarios that need a pin record this code
// on the in-tree leg instead of failing it.
var ErrPinNotSupported = errors.New("parity backend has no commit pin")

// Backend is one side of the differential harness: either the in-tree
// ControlStore or the satellite claim plane over HTTP. Method sets match
// workflow.ControlStore plus the commit pin and the instance read both
// sides serve.
type Backend interface {
	Name() string
	CreateRun(context.Context, workflow.Run) error
	EnqueueWorkItems(context.Context, []workflow.WorkItem) error
	ReconcileCollectorInstances(context.Context, time.Time, []workflow.DesiredCollectorInstance) error
	UpsertCompletenessStates(context.Context, []workflow.CompletenessState) error
	ReconcileWorkflowRuns(context.Context, time.Time) (int, error)
	ClaimNextEligible(context.Context, workflow.ClaimSelector, time.Time, time.Duration) (workflow.WorkItem, workflow.Claim, bool, error)
	HeartbeatClaim(context.Context, workflow.ClaimMutation) error
	CompleteClaim(context.Context, workflow.ClaimMutation) error
	ReleaseClaim(context.Context, workflow.ClaimMutation) error
	FailClaimRetryable(context.Context, workflow.ClaimMutation) error
	FailClaimTerminal(context.Context, workflow.ClaimMutation) error
	ReapExpiredClaims(context.Context, time.Time, int, time.Duration) ([]workflow.Claim, error)
	PinForCommit(ctx context.Context, claimID string, fencingToken int64, scopeID, sourceRunID, generationID string, commitDeadline time.Time) (string, error)
	ListCollectorInstances(context.Context) ([]workflow.CollectorInstance, error)
}

// Code is the comparable outcome class of one backend call. Message text
// is recorded on mismatch but never compared: only the code must agree.
type Code string

const (
	CodeOK             Code = "ok"
	CodeRejected       Code = "rejected"
	CodePaced          Code = "paced"
	CodePinUnsupported Code = "pin_unsupported"
	CodeFault          Code = "fault"
)

// Classify maps a backend call error to its comparable code. Fenced-away
// epochs collapse to CodeRejected on both legs: the in-tree store returns
// workflow.ErrClaimRejected and the HTTP leg maps stale_fence,
// lease_expired, and claim_conflict envelopes to the same sentinel.
func Classify(err error) Code {
	if err == nil {
		return CodeOK
	}
	if errors.Is(err, workflow.ErrClaimRejected) {
		return CodeRejected
	}
	var paced *workflow.ClaimPacedError
	if errors.As(err, &paced) {
		return CodePaced
	}
	if errors.Is(err, ErrPinNotSupported) {
		return CodePinUnsupported
	}
	return CodeFault
}

// wrapLegErr carries a delegate error up the harness chain with leg and
// operation context. Wrapping preserves errors.Is and errors.As, so
// Classify still maps the underlying sentinel.
func wrapLegErr(leg, op string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("parity %s %s: %w", leg, op, err)
}

// inTreeBackend drives the in-tree WorkflowControlStore directly.
type inTreeBackend struct {
	store *postgres.WorkflowControlStore
}

func (b *inTreeBackend) Name() string { return "in-tree" }

func (b *inTreeBackend) CreateRun(ctx context.Context, run workflow.Run) error {
	return wrapLegErr(b.Name(), "CreateRun", b.store.CreateRun(ctx, run))
}

func (b *inTreeBackend) EnqueueWorkItems(ctx context.Context, items []workflow.WorkItem) error {
	return wrapLegErr(b.Name(), "EnqueueWorkItems", b.store.EnqueueWorkItems(ctx, items))
}

func (b *inTreeBackend) ReconcileCollectorInstances(ctx context.Context, observedAt time.Time, desired []workflow.DesiredCollectorInstance) error {
	return wrapLegErr(b.Name(), "ReconcileCollectorInstances",
		b.store.ReconcileCollectorInstances(ctx, observedAt, desired))
}

func (b *inTreeBackend) UpsertCompletenessStates(ctx context.Context, states []workflow.CompletenessState) error {
	return wrapLegErr(b.Name(), "UpsertCompletenessStates", b.store.UpsertCompletenessStates(ctx, states))
}

func (b *inTreeBackend) ReconcileWorkflowRuns(ctx context.Context, observedAt time.Time) (int, error) {
	transitioned, err := b.store.ReconcileWorkflowRuns(ctx, observedAt)
	return transitioned, wrapLegErr(b.Name(), "ReconcileWorkflowRuns", err)
}

func (b *inTreeBackend) ClaimNextEligible(ctx context.Context, selector workflow.ClaimSelector, now time.Time, lease time.Duration) (workflow.WorkItem, workflow.Claim, bool, error) {
	item, claim, issued, err := b.store.ClaimNextEligible(ctx, selector, now, lease)
	return item, claim, issued, wrapLegErr(b.Name(), "ClaimNextEligible", err)
}

func (b *inTreeBackend) HeartbeatClaim(ctx context.Context, mutation workflow.ClaimMutation) error {
	return wrapLegErr(b.Name(), "HeartbeatClaim", b.store.HeartbeatClaim(ctx, mutation))
}

func (b *inTreeBackend) CompleteClaim(ctx context.Context, mutation workflow.ClaimMutation) error {
	return wrapLegErr(b.Name(), "CompleteClaim", b.store.CompleteClaim(ctx, mutation))
}

func (b *inTreeBackend) ReleaseClaim(ctx context.Context, mutation workflow.ClaimMutation) error {
	return wrapLegErr(b.Name(), "ReleaseClaim", b.store.ReleaseClaim(ctx, mutation))
}

func (b *inTreeBackend) FailClaimRetryable(ctx context.Context, mutation workflow.ClaimMutation) error {
	return wrapLegErr(b.Name(), "FailClaimRetryable", b.store.FailClaimRetryable(ctx, mutation))
}

func (b *inTreeBackend) FailClaimTerminal(ctx context.Context, mutation workflow.ClaimMutation) error {
	return wrapLegErr(b.Name(), "FailClaimTerminal", b.store.FailClaimTerminal(ctx, mutation))
}

func (b *inTreeBackend) ReapExpiredClaims(ctx context.Context, asOf time.Time, limit int, delay time.Duration) ([]workflow.Claim, error) {
	claims, err := b.store.ReapExpiredClaims(ctx, asOf, limit, delay)
	return claims, wrapLegErr(b.Name(), "ReapExpiredClaims", err)
}

func (b *inTreeBackend) PinForCommit(context.Context, string, int64, string, string, string, time.Time) (string, error) {
	return "", fmt.Errorf("in-tree pin: %w", ErrPinNotSupported)
}

func (b *inTreeBackend) ListCollectorInstances(ctx context.Context) ([]workflow.CollectorInstance, error) {
	instances, err := b.store.ListCollectorInstances(ctx)
	return instances, wrapLegErr(b.Name(), "ListCollectorInstances", err)
}

// openInTreeBackend opens the in-tree store over a fresh isolated schema
// of the database named by dsn. The schema is dropped when the test ends,
// so reruns never see another scenario's rows.
func openInTreeBackend(t *testing.T, dsn string) Backend {
	t.Helper()
	if strings.TrimSpace(dsn) == "" {
		t.Skip("PARITY_INTREE_DSN is not set; skipping in-tree parity leg")
	}
	db := postgresproof.OpenIsolatedSchema(t, dsn, "parity", func(ctx context.Context, database *sql.DB) error {
		// Full bootstrap first, then the workflow schema: claim mutations
		// join tenant_scope_grants, which the workflow-only schema does
		// not create. This mirrors openWorkflowControlIntegrationStore.
		if err := postgres.ApplyBootstrap(ctx, postgres.SQLDB{DB: database}); err != nil {
			return fmt.Errorf("apply bootstrap schema: %w", err)
		}
		store := postgres.NewWorkflowControlStore(postgres.SQLDB{DB: database})
		if err := store.EnsureSchema(ctx); err != nil {
			return fmt.Errorf("ensure workflow schema: %w", err)
		}
		return nil
	})
	store := postgres.NewWorkflowControlStore(postgres.SQLDB{DB: db})
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("ensure workflow schema: %v", err)
	}
	return &inTreeBackend{store: store}
}

// satelliteBackend drives the satellite claim plane over HTTP through the
// in-tree versioned client: the same client production cutover would use.
type satelliteBackend struct {
	client *workflow.ClaimPlaneClient
}

func (b *satelliteBackend) Name() string { return "satellite" }

func (b *satelliteBackend) CreateRun(ctx context.Context, run workflow.Run) error {
	return wrapLegErr(b.Name(), "CreateRun", b.client.CreateRun(ctx, run))
}

func (b *satelliteBackend) EnqueueWorkItems(ctx context.Context, items []workflow.WorkItem) error {
	return wrapLegErr(b.Name(), "EnqueueWorkItems", b.client.EnqueueWorkItems(ctx, items))
}

func (b *satelliteBackend) ReconcileCollectorInstances(ctx context.Context, observedAt time.Time, desired []workflow.DesiredCollectorInstance) error {
	return wrapLegErr(b.Name(), "ReconcileCollectorInstances",
		b.client.ReconcileCollectorInstances(ctx, observedAt, desired))
}

func (b *satelliteBackend) UpsertCompletenessStates(ctx context.Context, states []workflow.CompletenessState) error {
	return wrapLegErr(b.Name(), "UpsertCompletenessStates", b.client.UpsertCompletenessStates(ctx, states))
}

func (b *satelliteBackend) ReconcileWorkflowRuns(ctx context.Context, observedAt time.Time) (int, error) {
	transitioned, err := b.client.ReconcileWorkflowRuns(ctx, observedAt)
	return transitioned, wrapLegErr(b.Name(), "ReconcileWorkflowRuns", err)
}

func (b *satelliteBackend) ClaimNextEligible(ctx context.Context, selector workflow.ClaimSelector, now time.Time, lease time.Duration) (workflow.WorkItem, workflow.Claim, bool, error) {
	item, claim, issued, err := b.client.ClaimNextEligible(ctx, selector, now, lease)
	return item, claim, issued, wrapLegErr(b.Name(), "ClaimNextEligible", err)
}

func (b *satelliteBackend) HeartbeatClaim(ctx context.Context, mutation workflow.ClaimMutation) error {
	return wrapLegErr(b.Name(), "HeartbeatClaim", b.client.HeartbeatClaim(ctx, mutation))
}

func (b *satelliteBackend) CompleteClaim(ctx context.Context, mutation workflow.ClaimMutation) error {
	return wrapLegErr(b.Name(), "CompleteClaim", b.client.CompleteClaim(ctx, mutation))
}

func (b *satelliteBackend) ReleaseClaim(ctx context.Context, mutation workflow.ClaimMutation) error {
	return wrapLegErr(b.Name(), "ReleaseClaim", b.client.ReleaseClaim(ctx, mutation))
}

func (b *satelliteBackend) FailClaimRetryable(ctx context.Context, mutation workflow.ClaimMutation) error {
	return wrapLegErr(b.Name(), "FailClaimRetryable", b.client.FailClaimRetryable(ctx, mutation))
}

func (b *satelliteBackend) FailClaimTerminal(ctx context.Context, mutation workflow.ClaimMutation) error {
	return wrapLegErr(b.Name(), "FailClaimTerminal", b.client.FailClaimTerminal(ctx, mutation))
}

func (b *satelliteBackend) ReapExpiredClaims(ctx context.Context, asOf time.Time, limit int, delay time.Duration) ([]workflow.Claim, error) {
	claims, err := b.client.ReapExpiredClaims(ctx, asOf, limit, delay)
	return claims, wrapLegErr(b.Name(), "ReapExpiredClaims", err)
}

func (b *satelliteBackend) PinForCommit(ctx context.Context, claimID string, fencingToken int64, scopeID, sourceRunID, generationID string, commitDeadline time.Time) (string, error) {
	commitID, err := b.client.PinClaimForCommit(ctx, claimID, fencingToken, scopeID, sourceRunID, generationID, commitDeadline)
	return commitID, wrapLegErr(b.Name(), "PinForCommit", err)
}

func (b *satelliteBackend) ListCollectorInstances(ctx context.Context) ([]workflow.CollectorInstance, error) {
	instances, err := b.client.ListCollectorInstances(ctx)
	return instances, wrapLegErr(b.Name(), "ListCollectorInstances", err)
}
