// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill/targeted"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Targeted-engine seam wiring (#7794, #11615 labels): the parent-owned
// phase, queue, and memo-gate implementations the targeted engine runs
// through, plus the stable alias surface the rest of the tree consumes.
// ReopenSucceeded here stamps maintenance_reopen: every targeted and floor
// backfill path is a deferred-maintenance replay.

// backwardEvidencePhaseCommitted reports whether a partition owes cross-repo
// evidence reconciliation for the targeted pass.
func backwardEvidencePhaseCommitted(
	ctx context.Context,
	database db.ExecQueryer,
	partition backfill.ScopeGenerationPartition,
) (bool, error) {
	ready, found, err := NewGraphProjectionPhaseStateStore(database).Lookup(ctx, gpphase.PhaseKey{
		ScopeID:          partition.ScopeID,
		AcceptanceUnitID: partition.ScopeID,
		SourceRunID:      partition.GenerationID,
		GenerationID:     partition.GenerationID,
		Keyspace:         gpphase.KeyspaceCrossRepoEvidence,
	}, gpphase.PhaseBackwardEvidenceCommitted)
	if err != nil {
		return false, fmt.Errorf("read backward evidence phase for %q/%q: %w", partition.ScopeID, partition.GenerationID, err)
	}
	return found && ready, nil
}

// targetedCatalogBaselineQuery remains a local alias for the whole-pass
// unchanged-partition proof, which compares the shipped targeted guard.
const targetedCatalogBaselineQuery = targeted.CatalogBaselineQuery

// targetedStore supplies the targeted engine with its existing parent-owned
// phase, queue, and same-pass memo-gate implementations.
func (s IngestionStore) targetedStore() targeted.Store {
	return targeted.Store{
		Backfill:       s.backfillStore(),
		PhaseCommitted: backwardEvidencePhaseCommitted,
		GateReopen: func(ctx context.Context, domain string, items []targeted.WorkItem,
			skipped map[backfill.ScopeGenerationPartition]struct{},
		) ([]string, int) {
			refs := make([]reopenWorkItemRef, len(items))
			for i, item := range items {
				refs[i] = reopenWorkItemRef{WorkItemID: item.WorkItemID, Partition: item.Partition}
			}
			gate := applyReopenPartitionMemoGate(ctx, domain, refs, skipped, nil)
			ids := make([]string, len(gate.ToReopen))
			for i, item := range gate.ToReopen {
				ids[i] = item.WorkItemID
			}
			return ids, len(gate.Skipped)
		},
		ReopenSucceeded: func(ctx context.Context, tx db.Transaction, workItemID string) (bool, error) {
			// Targeted and floor backfill reopens are deferred-maintenance
			// replays (#11615 decision 6).
			return (ReducerQueue{database: tx, Now: s.Now}).ReopenSucceeded(ctx, workItemID, telemetry.RequeueReasonMaintenanceReopen)
		},
		CorrelationDomains: CrossScopeCorrelationReopenDomains(),
		Queries:            targeted.ProductionQueries,
		InboundBudget:      backfill.RelationshipFactLoadBudget{MaxBytes: targeted.DefaultInboundBudgetBytes},
	}
}

// RunDeferredRelationshipMaintenanceForPartitions runs the exact
// partition-scoped maintenance engine for the owed partitions.
func (s IngestionStore) RunDeferredRelationshipMaintenanceForPartitions(
	ctx context.Context, tracer trace.Tracer, instruments *telemetry.Instruments, owed []OwedPartition,
) (TargetedMaintenanceResult, error) {
	return s.targetedStore().RunDeferredRelationshipMaintenanceForPartitions(ctx, tracer, instruments, owed)
}

// OwedPartition names one activation obligation's scope and generation.
type OwedPartition = targeted.OwedPartition

// TargetedMaintenanceResult reports one targeted pass.
type TargetedMaintenanceResult = targeted.TargetedMaintenanceResult

// TargetedMaintenanceError is a stable typed refusal.
type TargetedMaintenanceError = targeted.TargetedMaintenanceError

// TargetedMaintenanceOutcomeKind names a per-owed result.
type TargetedMaintenanceOutcomeKind = targeted.TargetedMaintenanceOutcomeKind

// TargetedMaintenanceOutcome pairs an owed partition with its result.
type TargetedMaintenanceOutcome = targeted.TargetedMaintenanceOutcome

const (
	// TargetedMaintenancePublished reports a committed phase.
	TargetedMaintenancePublished = targeted.TargetedMaintenancePublished
	// TargetedMaintenanceNotActive reports a superseded generation.
	TargetedMaintenanceNotActive = targeted.TargetedMaintenanceNotActive
	// TargetedMaintenanceInapplicable reports a partition no pass can publish.
	TargetedMaintenanceInapplicable = targeted.TargetedMaintenanceInapplicable
	// TargetedMaintenanceRetry reports an active partition still awaiting a phase.
	TargetedMaintenanceRetry = targeted.TargetedMaintenanceRetry
)

var (
	// ErrTargetedMaintenanceCatalogChanged refuses a stale catalog.
	ErrTargetedMaintenanceCatalogChanged = targeted.ErrTargetedMaintenanceCatalogChanged
	// ErrTargetedMaintenanceNoMemoBaseline refuses an absent baseline.
	ErrTargetedMaintenanceNoMemoBaseline = targeted.ErrTargetedMaintenanceNoMemoBaseline
	// ErrTargetedMaintenanceClosureTooDeep refuses an unsettled closure.
	ErrTargetedMaintenanceClosureTooDeep = targeted.ErrTargetedMaintenanceClosureTooDeep
	// ErrTargetedMaintenanceInapplicable identifies an impossible phase.
	ErrTargetedMaintenanceInapplicable = targeted.ErrTargetedMaintenanceInapplicable
	// ErrTargetedMaintenanceNotActive identifies a superseded generation.
	ErrTargetedMaintenanceNotActive = targeted.ErrTargetedMaintenanceNotActive
	// ErrTargetedMaintenanceRetry identifies a phase still owed.
	ErrTargetedMaintenanceRetry = targeted.ErrTargetedMaintenanceRetry
)

// TargetedMaintenanceReasons returns the sorted closed error-label set.
func TargetedMaintenanceReasons() []string { return targeted.TargetedMaintenanceReasons() }

// TargetedMaintenanceReason maps an error to its stable label.
func TargetedMaintenanceReason(err error) string { return targeted.TargetedMaintenanceReason(err) }
