// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package producer

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/servicecatalog"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// RuntimeProducerStatesQuery counts each repository scope's producer work
// items at its ACTIVE generation, by status, and returns two timestamps over
// the pending, retrying, claimed, and running rows (each NULL when none is in
// flight):
//
//   - newest_in_flight_workable_at, the fence's elapsed-bound anchor: the max
//     of each row's last-workable time, GREATEST(COALESCE(reopened_at,
//     created_at), COALESCE(last_attempt_at, created_at),
//     COALESCE(visible_at, created_at), COALESCE(claim_until, created_at)).
//     That is the latest of the row's cycle start (enqueue or reopen), last
//     claim, scheduled visibility (a retry's next attempt), and lease expiry.
//   - newest_in_flight_claim_until, max(claim_until), for the log line.
//
// Each queue step moves one of those columns: a claim sets last_attempt_at
// and claim_until, a heartbeat extends claim_until, and a retry (counting or
// non-counting) sets visible_at and NULLs claim_until; ack, fail, and
// dead-letter NULL claim_until too, so claim_until is non-NULL only on claimed
// and running rows. A producer a worker keeps claiming, retrying, or
// heartbeating therefore never reaches the bound; only a starved row or a
// dead worker's row whose lease expired 30 minutes ago does. It never uses
// updated_at.
//
// It joins ingestion_scopes on its primary key to fact_work_items through
// fact_work_items_scope_generation_idx (scope_id, generation_id, ...), so it
// reads only the active generation's rows of each requested scope and filters
// stage and domain on that small set; last_attempt_at, visible_at, and
// claim_until are heap columns of rows already fetched. A newer pending
// generation's rows and an older generation's rows never match, and a scope
// with no active generation or no producer row returns no row. Measured on
// 1,057,631 fact_work_items rows: pgbench mean latency one scope 0.26 ms and
// fifty scopes 1.6 ms, within 3% of the A2 statement on the same data, never a
// sequential scan (see README.md).
const RuntimeProducerStatesQuery = `
SELECT scope.scope_id,
       count(*) FILTER (WHERE work.status = 'pending')  AS pending_count,
       count(*) FILTER (WHERE work.status = 'retrying') AS retrying_count,
       count(*) FILTER (WHERE work.status = 'claimed')  AS claimed_count,
       count(*) FILTER (WHERE work.status = 'running')  AS running_count,
       count(*)                                          AS producer_count,
       max(GREATEST(COALESCE(work.reopened_at, work.created_at),
                    COALESCE(work.last_attempt_at, work.created_at),
                    COALESCE(work.visible_at, work.created_at),
                    COALESCE(work.claim_until, work.created_at)))
           FILTER (WHERE work.status IN ('pending', 'retrying', 'claimed', 'running'))
                                                         AS newest_in_flight_workable_at,
       max(work.claim_until)
           FILTER (WHERE work.status IN ('pending', 'retrying', 'claimed', 'running'))
                                                         AS newest_in_flight_claim_until
FROM ingestion_scopes AS scope
JOIN fact_work_items AS work
  ON work.scope_id = scope.scope_id
 AND work.generation_id = scope.active_generation_id
WHERE scope.scope_id = ANY($1::text[])
  AND work.stage = 'reducer'
  AND work.domain = ANY($2::text[])
GROUP BY scope.scope_id
`

// Store answers servicecatalog.RuntimeProducerReadiness from fact_work_items.
// It holds no state; every call is one read-only statement.
type Store struct {
	DB db.Queryer
}

// RuntimeProducerStates returns, per repository id, the counts of the
// producer domains' work items at the repository scope's active generation
// and the newest in-flight last-workable time and lease end (each zero when
// none applies).
// A repository with no producer row there is absent from the result. A
// repository id maps to the git-repository-scope:<repo_id> scope the git
// collector registers, which is where the WorkloadInstance producer runs.
func (s Store) RuntimeProducerStates(
	ctx context.Context,
	repoIDs []string,
	producerDomains []reducercontract.Domain,
) (map[string]servicecatalog.RuntimeProducerState, error) {
	if len(repoIDs) == 0 || len(producerDomains) == 0 {
		return nil, nil
	}
	if s.DB == nil {
		return nil, fmt.Errorf("runtime producer readiness store requires a database")
	}
	scopeIDs := make([]string, 0, len(repoIDs))
	repoByScope := make(map[string]string, len(repoIDs))
	for _, repoID := range repoIDs {
		repoID = strings.TrimSpace(repoID)
		if repoID == "" {
			continue
		}
		scopeID := scope.GitRepositoryScopePrefix + repoID
		if _, seen := repoByScope[scopeID]; seen {
			continue
		}
		repoByScope[scopeID] = repoID
		scopeIDs = append(scopeIDs, scopeID)
	}
	if len(scopeIDs) == 0 {
		return nil, nil
	}
	domains := make([]string, 0, len(producerDomains))
	for _, domain := range producerDomains {
		domains = append(domains, string(domain))
	}
	rows, err := s.DB.QueryContext(ctx, RuntimeProducerStatesQuery, scopeIDs, domains)
	if err != nil {
		return nil, fmt.Errorf("query runtime producer states: %w", err)
	}
	defer func() { _ = rows.Close() }()
	states := make(map[string]servicecatalog.RuntimeProducerState, len(scopeIDs))
	for rows.Next() {
		var scopeID string
		var state servicecatalog.RuntimeProducerState
		var workableAt, claimUntil sql.NullTime
		if err := rows.Scan(&scopeID, &state.Pending, &state.Retrying, &state.Claimed, &state.Running, &state.Total,
			&workableAt, &claimUntil); err != nil {
			return nil, fmt.Errorf("scan runtime producer state: %w", err)
		}
		if workableAt.Valid {
			state.NewestInFlightWorkableAt = workableAt.Time
		}
		if claimUntil.Valid {
			state.NewestInFlightClaimUntil = claimUntil.Time
		}
		repoID, ok := repoByScope[scopeID]
		if !ok {
			return nil, fmt.Errorf("runtime producer state for unrequested scope %q", scopeID)
		}
		states[repoID] = state
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate runtime producer states: %w", err)
	}
	return states, nil
}
