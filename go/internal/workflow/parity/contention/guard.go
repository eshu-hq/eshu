// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contention

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
	"github.com/eshu-hq/eshu/go/internal/workflow/parity"
)

// ErrForeignWrite reports a harness write that would leave the synthetic
// prefix or the harness's own collector instance. The guard refuses it
// before it reaches the backend.
var ErrForeignWrite = errors.New("contention guard refused a write outside the synthetic instance")

// guardedBackend wraps every write the harness issues and refuses any that
// names a non-synthetic id or a collector instance other than InstanceID.
// The global routes (ReconcileCollectorInstances, ReconcileWorkflowRuns,
// UpsertCompletenessStates) are always refused, and ReapExpiredClaims is
// refused unless the leg allows the harness reaper. A refusal also calls
// onRefuse, which aborts the run.
type guardedBackend struct {
	inner      parity.Backend
	allowReap  bool
	onRefuse   func(*Violation)
	instanceID string
}

func newGuardedBackend(inner parity.Backend, allowReap bool, onRefuse func(*Violation)) *guardedBackend {
	return &guardedBackend{inner: inner, allowReap: allowReap, onRefuse: onRefuse, instanceID: InstanceID}
}

func (g *guardedBackend) refuse(op, format string, args ...any) error {
	detail := op + ": " + fmt.Sprintf(format, args...)
	if g.onRefuse != nil {
		g.onRefuse(violation(TriggerNonSynthetic, "guard refused %s", detail))
	}
	return fmt.Errorf("%s: %w", detail, ErrForeignWrite)
}

func (g *guardedBackend) checkIDs(op string, ids ...string) error {
	for _, id := range ids {
		if !IsSynthetic(id) {
			return g.refuse(op, "id %q lacks the %s prefix", id, Prefix)
		}
	}
	return nil
}

func (g *guardedBackend) checkItems(op string, items []workflow.WorkItem) error {
	for _, it := range items {
		if it.CollectorInstanceID != g.instanceID {
			return g.refuse(op, "item %q names collector instance %q, not %q", it.WorkItemID, it.CollectorInstanceID, g.instanceID)
		}
		if err := g.checkIDs(op, it.WorkItemID, it.RunID, it.ScopeID, it.AcceptanceUnitID,
			it.SourceRunID, it.GenerationID, it.SourceSystem); err != nil {
			return err
		}
	}
	return nil
}

// checkRun refuses a run that is not synthetic or that names a real
// collector or scope set. The harness sets neither field; an empty value
// passes.
func (g *guardedBackend) checkRun(op string, run workflow.Run) error {
	if err := g.checkIDs(op, run.RunID); err != nil {
		return err
	}
	if run.RequestedCollector != "" {
		if err := g.checkIDs(op, run.RequestedCollector); err != nil {
			return err
		}
	}
	if run.RequestedScopeSet == "" {
		return nil
	}
	var scopes []string
	if err := json.Unmarshal([]byte(run.RequestedScopeSet), &scopes); err != nil {
		return g.refuse(op, "requested scope set %q is not a list of synthetic scope ids", run.RequestedScopeSet)
	}
	return g.checkIDs(op, scopes...)
}

func (g *guardedBackend) Name() string { return g.inner.Name() }

func (g *guardedBackend) CreateRun(ctx context.Context, run workflow.Run) error {
	if err := g.checkRun("CreateRun", run); err != nil {
		return err
	}
	return g.inner.CreateRun(ctx, run) //nolint:wrapcheck // pass-through keeps the leg error chain for parity.Classify
}

func (g *guardedBackend) EnqueueWorkItems(ctx context.Context, items []workflow.WorkItem) error {
	if err := g.checkItems("EnqueueWorkItems", items); err != nil {
		return err
	}
	return g.inner.EnqueueWorkItems(ctx, items) //nolint:wrapcheck // pass-through keeps the leg error chain
}

func (g *guardedBackend) AdmitGuarded(ctx context.Context, run workflow.Run, items []workflow.WorkItem) (workflow.RunAdmission, error) {
	if err := g.checkRun("AdmitGuarded", run); err != nil {
		return workflow.RunAdmission{}, err
	}
	if err := g.checkItems("AdmitGuarded", items); err != nil {
		return workflow.RunAdmission{}, err
	}
	return g.inner.AdmitGuarded(ctx, run, items) //nolint:wrapcheck // pass-through keeps the leg error chain
}

func (g *guardedBackend) ReconcileCollectorInstances(context.Context, time.Time, []workflow.DesiredCollectorInstance) error {
	return g.refuse("ReconcileCollectorInstances", "global route rewrites every instance")
}

func (g *guardedBackend) UpsertCompletenessStates(context.Context, []workflow.CompletenessState) error {
	return g.refuse("UpsertCompletenessStates", "the harness owns no completeness state")
}

func (g *guardedBackend) ReconcileWorkflowRuns(context.Context, time.Time) (int, error) {
	return 0, g.refuse("ReconcileWorkflowRuns", "global route transitions runs the harness does not own")
}

func (g *guardedBackend) ClaimNextEligible(ctx context.Context, sel workflow.ClaimSelector, now time.Time, lease time.Duration) (workflow.WorkItem, workflow.Claim, bool, error) {
	if sel.CollectorInstanceID != g.instanceID {
		return workflow.WorkItem{}, workflow.Claim{}, false,
			g.refuse("ClaimNextEligible", "selector names collector instance %q, not %q", sel.CollectorInstanceID, g.instanceID)
	}
	if err := g.checkIDs("ClaimNextEligible", sel.ClaimID, sel.OwnerID); err != nil {
		return workflow.WorkItem{}, workflow.Claim{}, false, err
	}
	return g.inner.ClaimNextEligible(ctx, sel, now, lease) //nolint:wrapcheck // pass-through keeps the leg error chain
}

func (g *guardedBackend) checkMutation(op string, m workflow.ClaimMutation) error {
	return g.checkIDs(op, m.WorkItemID, m.ClaimID, m.OwnerID)
}

func (g *guardedBackend) HeartbeatClaim(ctx context.Context, m workflow.ClaimMutation) error {
	if err := g.checkMutation("HeartbeatClaim", m); err != nil {
		return err
	}
	return g.inner.HeartbeatClaim(ctx, m) //nolint:wrapcheck // pass-through keeps the leg error chain
}

func (g *guardedBackend) CompleteClaim(ctx context.Context, m workflow.ClaimMutation) error {
	if err := g.checkMutation("CompleteClaim", m); err != nil {
		return err
	}
	return g.inner.CompleteClaim(ctx, m) //nolint:wrapcheck // pass-through keeps the leg error chain
}

func (g *guardedBackend) ReleaseClaim(ctx context.Context, m workflow.ClaimMutation) error {
	if err := g.checkMutation("ReleaseClaim", m); err != nil {
		return err
	}
	return g.inner.ReleaseClaim(ctx, m) //nolint:wrapcheck // pass-through keeps the leg error chain
}

func (g *guardedBackend) FailClaimRetryable(ctx context.Context, m workflow.ClaimMutation) error {
	if err := g.checkMutation("FailClaimRetryable", m); err != nil {
		return err
	}
	return g.inner.FailClaimRetryable(ctx, m) //nolint:wrapcheck // pass-through keeps the leg error chain
}

func (g *guardedBackend) FailClaimTerminal(ctx context.Context, m workflow.ClaimMutation) error {
	if err := g.checkMutation("FailClaimTerminal", m); err != nil {
		return err
	}
	return g.inner.FailClaimTerminal(ctx, m) //nolint:wrapcheck // pass-through keeps the leg error chain
}

func (g *guardedBackend) ReapExpiredClaims(ctx context.Context, asOf time.Time, limit int, delay time.Duration) ([]workflow.Claim, error) {
	if !g.allowReap {
		return nil, g.refuse("ReapExpiredClaims", "the reap route is global; this leg relies on the deployed reaper")
	}
	return g.inner.ReapExpiredClaims(ctx, asOf, limit, delay) //nolint:wrapcheck // pass-through keeps the leg error chain
}

func (g *guardedBackend) PinForCommit(ctx context.Context, claimID string, token int64, scopeID, sourceRunID, generationID string, deadline time.Time) (string, error) {
	if err := g.checkIDs("PinForCommit", claimID, scopeID, sourceRunID, generationID); err != nil {
		return "", err
	}
	return g.inner.PinForCommit(ctx, claimID, token, scopeID, sourceRunID, generationID, deadline) //nolint:wrapcheck // pass-through keeps the leg error chain
}

func (g *guardedBackend) ListCollectorInstances(ctx context.Context) ([]workflow.CollectorInstance, error) {
	return g.inner.ListCollectorInstances(ctx) //nolint:wrapcheck // read-only pass-through
}
