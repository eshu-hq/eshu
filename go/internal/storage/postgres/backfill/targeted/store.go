// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package targeted

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// WorkItem identifies a succeeded reducer item and its owning partition.
type WorkItem struct {
	WorkItemID string
	Partition  backfill.ScopeGenerationPartition
}

// Store owns the targeted pass and receives parent-owned collaborators through
// narrow callbacks. Each callback uses the database or transaction supplied by
// this pass, so the transaction and lock lifetime remain here. The pass
// rejects missing callbacks, reopen queries, or correlation domains before
// starting telemetry or database work.
type Store struct {
	Backfill           backfill.Store
	PhaseCommitted     func(context.Context, db.ExecQueryer, backfill.ScopeGenerationPartition) (bool, error)
	GateReopen         func(context.Context, string, []WorkItem, map[backfill.ScopeGenerationPartition]struct{}) ([]string, int)
	ReopenSucceeded    func(context.Context, db.Transaction, string) (bool, error)
	CorrelationDomains []string
	Queries            Queries
	// InboundBudget bounds the anchor-scoped inbound fact load the closure
	// runs (backfill.LoadAnchorScopedRelationshipFactsBudget). A zero value is
	// unbounded. Set it to fail the pass loudly when the inbound load regresses
	// to the whole corpus instead of OOM-killing the reducer (#9781). It is a
	// per-pass bound, not shared state, so it never serializes workers.
	InboundBudget backfill.RelationshipFactLoadBudget
}
