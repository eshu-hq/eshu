// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contention

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func workflowKind(kind string) scope.CollectorKind { return scope.CollectorKind(kind) }

// quotaRunIndex numbers the quota workload's run after the backlog runs.
func (r *runner) quotaRunIndex() int { return r.spec.BacklogScopes }

// quotaItemPrefix marks quota-workload item ids so the drain check can tell
// a held quota item from a stuck backlog item.
func (r *runner) quotaItemPrefix() string { return fmt.Sprintf("%swi-%d-q-", Prefix, r.spec.Seed) }

func (r *runner) isQuotaHeldEntry(entry string) bool {
	id, _, _ := strings.Cut(entry, ":")
	return strings.HasPrefix(id, r.quotaItemPrefix()+"at-")
}

// newRun builds one synthetic run row.
func newRun(id string, now time.Time) workflow.Run {
	return workflow.Run{
		RunID: id, TriggerKind: workflow.TriggerKindSchedule,
		Status: workflow.RunStatusCollectionActive, CreatedAt: now, UpdatedAt: now,
	}
}

// newItem builds one pending synthetic item. Every identity column the
// claim statement filters on is non-empty and prefixed; the fairness key is
// the scope, so selection rotates across scopes.
func newItem(id, runID, scopeID string, kind scope.CollectorKind, now time.Time) workflow.WorkItem {
	return workflow.WorkItem{
		WorkItemID: id, RunID: runID, CollectorKind: kind, CollectorInstanceID: InstanceID,
		SourceSystem: Prefix + "source", ScopeID: scopeID,
		AcceptanceUnitID: Prefix + "au-" + id, SourceRunID: Prefix + "sr-" + id,
		GenerationID: Prefix + "gen-" + id, FairnessKey: scopeID,
		Status: workflow.WorkItemStatusPending, CreatedAt: now, UpdatedAt: now,
	}
}

// seed writes the backlog: one run per scope, BacklogItems items spread
// round-robin over BacklogScopes scopes, then the quota workload when the
// leg enforces a quota. Runs go first (items reference them).
func (r *runner) seed(ctx context.Context) error {
	now := time.Now().UTC()
	for k := range r.spec.BacklogScopes {
		if err := r.leg.Backend.CreateRun(ctx, newRun(r.ids.run(k), now)); err != nil {
			return fmt.Errorf("create run %d: %w", k, err)
		}
	}
	batch := make([]workflow.WorkItem, 0, r.opt.EnqueueBatch)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		t0 := time.Now()
		err := r.leg.Backend.EnqueueWorkItems(ctx, batch)
		r.rec.observe("enqueue", time.Since(t0), err)
		if err != nil {
			return fmt.Errorf("enqueue %d items: %w", len(batch), err)
		}
		r.created.Add(int64(len(batch)))
		batch = batch[:0]
		return nil
	}
	for n := range r.spec.BacklogItems {
		if err := r.workCtx.Err(); err != nil {
			return fmt.Errorf("seed stopped after %d items: %w", r.created.Load(), err)
		}
		k := n % r.spec.BacklogScopes
		batch = append(batch, newItem(r.ids.workItem(n), r.ids.run(k), r.ids.scope(k), r.opt.CollectorKind, now))
		if len(batch) == cap(batch) {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	if err := flush(); err != nil {
		return err
	}
	if !r.leg.Quota || r.spec.QuotaBudget == 0 {
		return nil
	}
	return r.seedQuota(ctx, now)
}

// seedQuota writes the §5 step-5 workload on the quota kind: the "at" scope
// gets budget+extra items, so claimants overload its reservation and extra
// items stay held; the "below" scope gets budget-1 items and must drain.
func (r *runner) seedQuota(ctx context.Context, now time.Time) error {
	budget := r.spec.QuotaBudget
	extra := max(2, budget/2)
	r.quotaHeldMax = extra
	runID := r.ids.run(r.quotaRunIndex())
	if err := r.leg.Backend.CreateRun(ctx, newRun(runID, now)); err != nil {
		return fmt.Errorf("create quota run: %w", err)
	}
	var items []workflow.WorkItem
	for n := range budget + extra {
		items = append(items, newItem(fmt.Sprintf("%sat-%d", r.quotaItemPrefix(), n), runID,
			r.ids.quotaScope("at"), r.opt.QuotaKind, now))
	}
	for n := range budget - 1 {
		items = append(items, newItem(fmt.Sprintf("%sbelow-%d", r.quotaItemPrefix(), n), runID,
			r.ids.quotaScope("below"), r.opt.QuotaKind, now))
	}
	t0 := time.Now()
	err := r.leg.Backend.EnqueueWorkItems(ctx, items)
	r.rec.observe("enqueue", time.Since(t0), err)
	if err != nil {
		return fmt.Errorf("enqueue quota items: %w", err)
	}
	r.created.Add(int64(len(items)))
	return nil
}
