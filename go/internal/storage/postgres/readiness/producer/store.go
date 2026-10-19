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

// RunsOnIntentStatesQuery counts each repository's pending RUNS_ON intents of
// the shared-intent producer domains (#8169): shared_projection_intents rows
// with repository_id in $3, projection_domain in $4, completed_at NULL, and
// payload relationship_type RUNS_ON, with the newest created_at as the
// elapsed-bound anchor. repository_id equals the acceptance unit and
// payload.repo_id for repo_dependency rows (the runner rejects any other row,
// repo_dependency_projection_acceptance_cycle.go). It reads
// shared_projection_intents_repo_run_idx (repository_id, source_run_id,
// projection_domain, ...) and filters completed_at and the relationship type
// on the heap rows of the requested repositories only. Measured on 875,000
// intents (4,973 pending): pgbench mean 0.12 ms for one repository and 0.98 ms
// for fifty, never a sequential scan (see README.md).
const RunsOnIntentStatesQuery = `
SELECT intent.repository_id,
       count(*)               AS pending_count,
       max(intent.created_at) AS newest_created_at
FROM shared_projection_intents AS intent
WHERE intent.repository_id = ANY($3::text[])
  AND intent.projection_domain = ANY($4::text[])
  AND intent.completed_at IS NULL
  AND intent.payload->>'relationship_type' = 'RUNS_ON'
GROUP BY intent.repository_id
`

// RuntimeProducerAndRunsOnStatesQuery answers both halves of the fence in ONE
// statement, so both reads share one READ COMMITTED snapshot. The
// repo_dependency runner writes RUNS_ON, then reopens the repository's
// workload_materialization row, then marks the intent completed, each in its
// own commit; one snapshot therefore sees either the pending intent or the
// reopened producer row, never neither. Producer rows key on the repository
// scope id; RUNS_ON rows key on the repository id.
const RuntimeProducerAndRunsOnStatesQuery = `
SELECT 'producer' AS kind, producer.scope_id AS state_key,
       producer.pending_count, producer.retrying_count, producer.claimed_count, producer.running_count,
       producer.producer_count, producer.newest_in_flight_workable_at, producer.newest_in_flight_claim_until
FROM (` + RuntimeProducerStatesQuery + `) AS producer
UNION ALL
SELECT 'runs_on' AS kind, runs_on.repository_id AS state_key,
       runs_on.pending_count, 0, 0, 0, 0, runs_on.newest_created_at, NULL::timestamptz
FROM (` + RunsOnIntentStatesQuery + `) AS runs_on
`

// Store answers servicecatalog.RuntimeProducerReadiness from fact_work_items
// and shared_projection_intents. It holds no state; every call is one
// read-only statement.
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
	repoKeys := make([]string, 0, len(repoByScope))
	for _, scopeID := range scopeIDs {
		repoKeys = append(repoKeys, repoByScope[scopeID])
	}
	sharedDomains := make([]string, 0, 1)
	for _, domain := range servicecatalog.RuntimeSharedIntentProducerDomains() {
		sharedDomains = append(sharedDomains, string(domain))
	}
	rows, err := s.DB.QueryContext(ctx, RuntimeProducerAndRunsOnStatesQuery, scopeIDs, domains, repoKeys, sharedDomains)
	if err != nil {
		return nil, fmt.Errorf("query runtime producer states: %w", err)
	}
	defer func() { _ = rows.Close() }()
	states := make(map[string]servicecatalog.RuntimeProducerState, len(scopeIDs))
	for rows.Next() {
		var kind, key string
		var pending, retrying, claimed, running, total int
		var workableAt, claimUntil sql.NullTime
		if err := rows.Scan(&kind, &key, &pending, &retrying, &claimed, &running, &total,
			&workableAt, &claimUntil); err != nil {
			return nil, fmt.Errorf("scan runtime producer state: %w", err)
		}
		switch kind {
		case "producer":
			repoID, ok := repoByScope[key]
			if !ok {
				return nil, fmt.Errorf("runtime producer state for unrequested scope %q", key)
			}
			state := states[repoID]
			state.Pending, state.Retrying, state.Claimed, state.Running, state.Total = pending, retrying, claimed, running, total
			if workableAt.Valid {
				state.NewestInFlightWorkableAt = workableAt.Time
			}
			if claimUntil.Valid {
				state.NewestInFlightClaimUntil = claimUntil.Time
			}
			states[repoID] = state
		case "runs_on":
			if _, ok := repoByScope[scope.GitRepositoryScopePrefix+key]; !ok {
				return nil, fmt.Errorf("runs_on intent state for unrequested repository %q", key)
			}
			state := states[key]
			state.RunsOnIntentsPending = pending
			if workableAt.Valid {
				state.NewestRunsOnIntentCreatedAt = workableAt.Time
			}
			states[key] = state
		default:
			return nil, fmt.Errorf("runtime producer state of unknown kind %q", kind)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate runtime producer states: %w", err)
	}
	return states, nil
}
