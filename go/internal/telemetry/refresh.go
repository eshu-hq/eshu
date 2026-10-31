// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Refresh skip reasons label eshu_runtime_refresh_skipped_by_reason_total and
// the refresh_skip_reason log key (#7978). The set is closed: an unknown
// reason records as RefreshSkipReasonOther, so a caller cannot add a label
// value by accident.
const (
	// RefreshSkipReasonUnchanged means the snapshot's freshness hint and
	// commit equal the scope's active generation, so the ingestion store
	// stored nothing.
	RefreshSkipReasonUnchanged = "unchanged"
	// RefreshSkipReasonPendingSameIdentity means the snapshot's hint and
	// commit equal a generation that is still pending (ruling a9-120): the
	// projector has not activated it yet, so the store stored nothing. A
	// rising count means the projector or reducer is behind.
	RefreshSkipReasonPendingSameIdentity = "pending_same_identity"
	// RefreshSkipReasonReconcileReprojected means a reconciliation snapshot
	// matched the active generation, so the store re-opened that generation's
	// projector work instead of storing a duplicate generation.
	RefreshSkipReasonReconcileReprojected = "reconcile_reprojected"
	// RefreshSkipReasonReconcileReprojectionPending means a reconciliation
	// snapshot matched the active generation whose projector work was already
	// pending, retrying, or under a live lease, so the store stored nothing and
	// stamped nothing: the work already queued is the re-projection.
	RefreshSkipReasonReconcileReprojectionPending = "reconcile_reprojection_pending"
	// RefreshSkipReasonReconcileSuperseded means a reconciliation snapshot
	// matched the active generation, but a newer generation activated before
	// the re-open took the scope lock. The snapshot describes an older commit,
	// so it is dropped; the sweep decides the scope again next cycle.
	RefreshSkipReasonReconcileSuperseded = "reconcile_superseded"
	// RefreshSkipReasonFinalizedGenerationReplay means a retry tried to
	// re-commit a generation that is already finalized.
	RefreshSkipReasonFinalizedGenerationReplay = "finalized_generation_replay"
	// RefreshSkipReasonDestructiveSuccessor means the store refused an
	// empty or sharply shrunken successor over a content-bearing prior
	// generation without a trusted-deletion signal (#7926): nothing was
	// stored, so the prior generation stays active. A rising count with no
	// matching operator attestation means a collector is emitting
	// suspiciously empty observations.
	RefreshSkipReasonDestructiveSuccessor = "destructive_successor_refused"
	// RefreshSkipReasonOther counts a record with a reason outside this set.
	RefreshSkipReasonOther = "other"
)

// refreshSkipReasons is the closed, ordered reason set. The counter array
// below is sized from it, so adding a reason here cannot index past the end.
var refreshSkipReasons = [...]string{
	RefreshSkipReasonUnchanged,
	RefreshSkipReasonPendingSameIdentity,
	RefreshSkipReasonReconcileReprojected,
	RefreshSkipReasonReconcileReprojectionPending,
	RefreshSkipReasonReconcileSuperseded,
	RefreshSkipReasonFinalizedGenerationReplay,
	RefreshSkipReasonDestructiveSuccessor,
	RefreshSkipReasonOther,
}

var (
	skippedRefreshCount         atomic.Uint64
	skippedRefreshCountByReason [len(refreshSkipReasons)]atomic.Uint64
)

// refreshSkipReasonIndex maps reason onto the closed set.
func refreshSkipReasonIndex(reason string) int {
	if index := slices.Index(refreshSkipReasons[:], reason); index >= 0 {
		return index
	}
	return len(refreshSkipReasons) - 1
}

// RefreshSkipReasons returns the closed, ordered set of refresh skip reasons.
func RefreshSkipReasons() []string {
	return slices.Clone(refreshSkipReasons[:])
}

// RecordSkippedRefresh increments the process-local skipped refresh counter
// and the counter for reason, which is mapped onto the closed reason set. It
// returns the new total.
func RecordSkippedRefresh(reason string) uint64 {
	skippedRefreshCountByReason[refreshSkipReasonIndex(reason)].Add(1)
	return skippedRefreshCount.Add(1)
}

// SkippedRefreshCount reports the process-local skipped refresh total.
func SkippedRefreshCount() uint64 {
	return skippedRefreshCount.Load()
}

// SkippedRefreshCountByReason reports the process-local skipped refresh count
// for one reason of the closed set, or zero for any other value.
func SkippedRefreshCountByReason(reason string) uint64 {
	index := slices.Index(refreshSkipReasons[:], reason)
	if index < 0 {
		return 0
	}
	return skippedRefreshCountByReason[index].Load()
}

// ResetSkippedRefreshCountForTesting clears the skipped refresh counters.
func ResetSkippedRefreshCountForTesting() {
	skippedRefreshCount.Store(0)
	for index := range skippedRefreshCountByReason {
		skippedRefreshCountByReason[index].Store(0)
	}
}

// Successor guard rules label eshu_dp_ingestion_successor_guard_decisions_total
// and the successor_guard log stage (#8750): which arm of the #7926
// destructive-successor backstop decided one commit. The set is closed; an
// unknown rule records as SuccessorGuardRuleOther.
const (
	// SuccessorGuardRuleTrustedDeletion: the collector attested the
	// deletion, so the successor stores whatever its size.
	SuccessorGuardRuleTrustedDeletion = "trusted_deletion"
	// SuccessorGuardRuleNoPrior: the scope has no prior generation, or the
	// prior holds no collector rows, so nothing can be retracted.
	SuccessorGuardRuleNoPrior = "no_prior"
	// SuccessorGuardRuleOrphanFallback: the prior is a delta whose base is
	// missing (#8362), so it is sized on its own rows.
	SuccessorGuardRuleOrphanFallback = "orphan_fallback"
	// SuccessorGuardRuleEmpty: the successor streamed no facts.
	SuccessorGuardRuleEmpty = "empty"
	// SuccessorGuardRuleCollectorRatio: a full successor compared with the
	// prior's collector rows (reducer-derived rows excluded).
	SuccessorGuardRuleCollectorRatio = "collector_ratio"
	// SuccessorGuardRuleAnchorMissing: a delta with no projected full
	// generation at its baseline commit stores; the projector refuses it as
	// base-missing and arms the reindex watermark.
	SuccessorGuardRuleAnchorMissing = "anchor_missing"
	// SuccessorGuardRuleFileUnionBound: a delta whose zero-query lower bound
	// on its union's file count holds at least half the base's files.
	SuccessorGuardRuleFileUnionBound = "file_union_bound"
	// SuccessorGuardRuleFileUnionExact: a delta whose lower bound failed and
	// whose exact union file count was probed against the base's paths.
	SuccessorGuardRuleFileUnionExact = "file_union_exact"
	// SuccessorGuardRuleOther counts a rule outside this set.
	SuccessorGuardRuleOther = "other"
)

// Successor guard outcomes and kinds, the other two closed label sets of
// eshu_dp_ingestion_successor_guard_decisions_total.
const (
	// SuccessorGuardOutcomeStored: the successor was stored.
	SuccessorGuardOutcomeStored = "stored"
	// SuccessorGuardOutcomeRefused: the successor was refused and the prior
	// generation stays active.
	SuccessorGuardOutcomeRefused = "refused"
	// SuccessorGuardKindFull labels a full successor generation.
	SuccessorGuardKindFull = "full"
	// SuccessorGuardKindDelta labels a delta successor generation.
	SuccessorGuardKindDelta = "delta"
)

var successorGuardRules = [...]string{
	SuccessorGuardRuleTrustedDeletion,
	SuccessorGuardRuleNoPrior,
	SuccessorGuardRuleOrphanFallback,
	SuccessorGuardRuleEmpty,
	SuccessorGuardRuleCollectorRatio,
	SuccessorGuardRuleAnchorMissing,
	SuccessorGuardRuleFileUnionBound,
	SuccessorGuardRuleFileUnionExact,
	SuccessorGuardRuleOther,
}

// SuccessorGuardRules returns the closed, ordered successor guard rule set.
func SuccessorGuardRules() []string {
	return slices.Clone(successorGuardRules[:])
}

// SuccessorGuardRule maps rule onto the closed set.
func SuccessorGuardRule(rule string) string {
	if slices.Contains(successorGuardRules[:], rule) {
		return rule
	}
	return SuccessorGuardRuleOther
}

// SuccessorHoldThreshold is how many consecutive destructive-successor
// refusals make a scope "held stale" (#9694). One refusal is a normal guard
// decision the next collection re-evaluates. Three in a row mean the source
// keeps returning a shrunken or empty snapshot that the store keeps
// rejecting, so the scope's active generation is going stale and an operator
// has to look.
const SuccessorHoldThreshold = 3

// SuccessorHoldTracker counts consecutive destructive-successor refusals per
// scope inside one process. The ingestion store records a refusal when the
// guard refuses a commit and clears the run when the same scope durably stores
// a generation or is skipped as unchanged. It is process-local by design: it
// feeds the eshu_dp_ingestion_successor_held_stale_scopes gauge and adds no
// Postgres read or write to the commit path.
//
// What that costs, exactly. Each replica counts only the refusals it handled,
// and clears only what it saw:
//
//   - a run split across replicas reaches the threshold late or not at all;
//   - a scope that replica A holds and replica B later stores stays held on A
//     until A next handles that scope (a phantom hold);
//   - summing the gauge across replicas can count one scope twice, so sum is an
//     upper bound; max by collector_kind is a lower bound (two replicas each
//     holding a different scope read 1);
//   - a restart drops every held scope from the gauge until three more
//     refusals arrive on that process.
//
// The successor_held_stale commit log names the scope and is the per-scope
// source of truth.
type SuccessorHoldTracker struct {
	mu     sync.Mutex
	scopes map[string]successorHold
}

type successorHold struct {
	collectorKind string
	refusals      int
}

// NewSuccessorHoldTracker returns an empty tracker.
func NewSuccessorHoldTracker() *SuccessorHoldTracker {
	return &SuccessorHoldTracker{scopes: make(map[string]successorHold)}
}

// RecordRefusal counts one refusal for scopeID and returns the consecutive
// run length. A nil tracker or a blank scope records nothing and returns 0.
func (t *SuccessorHoldTracker) RecordRefusal(scopeID, collectorKind string) int {
	scopeID = strings.TrimSpace(scopeID)
	if t == nil || scopeID == "" {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	hold := t.scopes[scopeID]
	hold.refusals++
	hold.collectorKind = strings.TrimSpace(collectorKind)
	t.scopes[scopeID] = hold
	return hold.refusals
}

// Clear ends scopeID's refusal run. It is a no-op for an unknown scope, so
// the commit path can call it on every stored or skipped outcome.
func (t *SuccessorHoldTracker) Clear(scopeID string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.scopes, strings.TrimSpace(scopeID))
}

// HeldByKind returns, per collector kind, how many scopes have reached
// SuccessorHoldThreshold consecutive refusals.
func (t *SuccessorHoldTracker) HeldByKind() map[string]int64 {
	held := make(map[string]int64)
	if t == nil {
		return held
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, hold := range t.scopes {
		if hold.refusals >= SuccessorHoldThreshold {
			held[hold.collectorKind]++
		}
	}
	return held
}

// registerSuccessorHeldStaleScopes registers the #9694 gauge of scopes held
// stale by repeated destructive-successor refusals and the tracker that
// feeds it. The callback reads process-local state only, so a scrape adds no
// query. A collector_kind series exists only while some scope of that kind is
// held on this process; an absent series means this process holds none. With
// several replicas max by collector_kind is a lower bound and sum an upper
// bound (see SuccessorHoldTracker for the per-replica limits).
func registerSuccessorHeldStaleScopes(meter metric.Meter, inst *Instruments) error {
	inst.SuccessorHolds = NewSuccessorHoldTracker()
	var err error
	inst.IngestionSuccessorHeldStaleScopes, err = meter.Int64ObservableGauge(
		"eshu_dp_ingestion_successor_held_stale_scopes",
		metric.WithDescription("Scopes whose last "+fmt.Sprint(SuccessorHoldThreshold)+" or more consecutive commits were refused by the "+
			"destructive-successor guard (#9694), by collector_kind; the scope's active generation is going stale. "+
			"Process-local and per replica: max by collector_kind is a lower bound and sum an upper bound; a series exists only while this process holds a scope"),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			for kind, count := range inst.SuccessorHolds.HeldByKind() {
				o.Observe(count, metric.WithAttributes(attribute.String(MetricDimensionCollectorKind, kind)))
			}
			return nil
		}),
	)
	if err != nil {
		return fmt.Errorf("register IngestionSuccessorHeldStaleScopes gauge: %w", err)
	}
	return nil
}
