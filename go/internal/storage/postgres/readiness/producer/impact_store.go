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

// ImpactProducerStateQuery counts one scope generation's reducer work items of
// one domain ($3) by status and returns the newest in-flight last-workable
// time (NULL when none is in flight). Two callers bind $3: the #8827
// reconciliation fence binds supply_chain_impact, and the #11105 impact
// attachment wait binds sbom_attestation_attachment. The succeeded and failed
// (dead_letter or failed) counts tell a settled producer, a genuine no-match,
// from one that stopped without its evidence (#8956); a superseded or other
// terminal row is in neither count and so is never settled.
//
// It is an aggregate with no GROUP BY, so it always returns exactly one row:
// all zeros when no work of the domain was ever enqueued for the generation. It reads
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
       count(*) FILTER (WHERE work.status = 'succeeded') AS succeeded_count,
       count(*) FILTER (WHERE work.status IN ('dead_letter', 'failed')) AS failed_count,
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
	return s.producerState(ctx, scopeID, generationID, reducercontract.DomainSupplyChainImpact)
}

// SBOMAttestationAttachmentState returns the same status counts and anchor for
// the sbom_attestation_attachment work of one scope generation (#11105). The
// supply_chain_impact handler reads it before its evidence load so an impact
// pass never runs while its own generation's attachment is still in flight.
// The statement is [ImpactProducerStateQuery] with the domain bound to
// sbom_attestation_attachment, so the plan and the anchor are the #8827 ones.
// An error is never reported as an empty state.
func (s ImpactStore) SBOMAttestationAttachmentState(
	ctx context.Context,
	scopeID string,
	generationID string,
) (securityalert.ImpactProducerState, error) {
	return s.producerState(ctx, scopeID, generationID, reducercontract.DomainSBOMAttestationAttachment)
}

// producerState runs [ImpactProducerStateQuery] for one reducer domain of one
// scope generation.
func (s ImpactStore) producerState(
	ctx context.Context,
	scopeID string,
	generationID string,
	domain reducercontract.Domain,
) (securityalert.ImpactProducerState, error) {
	if s.DB == nil {
		return securityalert.ImpactProducerState{}, fmt.Errorf("%s producer readiness store requires a database", domain)
	}
	scopeID, generationID = strings.TrimSpace(scopeID), strings.TrimSpace(generationID)
	if scopeID == "" || generationID == "" {
		return securityalert.ImpactProducerState{}, fmt.Errorf("%s producer readiness requires a scope and generation id", domain)
	}
	rows, err := s.DB.QueryContext(ctx, ImpactProducerStateQuery, scopeID, generationID, string(domain))
	if err != nil {
		return securityalert.ImpactProducerState{}, fmt.Errorf("query %s producer state: %w", domain, err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return securityalert.ImpactProducerState{}, fmt.Errorf("read %s producer state: %w", domain, err)
		}
		return securityalert.ImpactProducerState{}, fmt.Errorf("%s producer state query returned no row", domain)
	}
	var state securityalert.ImpactProducerState
	var workableAt sql.NullTime
	if err := rows.Scan(&state.Pending, &state.Retrying, &state.Claimed, &state.Running,
		&state.Succeeded, &state.Failed, &state.Total, &workableAt); err != nil {
		return securityalert.ImpactProducerState{}, fmt.Errorf("scan %s producer state: %w", domain, err)
	}
	if workableAt.Valid {
		state.NewestInFlightWorkableAt = workableAt.Time
	}
	if err := rows.Err(); err != nil {
		return securityalert.ImpactProducerState{}, fmt.Errorf("read %s producer state: %w", domain, err)
	}
	return state, nil
}
