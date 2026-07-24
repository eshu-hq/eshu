// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ghactionsruntime

import (
	"sync"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// This file holds the #5429 commit-ordering fix's in-memory staging area:
// NextClaimed stashes the newest run ID it observed here instead of saving
// it directly to the durable runwatermark.Store, and
// ClaimedSource.ObserveClaimedGenerationCommitted
// (source_commit_observer.go) is what actually persists it, AFTER
// collector.ClaimedService confirms the generation's facts committed
// durably. See run_watermark.go's saveWatermark doc comment for the full
// ordering argument.

// pendingWatermarkKey identifies one in-flight claim cycle's not-yet-durable
// newest observed run ID. Keyed by (ScopeID, GenerationID) rather than just
// the target's watermarkKey (ScopeID, Repository) so a stashed entry can
// only ever be consumed by the commit-observer call for the EXACT claim
// cycle that produced it, even if more than one generation for the same
// scope were ever in flight at once.
type pendingWatermarkKey struct {
	ScopeID      string
	GenerationID string
}

// pendingWatermarkEntry is the not-yet-durable state NextClaimed stashed for
// one claim cycle, consumed by ObserveClaimedGenerationCommitted once the
// cycle's commit is confirmed durable.
type pendingWatermarkEntry struct {
	target      TargetConfig
	newestRunID string
}

// pendingWatermarks is a mutex-guarded staging map shared, via pointer
// indirection, across every value copy of the ClaimedSource that owns it.
// ClaimedSource is a value type (see NewClaimedSource), and
// collector.MultiSourceCollectorHost can run several ClaimedService workers
// concurrently against the SAME registered source
// (MultiSourceCollectorHostConfig.Workers); those workers claim DIFFERENT
// work items and can call NextClaimed and ObserveClaimedGenerationCommitted
// for different items concurrently, so the map itself needs a mutex. The
// pointer field (not a value field) is what lets every copy of ClaimedSource
// -- interface boxing, collector.ClaimSourceRegistration.Source, the
// resolver's map return -- observe writes made through any other copy:
// NewClaimedSource allocates pendingWatermarks once, and every subsequent
// struct copy carries the same pointer.
//
// Per-key races are not possible: workflow.WorkItem rows are claimed with
// `FOR UPDATE SKIP LOCKED` over `status = 'pending'`
// (workflow_control_sql.go's claimNextWorkflowWorkItemQuery), so at most one
// claim per work item row -- and therefore per (ScopeID, GenerationID) -- is
// ever active at a time. Two concurrent workers therefore never stash or
// take the SAME key concurrently; they only ever contend on the shared map
// structure itself, which the mutex serializes.
//
// Bounding note: a stashed entry is removed by takeFor when
// ObserveClaimedGenerationCommitted successfully consumes it, and is
// overwritten (not accumulated) by a later stash for the SAME key (a
// retried claim re-running NextClaimed for the same work item). If a claim
// cycle's commit permanently fails (a terminal failure whose work item is
// never retried under the same GenerationID), its stashed entry is never
// consumed and stays in this map for the life of the process --
// ClaimedSource has no reachable "claim permanently abandoned" hook to
// clean it up from, since NextClaimed is only ever called on the collect
// side. This is an accepted, documented tradeoff: terminal failures are the
// rare, MaxAttempts-guarded exception path, not the steady state, and each
// leaked entry costs two small strings plus a TargetConfig value.
type pendingWatermarks struct {
	mu   sync.Mutex
	rows map[pendingWatermarkKey]pendingWatermarkEntry
}

// newPendingWatermarks returns an empty pending-watermark staging map.
func newPendingWatermarks() *pendingWatermarks {
	return &pendingWatermarks{rows: make(map[pendingWatermarkKey]pendingWatermarkEntry)}
}

// stash records newestRunID as the not-yet-durable watermark for item's
// (ScopeID, GenerationID). A later call with the SAME key (a retried claim
// re-running NextClaimed) overwrites the prior entry: the retry's window is
// what should be persisted if the retry is the one that ends up committing.
// An empty newestRunID (an empty fetched window) is a no-op, matching
// saveWatermark's own prior no-op behavior for an empty window. p is
// expected to be non-nil (NewClaimedSource always allocates it), but the
// nil check keeps a zero-value ClaimedSource{} (as used by some stub-only
// tests) safe rather than panicking.
func (p *pendingWatermarks) stash(item workflow.WorkItem, target TargetConfig, newestRunID string) {
	if p == nil || newestRunID == "" {
		return
	}
	key := pendingWatermarkKey{ScopeID: item.ScopeID, GenerationID: item.GenerationID}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rows[key] = pendingWatermarkEntry{target: target, newestRunID: newestRunID}
}

// takeFor returns and removes the stashed entry for item's (ScopeID,
// GenerationID), if any. ok is false when NextClaimed never stashed an
// entry for this exact key (an empty fetched window, or the observer firing
// for an item this ClaimedSource never actually claimed) -- both are
// no-ops, not errors, for the caller.
func (p *pendingWatermarks) takeFor(item workflow.WorkItem) (pendingWatermarkEntry, bool) {
	if p == nil {
		return pendingWatermarkEntry{}, false
	}
	key := pendingWatermarkKey{ScopeID: item.ScopeID, GenerationID: item.GenerationID}
	p.mu.Lock()
	defer p.mu.Unlock()
	entry, ok := p.rows[key]
	if ok {
		delete(p.rows, key)
	}
	return entry, ok
}
