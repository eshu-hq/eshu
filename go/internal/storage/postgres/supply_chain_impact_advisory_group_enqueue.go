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

// advisoryGroupIntentQueue is the reducer-queue surface the projection
// enqueuer needs: admit new work, and reopen work that already finished
// in this generation.
type advisoryGroupIntentQueue interface {
	Enqueue(ctx context.Context, intents []runtime.ReducerIntent) (runtime.IntentResult, error)
	ReopenSucceeded(ctx context.Context, workItemID string) (bool, error)
}

// AdvisoryGroupProjectionEnqueuer admits one advisory_group_projection
// reducer intent per advisory group a committed supply_chain_impact pass
// touched (#10411). It is the storage side of the handler's
// SupplyChainAdvisoryGroupProjectionEnqueuer port, beside the fan-out
// store whose post-commit call shape it mirrors.
//
// Each intent rides the origin pass's scope and generation with the
// group as its single entity key, so the claim path materializes exactly
// the contract intent the advisory group handler reads: EntityKeys =
// ["advisory_group:<id>"], Cause = advisoryGroupProjectionCause, and the
// origin source system mirrored. The related scope is the origin scope,
// which the claim read derives from the work item's own scope.
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
// distinct non-blank group id, then reopens each intent's row in case it
// already succeeded in this generation. The reopen is load-bearing: the
// enqueue insert is ON CONFLICT (work_item_id) DO NOTHING, and the work
// item id is a function of (scope, generation, domain, entity key), so a
// group first projected and then touched again in the same generation —
// a fan-out reopened one-hop pass re-deriving its stamp, say — would
// otherwise keep its first projection until the next generation.
// ReopenSucceeded only transitions succeeded rows, so pending or
// in-flight work is undisturbed; its reopened_at also becomes the row's
// cycle start, which is what the handler's winners watermark bound
// compares against, so a reopened projection still waits for winners
// that cover the later touch.
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
			ScopeID:      origin.ScopeID,
			GenerationID: origin.GenerationID,
			Domain:       reducercontract.DomainAdvisoryGroupProjection,
			EntityKey:    advisorygroup.EntityKey(groupID),
			Reason:       advisoryGroupProjectionCause,
			SourceSystem: origin.SourceSystem,
			Payload:      map[string]any{"group_id": groupID},
		})
	}
	if len(intents) == 0 {
		return nil
	}
	if _, err := e.queue.Enqueue(ctx, intents); err != nil {
		return fmt.Errorf("enqueue advisory group projections: %w", err)
	}
	for _, intent := range intents {
		if _, err := e.queue.ReopenSucceeded(ctx, reducerWorkItemID(intent)); err != nil {
			return fmt.Errorf("reopen succeeded advisory group projection: %w", err)
		}
	}
	return nil
}
