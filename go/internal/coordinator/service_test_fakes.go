// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

type fakeStore struct {
	observed             []time.Time
	desired              [][]workflow.DesiredCollectorInstance
	instances            []workflow.CollectorInstance
	createdRuns          []workflow.Run
	enqueuedItems        []workflow.WorkItem
	reapedClaims         []workflow.Claim
	reconcileErr         error
	listErr              error
	createRunErr         error
	enqueueErr           error
	reapErr              error
	runReconcileErr      error
	reapCalls            int
	runReconcileCalls    int
	runReconcileObserved []time.Time
	runReconcileHook     func(int)
}

type fakeOwnedPackageTargetReader struct {
	requests []workflow.OwnedPackageDependencyTargetFilter
	targets  []workflow.OwnedPackageDependencyTarget
	err      error
}

type fakeGovernanceAuditAppender struct {
	events []governanceaudit.Event
	err    error
}

func (f *fakeGovernanceAuditAppender) Append(_ context.Context, events []governanceaudit.Event) error {
	if f.err != nil {
		return f.err
	}
	f.events = append(f.events, events...)
	return nil
}

func (f *fakeOwnedPackageTargetReader) ListOwnedPackageDependencyTargets(
	_ context.Context,
	filter workflow.OwnedPackageDependencyTargetFilter,
) ([]workflow.OwnedPackageDependencyTarget, error) {
	f.requests = append(f.requests, filter)
	if f.err != nil {
		return nil, f.err
	}
	targets := append([]workflow.OwnedPackageDependencyTarget(nil), f.targets...)
	if filter.Limit > 0 && len(targets) > filter.Limit {
		targets = targets[:filter.Limit]
	}
	return targets, nil
}

func (f *fakeStore) ReconcileCollectorInstances(_ context.Context, observedAt time.Time, desired []workflow.DesiredCollectorInstance) error {
	f.observed = append(f.observed, observedAt)
	f.desired = append(f.desired, desired)
	return f.reconcileErr
}

func (f *fakeStore) ListCollectorInstances(context.Context) ([]workflow.CollectorInstance, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return append([]workflow.CollectorInstance(nil), f.instances...), nil
}

func (f *fakeStore) CreateRun(_ context.Context, run workflow.Run) error {
	if f.createRunErr != nil {
		return f.createRunErr
	}
	f.createdRuns = append(f.createdRuns, run)
	return nil
}

func (f *fakeStore) CreateRunWithWorkItemsIfNoOpenTargets(
	ctx context.Context,
	run workflow.Run,
	items []workflow.WorkItem,
) (int, error) {
	if f.createRunErr != nil {
		return 0, f.createRunErr
	}
	eligible := make([]workflow.WorkItem, 0, len(items))
	for _, item := range items {
		if fakeStoreHasOpenWorkItem(f.enqueuedItems, item) {
			continue
		}
		eligible = append(eligible, item)
	}
	if len(eligible) == 0 {
		return 0, nil
	}
	if err := f.CreateRun(ctx, run); err != nil {
		return 0, err
	}
	if err := f.EnqueueWorkItems(ctx, eligible); err != nil {
		return 0, err
	}
	return len(eligible), nil
}

func fakeStoreHasOpenWorkItem(existing []workflow.WorkItem, candidate workflow.WorkItem) bool {
	for _, item := range existing {
		if item.CollectorKind == candidate.CollectorKind &&
			item.CollectorInstanceID == candidate.CollectorInstanceID &&
			item.ScopeID == candidate.ScopeID &&
			item.TenantID == candidate.TenantID &&
			item.WorkspaceID == candidate.WorkspaceID &&
			item.SubjectClass == candidate.SubjectClass &&
			item.PolicyRevisionHash == candidate.PolicyRevisionHash &&
			item.AcceptanceUnitID == candidate.AcceptanceUnitID {
			return true
		}
	}
	return false
}

func (f *fakeStore) EnqueueWorkItems(_ context.Context, items []workflow.WorkItem) error {
	if f.enqueueErr != nil {
		return f.enqueueErr
	}
	f.enqueuedItems = append(f.enqueuedItems, items...)
	return nil
}

func (f *fakeStore) ReapExpiredClaims(_ context.Context, observedAt time.Time, limit int, requeueDelay time.Duration) ([]workflow.Claim, error) {
	f.reapCalls++
	f.observed = append(f.observed, observedAt)
	if f.reapErr != nil {
		return nil, f.reapErr
	}
	return append([]workflow.Claim(nil), f.reapedClaims...), nil
}

func (f *fakeStore) ReconcileWorkflowRuns(_ context.Context, observedAt time.Time) (int, error) {
	f.runReconcileCalls++
	f.runReconcileObserved = append(f.runReconcileObserved, observedAt)
	if f.runReconcileHook != nil {
		f.runReconcileHook(f.runReconcileCalls)
	}
	if f.runReconcileErr != nil {
		return 0, f.runReconcileErr
	}
	return 2, nil
}

type fakeMetrics struct {
	observations             []ReconcileObservation
	reapObservations         []ReapObservation
	runReconcilations        []RunReconciliationObservation
	awsFreshnessReapObserved []FreshnessReapObservation
	gcpFreshnessReapObserved []FreshnessReapObservation
}

func (f *fakeMetrics) RecordReconcile(_ context.Context, observation ReconcileObservation) {
	f.observations = append(f.observations, observation)
}

func (f *fakeMetrics) RecordReap(_ context.Context, observation ReapObservation) {
	f.reapObservations = append(f.reapObservations, observation)
}

func (f *fakeMetrics) RecordRunReconciliation(_ context.Context, observation RunReconciliationObservation) {
	f.runReconcilations = append(f.runReconcilations, observation)
}

func (f *fakeMetrics) RecordAWSFreshnessReap(_ context.Context, observation FreshnessReapObservation) {
	f.awsFreshnessReapObserved = append(f.awsFreshnessReapObserved, observation)
}

func (f *fakeMetrics) RecordGCPFreshnessReap(_ context.Context, observation FreshnessReapObservation) {
	f.gcpFreshnessReapObserved = append(f.gcpFreshnessReapObserved, observation)
}
