// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package producer

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/securityalert"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// ImpactProducerStateQuery counts one scope generation's supply_chain_impact
// work items by status and returns the newest in-flight last-workable time
// (NULL when none is in flight) (#8827).
//
// It is an aggregate with no GROUP BY, so it always returns exactly one row:
// all zeros when no impact work was ever enqueued for the generation. It reads
// fact_work_items through fact_work_items_scope_generation_idx (scope_id,
// generation_id, ...) with an equality on both columns, so only the one scope
// generation's rows are fetched and stage and domain filter that small set.
// The anchor is the same last-workable expression as
// [RuntimeProducerStatesQuery]: GREATEST(COALESCE(reopened_at, created_at),
// COALESCE(last_attempt_at, created_at), COALESCE(visible_at, created_at),
// COALESCE(claim_until, created_at)) over the pending, retrying, claimed, and
// running rows. Each queue step moves one of those columns, so a producer a
// worker keeps claiming, retrying, or heartbeating never reaches the bound;
// updated_at is never used.
const ImpactProducerStateQuery = `
SELECT count(*) FILTER (WHERE work.status = 'pending')  AS pending_count,
       count(*) FILTER (WHERE work.status = 'retrying') AS retrying_count,
       count(*) FILTER (WHERE work.status = 'claimed')  AS claimed_count,
       count(*) FILTER (WHERE work.status = 'running')  AS running_count,
       count(*)                                          AS producer_count,
       max(GREATEST(COALESCE(work.reopened_at, work.created_at),
                    COALESCE(work.last_attempt_at, work.created_at),
                    COALESCE(work.visible_at, work.created_at),
                    COALESCE(work.claim_until, work.created_at)))
           FILTER (WHERE work.status IN ('pending', 'retrying', 'claimed', 'running'))
                                                         AS newest_in_flight_workable_at
FROM fact_work_items AS work
WHERE work.scope_id = $1
  AND work.generation_id = $2
  AND work.stage = 'reducer'
  AND work.domain = $3
`

// ImpactStore answers securityalert.ImpactProducerReadiness from
// fact_work_items. It holds no state; every call is one read-only statement.
type ImpactStore struct {
	DB db.Queryer
}

// ImpactProducerState returns the status counts and the newest in-flight
// last-workable time of the supply_chain_impact work of one scope generation.
// An error means the question could not be answered; it is never reported as an
// empty state, which would read as "no impact work in flight".
func (s ImpactStore) ImpactProducerState(
	ctx context.Context,
	scopeID string,
	generationID string,
) (securityalert.ImpactProducerState, error) {
	if s.DB == nil {
		return securityalert.ImpactProducerState{}, fmt.Errorf("impact producer readiness store requires a database")
	}
	scopeID, generationID = strings.TrimSpace(scopeID), strings.TrimSpace(generationID)
	if scopeID == "" || generationID == "" {
		return securityalert.ImpactProducerState{}, fmt.Errorf("impact producer readiness requires a scope and generation id")
	}
	rows, err := s.DB.QueryContext(ctx, ImpactProducerStateQuery, scopeID, generationID,
		string(reducercontract.DomainSupplyChainImpact))
	if err != nil {
		return securityalert.ImpactProducerState{}, fmt.Errorf("query impact producer state: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return securityalert.ImpactProducerState{}, fmt.Errorf("read impact producer state: %w", err)
		}
		return securityalert.ImpactProducerState{}, fmt.Errorf("impact producer state query returned no row")
	}
	var state securityalert.ImpactProducerState
	var workableAt sql.NullTime
	if err := rows.Scan(&state.Pending, &state.Retrying, &state.Claimed, &state.Running, &state.Total, &workableAt); err != nil {
		return securityalert.ImpactProducerState{}, fmt.Errorf("scan impact producer state: %w", err)
	}
	if workableAt.Valid {
		state.NewestInFlightWorkableAt = workableAt.Time
	}
	if err := rows.Err(); err != nil {
		return securityalert.ImpactProducerState{}, fmt.Errorf("read impact producer state: %w", err)
	}
	return state, nil
}
