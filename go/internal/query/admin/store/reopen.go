// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package store

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"github.com/eshu-hq/eshu/go/internal/query/admin"
)

// reopenLockTimeout bounds how long a reopen waits on a row lock. The
// selection locks SKIP LOCKED, so this fires only on a relation-level wait
// (for example behind DDL); an admin repair must fail fast rather than wedge.
const reopenLockTimeout = "5s"

// Scope selection is the shared resolveScopeID (scope_selector.go): skip and
// reopen resolve the same selector to the same scope, and both fail closed
// when it matches more than one scope (#7732).

// reopenActiveGenerationQuery resolves the scope's active generation: the
// scope's pinned active_generation_id, else the newest generation row. It
// mirrors the backfill fan-in's activeScopeGenerationQuery
// (ingestion_backfill_generation_guard.go), restricted to one scope.
const reopenActiveGenerationQuery = `
SELECT COALESCE(scope.active_generation_id, generation.generation_id) AS generation_id
FROM scope_generations AS generation
LEFT JOIN ingestion_scopes AS scope
  ON scope.scope_id = generation.scope_id
WHERE generation.scope_id = $1
ORDER BY generation.ingested_at DESC, generation.generation_id DESC
LIMIT 1
`

// reopenReducerCandidatesQuery lists succeeded reducer rows for one
// scope-generation domain, oldest update first, bounded by the request limit.
const reopenReducerCandidatesQuery = `
SELECT work.work_item_id
FROM fact_work_items AS work
WHERE work.scope_id = $1
  AND work.generation_id = $2
  AND work.domain = $3
  AND work.stage = 'reducer'
  AND work.status = 'succeeded'
ORDER BY work.updated_at ASC, work.work_item_id ASC
LIMIT $4
`

// reopenReducerLockQuery takes the candidate rows under FOR UPDATE, skipping
// rows a concurrent worker or admin call holds. The caller re-checks every
// predicate in the UPDATE, so a row that changed between selection and lock
// is never reopened blind.
const reopenReducerLockQuery = `
SELECT work.work_item_id
FROM fact_work_items AS work
WHERE work.work_item_id = ANY($1)
ORDER BY work.work_item_id
FOR UPDATE SKIP LOCKED
`

// reopenReducerWorkQuery resets succeeded reducer rows to pending. The SET
// list is ReopenSucceededReducerSetClause, shared with the replay queries,
// so the lists cannot drift (#7731). The WHEN refs are bare column names:
// this is a single-table UPDATE, so they resolve identically to the former
// work.-qualified refs. The WHERE re-checks the full selection predicate
// after the lock, so EvalPlanQual drops rows a concurrent writer moved. It
// returns the reopened ids in stable order.
const reopenReducerWorkQuery = `
UPDATE fact_work_items AS work
SET ` + postgres.ReopenSucceededReducerSetClause + `
WHERE work.work_item_id = ANY($2)
  AND work.scope_id = $3
  AND work.generation_id = $4
  AND work.domain = $5
  AND work.stage = 'reducer'
  AND work.status = 'succeeded'
RETURNING work.work_item_id
`

// reopenIntentCandidatesQuery picks one completed intent row per acceptance
// unit for the scope: the newest completed row of the domain at an accepted
// (unit, source run). DISTINCT ON keeps the latest first by acceptance
// recency, then by intent recency. One row per unit is enough because the
// unit's cycle loads every row of the unit once any one of them is pending
// (ListAcceptanceUnitDomainIntents). FOR UPDATE cannot combine with DISTINCT,
// so the caller locks the picked ids in a second statement.
const reopenIntentCandidatesQuery = `
SELECT DISTINCT ON (intent.acceptance_unit_id)
    intent.intent_id,
    intent.acceptance_unit_id,
    intent.source_run_id
FROM shared_projection_intents AS intent
JOIN shared_projection_acceptance AS acceptance
  ON acceptance.scope_id = intent.scope_id
 AND acceptance.acceptance_unit_id = intent.acceptance_unit_id
 AND acceptance.source_run_id = intent.source_run_id
WHERE intent.scope_id = $1
  AND intent.projection_domain = $2
  AND intent.completed_at IS NOT NULL
ORDER BY intent.acceptance_unit_id ASC,
         acceptance.updated_at DESC,
         intent.created_at DESC,
         intent.intent_id DESC
LIMIT $3
`

// reopenIntentLockQuery takes the picked intent rows under FOR UPDATE,
// skipping rows a concurrent worker or admin call holds.
const reopenIntentLockQuery = `
SELECT intent.intent_id, intent.acceptance_unit_id, intent.source_run_id
FROM shared_projection_intents AS intent
WHERE intent.intent_id = ANY($1)
ORDER BY intent.intent_id
FOR UPDATE SKIP LOCKED
`

// reopenIntentsQuery clears completed_at so the partition workers drain the
// picked intents again, mirroring the refinalize reset's reopen (SET
// completed_at = NULL, never a delete: the payload is the drain's input).
// The completed_at IS NOT NULL guard makes the reported count mean
// "reopened": a row a concurrent drain already re-completed is left alone.
const reopenIntentsQuery = `
UPDATE shared_projection_intents AS intent
SET completed_at = NULL
WHERE intent.intent_id = ANY($1)
  AND intent.completed_at IS NOT NULL
RETURNING intent.intent_id, intent.acceptance_unit_id, intent.source_run_id
`

// ResolveReopenTarget resolves the scope selector to the canonical scope id
// and active generation without mutating anything.
func (s *postgresStore) ResolveReopenTarget(ctx context.Context, scopeID string) (string, string, error) {
	scope, err := s.reopenScopeID(ctx, scopeID)
	if err != nil {
		return "", "", err
	}
	generation, err := s.reopenActiveGeneration(ctx, scope)
	if err != nil {
		return "", "", err
	}
	return scope, generation, nil
}

// ReopenCompletedWork reopens completed reducer or shared-projection work for
// one domain and scope. It resolves the canonical scope id and the scope's
// active generation at run time, then reopens inside one transaction with a
// lock_timeout: succeeded reducer rows for the reducer domains, one
// completed intent row per acceptance unit for the intent domain. Unknown
// domains fail closed here as well as in the handler.
func (s *postgresStore) ReopenCompletedWork(ctx context.Context, f admin.ReopenFilter) (admin.ReopenResult, error) {
	var result admin.ReopenResult
	switch f.Domain {
	case admin.ReopenDomainRepoDependency, admin.ReopenDomainWorkloadMaterialization, admin.ReopenDomainSubmodulePin:
	default:
		return result, fmt.Errorf("reopen domain %q is not reopenable", f.Domain)
	}
	if s.beginner == nil {
		return result, fmt.Errorf("reopen requires a transaction-capable store")
	}

	scopeID, err := s.reopenScopeID(ctx, f.ScopeID)
	if err != nil {
		return result, err
	}
	generationID, err := s.reopenActiveGeneration(ctx, scopeID)
	if err != nil {
		return result, err
	}
	result.GenerationID = generationID

	// The store is authoritative for the bound: direct callers bypass
	// the handler, so a huge limit must not reopen an arbitrarily
	// large set in one transaction.
	limit := f.Limit
	if limit <= 0 || limit > admin.DefaultReopenLimit {
		limit = admin.DefaultReopenLimit
	}

	tx, err := s.beginner.Begin(ctx)
	if err != nil {
		return result, fmt.Errorf("begin reopen: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.ExecContext(ctx, "SET LOCAL lock_timeout = '"+reopenLockTimeout+"'"); err != nil {
		return result, fmt.Errorf("set reopen lock timeout: %w", err)
	}

	if f.Domain == admin.ReopenDomainRepoDependency {
		if err := s.reopenIntents(ctx, tx, scopeID, f.Domain, limit, &result); err != nil {
			return result, err
		}
	} else if err := s.reopenReducerWork(ctx, tx, scopeID, generationID, f.Domain, limit, &result); err != nil {
		return result, err
	}

	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf("commit reopen: %w", err)
	}
	committed = true
	// UPDATE ... RETURNING has no output order; sort so the response and
	// the ledger outcome are deterministic across runs.
	sort.Strings(result.ReducerWorkItemIDs)
	sort.Slice(result.Units, func(i, j int) bool {
		if result.Units[i].AcceptanceUnitID != result.Units[j].AcceptanceUnitID {
			return result.Units[i].AcceptanceUnitID < result.Units[j].AcceptanceUnitID
		}
		return result.Units[i].IntentID < result.Units[j].IntentID
	})
	result.IntentIDs = result.IntentIDs[:0]
	for _, unit := range result.Units {
		result.IntentIDs = append(result.IntentIDs, unit.IntentID)
	}
	return result, nil
}

// reopenScopeID resolves the scope selector to the canonical scope id
// through the shared skip/reopen resolver.
func (s *postgresStore) reopenScopeID(ctx context.Context, scope string) (string, error) {
	return s.resolveScopeID(ctx, scope)
}

// reopenActiveGeneration resolves the scope's active generation.
func (s *postgresStore) reopenActiveGeneration(ctx context.Context, scopeID string) (string, error) {
	rows, err := s.database.QueryContext(ctx, reopenActiveGenerationQuery, scopeID)
	if err != nil {
		return "", fmt.Errorf("resolve reopen generation: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return "", fmt.Errorf("resolve reopen generation: %w", err)
		}
		return "", admin.ErrReopenNoActiveGeneration
	}
	var generationID sql.NullString
	if err := rows.Scan(&generationID); err != nil {
		return "", fmt.Errorf("scan reopen generation: %w", err)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("read reopen generation: %w", err)
	}
	if !generationID.Valid || strings.TrimSpace(generationID.String) == "" {
		return "", admin.ErrReopenNoActiveGeneration
	}
	return generationID.String, nil
}

// reopenReducerWork reopens succeeded reducer rows for one scope-generation
// domain: select candidates, lock them skipping locked rows, then update with
// the full predicate re-checked.
func (s *postgresStore) reopenReducerWork(
	ctx context.Context,
	tx db.ExecQueryer,
	scopeID, generationID, domain string,
	limit int,
	result *admin.ReopenResult,
) error {
	candidates, err := queryStrings(ctx, tx, reopenReducerCandidatesQuery, scopeID, generationID, domain, limit)
	if err != nil {
		return fmt.Errorf("select reopen reducer candidates: %w", err)
	}
	if len(candidates) == 0 {
		return nil
	}
	locked, err := queryStrings(ctx, tx, reopenReducerLockQuery, candidates)
	if err != nil {
		return fmt.Errorf("lock reopen reducer candidates: %w", err)
	}
	if len(locked) == 0 {
		return nil
	}
	now := s.time()
	rows, err := tx.QueryContext(ctx, reopenReducerWorkQuery, now, locked, scopeID, generationID, domain)
	if err != nil {
		return fmt.Errorf("reopen reducer work: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("scan reopened reducer work: %w", err)
		}
		result.ReducerWorkItemIDs = append(result.ReducerWorkItemIDs, id)
	}
	return rows.Err()
}

// reopenIntents reopens one completed intent row per acceptance unit: pick
// the rows, lock them skipping locked rows, then clear completed_at with the
// guard re-checked.
func (s *postgresStore) reopenIntents(
	ctx context.Context,
	tx db.ExecQueryer,
	scopeID, domain string,
	limit int,
	result *admin.ReopenResult,
) error {
	picks, err := queryReopenedUnits(ctx, tx, reopenIntentCandidatesQuery, scopeID, domain, limit)
	if err != nil {
		return fmt.Errorf("select reopen intent candidates: %w", err)
	}
	if len(picks) == 0 {
		return nil
	}
	ids := make([]string, 0, len(picks))
	for _, pick := range picks {
		ids = append(ids, pick.IntentID)
	}
	locked, err := queryReopenedUnits(ctx, tx, reopenIntentLockQuery, ids)
	if err != nil {
		return fmt.Errorf("lock reopen intent candidates: %w", err)
	}
	if len(locked) == 0 {
		return nil
	}
	lockedIDs := make([]string, 0, len(locked))
	for _, row := range locked {
		lockedIDs = append(lockedIDs, row.IntentID)
	}
	rows, err := tx.QueryContext(ctx, reopenIntentsQuery, lockedIDs)
	if err != nil {
		return fmt.Errorf("reopen intents: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var unit admin.ReopenedUnit
		if err := rows.Scan(&unit.IntentID, &unit.AcceptanceUnitID, &unit.SourceRunID); err != nil {
			return fmt.Errorf("scan reopened intents: %w", err)
		}
		result.IntentIDs = append(result.IntentIDs, unit.IntentID)
		result.Units = append(result.Units, unit)
	}
	return rows.Err()
}

// queryStrings runs a single-text-column select and returns the values.
func queryStrings(ctx context.Context, database db.ExecQueryer, query string, args ...any) ([]string, error) {
	rows, err := database.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, rows.Err()
}

// queryReopenedUnits runs a (intent_id, acceptance_unit_id, source_run_id)
// select and returns the rows.
func queryReopenedUnits(ctx context.Context, database db.ExecQueryer, query string, args ...any) ([]admin.ReopenedUnit, error) {
	rows, err := database.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []admin.ReopenedUnit
	for rows.Next() {
		var unit admin.ReopenedUnit
		if err := rows.Scan(&unit.IntentID, &unit.AcceptanceUnitID, &unit.SourceRunID); err != nil {
			return nil, err
		}
		out = append(out, unit)
	}
	return out, rows.Err()
}
