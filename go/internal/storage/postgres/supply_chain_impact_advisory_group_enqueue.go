// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	advisorygroup "github.com/eshu-hq/eshu/go/internal/reducer/advisory/group"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// advisoryGroupProjectionCause is the reason stamped on every
// advisory_group_projection work item a supply_chain_impact pass admits.
// The claim path surfaces it as the intent's Cause, so a dead letter
// names the winners touch that owed the projection. It is a constant on
// purpose: the group id already rides the entity key, and a per-group
// reason would make the durable cause an unbounded string family.
const advisoryGroupProjectionCause = "supply_chain_impact_winners_touch"

// AdvisoryGroupsSyntheticScope and AdvisoryGroupsSyntheticGenesis are the
// reserved re-homing slot for advisory_group_projection rows (#10566,
// seeded by migration 295). Every touch of a group rides this slot instead
// of the origin pass's scope/generation, so the domain holds one live row
// per group and the backlog caps at the group count. The claim-time
// supersede sweep never fires on these rows by its existing
// active-generation guard, and retention never considers the always-active
// genesis.
const (
	AdvisoryGroupsSyntheticScope   = "synthetic:advisory-groups"
	AdvisoryGroupsSyntheticGenesis = "synthetic:advisory-groups:genesis"
)

// Advisory group payload keys. group_id names the projected group;
// origin_scope_id and origin_generation_id record the touching pass for
// drill-down only: they affect no work item id, conflict key, predicate,
// or handler read.
const (
	advisoryGroupPayloadGroupID     = "group_id"
	advisoryGroupPayloadOriginScope = "origin_scope_id"
	advisoryGroupPayloadOriginGen   = "origin_generation_id"
)

// advisoryGroupIntentQueue is the reducer-queue surface the projection
// enqueuer needs: admit new work, and reopen work that already finished
// in this generation.
type advisoryGroupIntentQueue interface {
	Enqueue(ctx context.Context, intents []runtime.ReducerIntent) (runtime.IntentResult, error)
	RetouchWorkItem(ctx context.Context, workItemID string) (bool, error)
}

// AdvisoryGroupProjectionEnqueuer admits one advisory_group_projection
// reducer intent per advisory group a committed supply_chain_impact pass
// touched (#10411). It is the storage side of the handler's
// SupplyChainAdvisoryGroupProjectionEnqueuer port, beside the fan-out
// store whose post-commit call shape it mirrors.
//
// Each intent rides the synthetic re-homing slot (#10566) with the group
// as its single entity key, so the claim path materializes exactly the
// contract intent the advisory group handler reads: EntityKeys =
// ["advisory_group:<id>"], Cause = advisoryGroupProjectionCause, and the
// origin source system mirrored. The related scope is the synthetic slot
// scope, which the claim read derives from the work item's own scope; the
// origin pass rides the payload for drill-down only. The work item id is
// a pure function of the group, so the existing ON CONFLICT DO NOTHING
// plus RetouchWorkItem is the race-free coalescing primitive.
//
// reducerConflictDomainKey partitions these intents by entity key, so
// disjoint groups claim concurrently while the same group never
// reconciles concurrently from two scopes.
type AdvisoryGroupProjectionEnqueuer struct {
	queue advisoryGroupIntentQueue
}

// NewAdvisoryGroupProjectionEnqueuer returns an enqueuer over the
// reducer work queue the reducer service claims from.
func NewAdvisoryGroupProjectionEnqueuer(queue ReducerQueue) AdvisoryGroupProjectionEnqueuer {
	return AdvisoryGroupProjectionEnqueuer{queue: queue}
}

// EnqueueAdvisoryGroupProjections enqueues one projection intent per
// distinct non-blank group id, then retouches each intent's row. The
// retouch is load-bearing: the enqueue insert is ON CONFLICT
// (work_item_id) DO NOTHING, and the work item id is a pure function of
// the group (synthetic slot, domain, entity key), so a group touched
// twice — from any scope or generation — collides with its own first
// intent instead of admitting a second one. RetouchWorkItem covers every
// live state of that row: a succeeded, failed, or dead-letter row reopens
// (a new touch is new evidence, not the same poison), a pending or
// retrying row advances its cycle start (reopened_at, which the claim
// read derives CycleStartedAt from) so the handler's winners watermark
// bound waits for winners covering the later touch, and a claimed or
// running row is marked for replay so its ACK returns it to pending for a
// rerun instead of succeeding against pre-commit winners.
func (e AdvisoryGroupProjectionEnqueuer) EnqueueAdvisoryGroupProjections(
	ctx context.Context,
	origin reducercontract.Intent,
	groupIDs []string,
) error {
	if e.queue == nil {
		return fmt.Errorf("advisory group projection queue is required")
	}
	seen := make(map[string]struct{}, len(groupIDs))
	intents := make([]runtime.ReducerIntent, 0, len(groupIDs))
	for _, raw := range groupIDs {
		groupID := strings.TrimSpace(raw)
		if groupID == "" {
			continue
		}
		if _, dup := seen[groupID]; dup {
			continue
		}
		seen[groupID] = struct{}{}
		intents = append(intents, runtime.ReducerIntent{
			ScopeID:      AdvisoryGroupsSyntheticScope,
			GenerationID: AdvisoryGroupsSyntheticGenesis,
			Domain:       reducercontract.DomainAdvisoryGroupProjection,
			EntityKey:    advisorygroup.EntityKey(groupID),
			Reason:       advisoryGroupProjectionCause,
			SourceSystem: origin.SourceSystem,
			Payload: map[string]any{
				advisoryGroupPayloadGroupID:     groupID,
				advisoryGroupPayloadOriginScope: origin.ScopeID,
				advisoryGroupPayloadOriginGen:   origin.GenerationID,
			},
		})
	}
	if len(intents) == 0 {
		return nil
	}
	if _, err := e.queue.Enqueue(ctx, intents); err != nil {
		return fmt.Errorf("enqueue advisory group projections: %w", err)
	}
	for _, intent := range intents {
		if _, err := e.queue.RetouchWorkItem(ctx, reducerWorkItemID(intent)); err != nil {
			return fmt.Errorf("retouch advisory group projection: %w", err)
		}
	}
	return nil
}
