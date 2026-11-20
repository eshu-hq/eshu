// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/config/artifacts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
)

// purgeKeyedDelete is one leftover delete: the census table it clears, its
// statement, and the arguments. The ledger reports the affected-row count
// under the table name.
type purgeKeyedDelete struct {
	table string
	query string
	args  []any
}

// deletePurgeLeftovers deletes the no-FK keyed leftover rows that sit BEFORE
// the scope delete and returns the deleted-row ledger. Grant rows (and the
// service evidence snapshots) cascade, so the ledger pre-counts them here;
// everything else reports its own delete count. The advisory locks come first
// so the purge waits out an in-flight derive before it touches a row that
// derive rewrites, within the request's derive-lock budget. The impact
// write-admission delete belongs before the scope cascade to match the impact
// writer's lock order (see purgeDeleteWriteAdmissionQuery); the pass-state
// delete and the statements listed in deletePurgeAfterCascade are the opposite
// and stay after it.
func deletePurgeLeftovers(
	ctx context.Context,
	tx db.Transaction,
	repoID string,
	scopeIDs []string,
	budget *deriveLockBudget,
) (map[string]int64, time.Duration, error) {
	ledger := make(map[string]int64)
	cascaded, err := scanGrantCounts(ctx, tx, scopeIDs)
	if err != nil {
		return nil, 0, err
	}
	for table, count := range cascaded {
		ledger[table] = count
	}
	lockWait, err := lockPurgeDerivedModels(ctx, tx, repoID, budget)
	if err != nil {
		return ledger, lockWait, err
	}
	for _, keyed := range purgeLeftoverDeletes(repoID, scopeIDs) {
		deleted, err := execCount(ctx, tx, keyed.query, keyed.args...)
		if err != nil {
			return ledger, lockWait, fmt.Errorf("retirement purge delete %s: %w", keyed.table, err)
		}
		ledger[keyed.table] = deleted
	}
	return ledger, lockWait, nil
}

// purgeLeftoverDeletes lists the leftover deletes in execution order. The
// order carries three constraints, each pinned by a test:
//
//   - the content_entities delete precedes the infra dirty-mark delete, which
//     the content delete's row trigger would otherwise re-create;
//   - the key-sidecar and write-admission positions are unchanged from #10800;
//   - the relationship evidence and candidate tables delete by generation id
//     while scope_generations still exists, before the scope cascade.
//     resolved_relationships is not deleted: it is append-only by contract
//     (TestResolvedRelationshipsStayAppendOnly, #8283) and is a census
//     deferred row.
func purgeLeftoverDeletes(repoID string, scopeIDs []string) []purgeKeyedDelete {
	return []purgeKeyedDelete{
		{"repository_reindex_requests", purgeDeleteReindexRequestsQuery, []any{scopeIDs}},
		{"repository_selection_observations", purgeDeleteSelectionObservationsQuery, []any{scopeIDs}},
		{"content_files", purgeDeleteContentFilesQuery, []any{repoID}},
		{"content_entities", purgeDeleteContentEntitiesQuery, []any{repoID}},
		{"content_file_references", purgeDeleteContentReferencesQuery, []any{repoID}},
		{"repository_refs", purgeDeleteRepositoryRefsQuery, []any{repoID}},
		{"code_function_fingerprint", purgeDeleteFingerprintsQuery, []any{repoID}},
		{"code_fingerprint_band", purgeDeleteFingerprintBandsQuery, []any{repoID}},
		{"content_file_ref_deltas", purgeDeleteContentRefDeltasQuery, []any{repoID}},
		{"content_file_ref_delta_bases", purgeDeleteContentRefDeltaBasesQuery, []any{repoID}},
		{"function_summaries", purgeDeleteFunctionSummariesQuery, []any{repoID}},
		{"function_sources", purgeDeleteFunctionSourcesQuery, []any{repoID}},
		{"function_graph_ids", purgeDeleteFunctionGraphIDsQuery, []any{repoID}},
		{"infra_resource_entities", purgeDeleteInfraEntitiesQuery, []any{repoID}},
		{"infra_resource_entity_dirty_repos", purgeDeleteInfraDirtyReposQuery, []any{repoID}},
		{"config_artifact_paths", purgeDeleteConfigArtifactPathsQuery, []any{repoID}},
		{"shared_projection_intents", purgeDeleteSharedIntentsQuery, []any{repoID, scopeIDs}},
		{"shared_projection_unroutable_intents", purgeDeleteUnroutableIntentsQuery, []any{repoID, scopeIDs}},
		{"collector_generation_dead_letters", purgeDeleteDeadLettersQuery, []any{scopeIDs}},
		{"supply_chain_impact_write_admission", purgeDeleteWriteAdmissionQuery, []any{scopeIDs}},
		{"reducer_readiness_waits", purgeDeleteReadinessWaitsQuery, []any{scopeIDs}},
		{"reducer_waiting_items", purgeDeleteWaitingItemsQuery, []any{scopeIDs}},
		{"projection_decision_evidence", purgeDeleteProjectionDecisionEvidenceQuery, []any{repoID}},
		{"projection_decisions", purgeDeleteProjectionDecisionsQuery, []any{repoID}},
		{"crossplane_satisfied_by_redrive_state", purgeDeleteCrossplaneRedriveStateQuery, []any{scopeIDs}},
		{"crossplane_satisfied_by_redrive_target_ledger", purgeDeleteCrossplaneRedriveLedgerQuery, []any{scopeIDs}},
		{"service_materialization_generations", purgeDeleteServiceMaterializationQuery, []any{scopeIDs}},
		{"relationship_evidence_facts", purgeDeleteRelationshipEvidenceQuery, []any{scopeIDs}},
		{"relationship_candidates", purgeDeleteRelationshipCandidatesQuery, []any{scopeIDs}},
	}
}

// purgeDeriveLockBudget bounds one wait for a derive advisory lock. It matches
// the config-artifact derive deadline (config/artifacts/derive.go), so a purge
// never waits longer than one derive is allowed to run.
const purgeDeriveLockBudget = 10 * time.Second

// purgeRequestDeriveLockBudget bounds all the derive-lock waiting of one
// PurgeRetired call. PostgreSQL applies lock_timeout to each lock attempt, not
// to the transaction, so the per-lock bound alone leaves a 25-repository request
// up to 25 x 2 x 10 s = 500 s of waiting. 30 s is the margin the fence deadline
// (retirementFenceDeadline, 5 min 30 s) leaves inside the API server's 6 min
// write deadline.
const purgeRequestDeriveLockBudget = 30 * time.Second

// purgeDeriveLockFloor is the smallest lock_timeout the purge sets. Once the
// request budget is spent, each remaining repository still gets one short
// attempt per lock: a free lock purges, a held one reports derive_lock_busy.
// PostgreSQL reads a lock_timeout of 0 as "never time out", so the purge never
// sets less than this.
const purgeDeriveLockFloor = 100 * time.Millisecond

// purgeBoundDeriveLockSQL saves the transaction's current lock_timeout in a
// transaction-local setting, then sets the first derive-lock wait ($1, a
// lockTimeoutSetting value). The saved value is read inside the FROM subquery so
// it is captured before the wait replaces it.
const purgeBoundDeriveLockSQL = `SELECT set_config('lock_timeout', $1, true)
FROM (SELECT set_config('eshu.purge_prior_lock_timeout', current_setting('lock_timeout'), true)) AS saved`

// purgeSetLockTimeoutSQL sets the second derive-lock wait ($1) and leaves the
// saved value alone. It must not be purgeBoundDeriveLockSQL: by then
// current_setting('lock_timeout') is the first wait, so saving again would make
// the restore leave that wait on the deletes.
const purgeSetLockTimeoutSQL = `SELECT set_config('lock_timeout', $1, true)`

// purgeRestoreLockTimeoutSQL puts back the lock_timeout purgeBoundDeriveLockSQL
// saved, so the deletes after the locks keep the wait behavior they had.
const purgeRestoreLockTimeoutSQL = `SELECT set_config('lock_timeout', current_setting('eshu.purge_prior_lock_timeout'), true)`

// deriveLockBudget is the derive-lock waiting one PurgeRetired call has left.
// The call owns it and runs its repositories one after another, so it needs no
// lock. Never store it on a RetirementPurger: concurrent requests share the
// purger, and a request that spent its budget must not shorten the next one.
type deriveLockBudget struct {
	now       func() time.Time
	remaining time.Duration
}

func newDeriveLockBudget(now func() time.Time) *deriveLockBudget {
	return &deriveLockBudget{now: now, remaining: purgeRequestDeriveLockBudget}
}

// timeout is the lock_timeout for the next lock statement: what is left of the
// request budget, at most purgeDeriveLockBudget and at least
// purgeDeriveLockFloor. It is never zero and never above one lock wait, so a
// lockTimeoutSetting of it is always a real bound.
func (b *deriveLockBudget) timeout() time.Duration {
	return min(max(b.remaining, purgeDeriveLockFloor), purgeDeriveLockBudget)
}

// spend subtracts the time one lock statement took since started, whether it
// acquired the lock or timed out. A clock that steps backwards never refunds
// budget.
func (b *deriveLockBudget) spend(started time.Time) {
	if elapsed := b.now().Sub(started); elapsed > 0 {
		b.remaining -= elapsed
	}
}

// deriveLockBusyError reports that a derive advisory lock stayed held past the
// purge's wait for it. Nothing has been written when it is returned, so the
// transaction rolls back clean and the fenced tombstone stays open for a retry.
type deriveLockBusyError struct {
	lock string
	// wait is the time this repository spent on its derive locks so far.
	wait time.Duration
	// timeout is the lock_timeout in force for the lock that timed out: the
	// full per-lock wait, less as the request budget runs down, or the floor.
	timeout time.Duration
	err     error
}

func (e *deriveLockBusyError) Error() string {
	return fmt.Sprintf("retirement purge: %s derive lock still held after %s: %v", e.lock, e.wait, e.err)
}

func (e *deriveLockBusyError) Unwrap() error { return e.err }

// lockPurgeDerivedModels takes the repository's two derive advisory locks
// before the purge deletes any content row: the infra inventory lock, in the
// order retention takes it (lock, then content rows), and the config-artifact
// lock every derive and walker write takes first. A derive holds one of them
// for its whole write transaction, so the purge waits for it to commit and then
// deletes what it wrote. Taking a lock after the content delete would hold
// content rows while waiting on a lock a retention batch holds before it
// needs those rows. The locks release with the purge transaction.
//
// Each wait is bounded by a transaction-local lock_timeout taken from the
// request's budget just before the lock statement (see deriveLockBudget), and
// the statement's elapsed time comes off the budget whether it acquired the
// lock or timed out. The first setting saves the transaction's prior
// lock_timeout; the restore puts it back once both locks are held. A timeout
// (55P03) returns a *deriveLockBusyError; the purge reports it as the designed
// derive_lock_busy exit. The time spent on the locks is returned for the
// completion log.
func lockPurgeDerivedModels(
	ctx context.Context,
	tx db.Transaction,
	repoID string,
	budget *deriveLockBudget,
) (time.Duration, error) {
	started := budget.now()
	for _, step := range []struct {
		name  string
		bound string
		lock  func(context.Context, db.Executor, string) error
	}{
		{"infra inventory", purgeBoundDeriveLockSQL, inventory.LockRepository},
		{"config artifact", purgeSetLockTimeoutSQL, artifacts.LockRepository},
	} {
		timeout := budget.timeout()
		if _, err := tx.ExecContext(ctx, step.bound, lockTimeoutSetting(timeout)); err != nil {
			return budget.now().Sub(started), fmt.Errorf("retirement purge: bound derive lock wait: %w", err)
		}
		attempt := budget.now()
		err := step.lock(ctx, tx, repoID)
		budget.spend(attempt)
		if err != nil {
			wait := budget.now().Sub(started)
			return wait, classifyDeriveLockError(step.name, wait, timeout, err)
		}
	}
	if _, err := tx.ExecContext(ctx, purgeRestoreLockTimeoutSQL); err != nil {
		return budget.now().Sub(started), fmt.Errorf("retirement purge: restore lock timeout: %w", err)
	}
	return budget.now().Sub(started), nil
}

// classifyDeriveLockError turns a lock timeout into the blocked-exit error and
// leaves every other failure a plain wrapped error.
func classifyDeriveLockError(lock string, wait, timeout time.Duration, err error) error {
	if isLockTimeoutError(err) {
		return &deriveLockBusyError{lock: lock, wait: wait, timeout: timeout, err: err}
	}
	return fmt.Errorf("retirement purge: %w", err)
}

// deletePurgeAfterCascade deletes the leftover rows whose position is after the
// ingestion_scopes delete, and adds their counts to the ledger:
//
//   - package_manifest_consumption_key_dirty_scopes: the scope cascade deletes
//     the scope's dependency Variable fact_records rows, and the AFTER DELETE
//     trigger package_manifest_consumption_keys_dirty_delete (migration 139)
//     marks the scope dirty once per row unless the session carries the
//     sidecar marker. The key-sidecar prune earlier in this transaction sets
//     that marker, so the cascade currently writes no mark; the delete stays
//     after the cascade so it does not depend on that coupling;
//   - relationship_generations: the claim variant of the generation activation
//     locks the claimant fact_work_items row before it writes these rows, and
//     the cascade has locked the work rows by now, so the purge takes them in
//     the writer's order. The relationship evidence and candidate
//     deletes already ran in the leftover sweep: they read this table's
//     generation ids and must precede this delete.
func deletePurgeAfterCascade(
	ctx context.Context,
	tx db.Transaction,
	scopeIDs []string,
	ledger map[string]int64,
) error {
	for _, keyed := range []purgeKeyedDelete{
		{"package_manifest_consumption_key_dirty_scopes", purgeDeleteConsumptionDirtyScopesQuery, []any{scopeIDs}},
		{"relationship_generations", purgeDeleteRelationshipGenerationsQuery, []any{scopeIDs}},
	} {
		deleted, err := execCount(ctx, tx, keyed.query, keyed.args...)
		if err != nil {
			return fmt.Errorf("retirement purge delete %s: %w", keyed.table, err)
		}
		ledger[keyed.table] = deleted
	}
	return nil
}

// scanGrantCounts pre-counts the rows the scope delete and the leftover
// service-generation delete cascade (the four scope-FK grant tables and the
// service evidence snapshots), in one bounded statement.
func scanGrantCounts(ctx context.Context, tx db.Transaction, scopeIDs []string) (map[string]int64, error) {
	rows, err := tx.QueryContext(ctx, purgeGrantCountsQuery, scopeIDs)
	if err != nil {
		return nil, fmt.Errorf("retirement purge grant counts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("retirement purge grant counts read: %w", err)
		}
		return nil, fmt.Errorf("retirement purge grant counts: no row returned")
	}
	var scope, repository, roleScope, roleRepository, snapshots int64
	if err := rows.Scan(&scope, &repository, &roleScope, &roleRepository, &snapshots); err != nil {
		return nil, fmt.Errorf("retirement purge grant counts scan: %w", err)
	}
	return map[string]int64{
		"tenant_scope_grants":              scope,
		"tenant_repository_grants":         repository,
		"identity_role_scope_targets":      roleScope,
		"identity_role_repository_targets": roleRepository,
		"service_evidence_snapshots":       snapshots,
	}, rows.Err()
}
