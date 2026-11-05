// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	lockstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/lock"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// A derived scope belongs to a repository without carrying it in its partition
// key (#10054). The Go reachability scope's partition key is the planner's
// fairness key, shared by every repository of one scanner instance, so the only
// mapping is its scope id: this prefix followed by the repository id. Every
// selection below compares for equality on that built id. A prefix or LIKE match
// would reach repository:r_10 from repository:r_1, and the fairness key would
// erase every repository the instance scans.
const goReachabilityScopePrefix = "go-reachability://"

// Fragments shared by every statement that resolves a repository's scope set, so
// the fence, the purge, the capture, and the preview cannot disagree on it.
const (
	// retirementScopesOfReposSQL selects the scopes of the repositories in the
	// text[] parameter $1, as ingestion_scopes AS s. The derived ids are built
	// once as an uncorrelated array so the row filter stays one comparison.
	retirementScopesOfReposSQL = `(s.partition_key = ANY($1) OR s.scope_id = ANY(ARRAY(
			SELECT '` + goReachabilityScopePrefix + `' || derived.repo_id FROM unnest($1::text[]) AS derived(repo_id))))`
	// retirementScopesOfRepoSQL is the single-repository form over text $1.
	retirementScopesOfRepoSQL = `(s.partition_key = $1 OR s.scope_id = '` + goReachabilityScopePrefix + `' || $1::text)`
	// retirementScopeRepoIDSQL names the repository a scope row of s belongs to,
	// so a budget breakdown charges a derived scope to its repository and never
	// to a fairness key no operator can act on.
	retirementScopeRepoIDSQL = `CASE WHEN starts_with(s.scope_id, '` + goReachabilityScopePrefix + `')
			THEN substr(s.scope_id, length('` + goReachabilityScopePrefix + `') + 1)
			ELSE s.partition_key END`
)

const (
	// derivedParentOpenRetirementQuery reads the parent repository's open
	// tombstone through the partial open-repo index. Any open row refuses,
	// complete included: only the repository's own commit may readmit it.
	derivedParentOpenRetirementQuery = `SELECT state FROM repository_retirements
		WHERE repo_id = $1 AND readmitted_at IS NULL LIMIT 1`
	// purgeDerivedFactCountQuery pre-counts the derived scope's facts for the
	// ledger, one prefix range of fact_records_scope_generation_idx. The scope
	// delete cascades them and reports nothing of its own.
	purgeDerivedFactCountQuery = `SELECT count(*) FROM fact_records WHERE scope_id = ANY($1)`
)

// Refused-commit outcomes on eshu_dp_repository_retirement_derived_commits_refused_total:
// a closed set. retiring is a scan racing an open retirement, which is expected
// and short. retired is a claim still landing after the purge, which keeps
// costing scanner work until the queue drains.
const (
	derivedCommitOutcomeRetiring = "retiring"
	derivedCommitOutcomeRetired  = "retired"
)

// derivedParentRepoID returns the repository id inside a derived scope id. It
// reports false for every other scope id, including the bare prefix.
func derivedParentRepoID(scopeID string) (string, bool) {
	repoID, ok := strings.CutPrefix(scopeID, goReachabilityScopePrefix)
	if !ok || repoID == "" {
		return "", false
	}
	return repoID, true
}

// retirementRepoKey names the repository a scope commit belongs to for retirement
// purposes: the parent for a derived scope, the partition key for any other.
func retirementRepoKey(scopeValue scope.IngestionScope) string {
	if parent, ok := derivedParentRepoID(scopeValue.ScopeID); ok {
		return parent
	}
	return scopeValue.PartitionKey
}

// acquireCommitSharedBarrier takes the commit's shared advisory barrier. A derived
// scope also takes its parent repository's key: its first scan's scope row is
// uncommitted, so the fence's row locks cannot see it, and only this key makes the
// fence wait until the row is visible and purgeable. The keys go in the sorted
// order the exclusive callers use, so a maintenance leader holding the parent and
// waiting for the fairness key cannot deadlock with this commit. Any other scope
// sends the one statement it always sent.
func acquireCommitSharedBarrier(ctx context.Context, tx db.ExecQueryer, scopeValue scope.IngestionScope) error {
	keys := []string{lockstore.DeferredMaintenanceRepoLockKey(scopeValue)}
	if parent, ok := derivedParentRepoID(scopeValue.ScopeID); ok {
		keys = lockstore.SortedUniqueRepoKeys(append(keys, parent))
	}
	for _, key := range keys {
		if err := lockstore.AcquireDeferredMaintenanceRepoSharedLock(ctx, tx, key); err != nil {
			return err
		}
	}
	return nil
}

// gateDerivedParent refuses a derived commit whose parent repository has an open
// tombstone. It runs after the shared barrier, under READ COMMITTED, so its fresh
// statement snapshot sees every marker committed before this commit took the
// parent key; a marker committed later finds the fence waiting behind this commit.
// It returns the refusal outcome, empty when the commit is admitted.
func gateDerivedParent(ctx context.Context, tx db.ExecQueryer, parent string) (string, error) {
	rows, err := tx.QueryContext(ctx, derivedParentOpenRetirementQuery, parent)
	if err != nil {
		return "", fmt.Errorf("read parent retirement: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return "", fmt.Errorf("read parent retirement: %w", err)
		}
		return "", nil
	}
	var state string
	if err := rows.Scan(&state); err != nil {
		return "", fmt.Errorf("read parent retirement scan: %w", err)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("read parent retirement rows: %w", err)
	}
	if state == RetirementStateComplete {
		return derivedCommitOutcomeRetired, nil
	}
	return derivedCommitOutcomeRetiring, nil
}

// gateAndUpsertIngestionScope runs the retirement gates and writes the scope row.
// A derived scope's own partition key never names a tombstone, so its parent is
// gated first; every other scope goes straight to the folded gate.
func (s IngestionStore) gateAndUpsertIngestionScope(
	ctx context.Context,
	tx db.ExecQueryer,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
) (scopeGateOutcome, string, error) {
	if parent, ok := derivedParentRepoID(scopeValue.ScopeID); ok {
		outcome, err := gateDerivedParent(ctx, tx, parent)
		if err != nil {
			return scopeGateRefusedRetiring, "", err
		}
		if outcome != "" {
			s.recordDerivedCommitRefused(ctx, outcome)
			return scopeGateRefusedRetiring, "", nil
		}
	}
	return upsertIngestionScope(ctx, tx, scopeValue, generation)
}

// recordDerivedCommitRefused counts one refused derived commit. The count lands
// before the rollback, and a failed rollback is the only way it overstates: that
// path already returns an error the claim retries. Nil instruments are a no-op.
func (s IngestionStore) recordDerivedCommitRefused(ctx context.Context, outcome string) {
	if s.Instruments == nil || s.Instruments.DerivedScopeCommitsRefused == nil {
		return
	}
	s.Instruments.DerivedScopeCommitsRefused.Add(ctx, 1,
		metric.WithAttributes(telemetry.AttrOutcome(outcome)))
}

// derivedScopesOf returns the locked scope ids that are this repository's derived
// scopes, by the exact id built from the repository.
func derivedScopesOf(repoID string, scopeIDs []string) []string {
	want := goReachabilityScopePrefix + repoID
	var derived []string
	for _, scopeID := range scopeIDs {
		if scopeID == want {
			derived = append(derived, scopeID)
		}
	}
	return derived
}

// addDerivedLedgerCounts names the repository's derived scopes and their facts in
// the deleted-row ledger and returns those scopes. The fact count is a pre-count:
// the scope delete cascades the facts and reports nothing of its own. A repository
// with no derived scope runs no statement and adds no entry.
func addDerivedLedgerCounts(
	ctx context.Context,
	tx db.Transaction,
	repoID string,
	scopeIDs []string,
	ledger map[string]int64,
) ([]string, error) {
	derived := derivedScopesOf(repoID, scopeIDs)
	if len(derived) == 0 {
		return nil, nil
	}
	factCount, err := scanSingleCount(ctx, tx, purgeDerivedFactCountQuery, derived)
	if err != nil {
		return nil, fmt.Errorf("retirement purge count derived facts: %w", err)
	}
	ledger["derived_ingestion_scopes"] = int64(len(derived))
	ledger["derived_fact_records"] = factCount
	return derived, nil
}

// recordDerivedScopesPurged counts the derived scopes a committed purge removed.
// Like the completion counter it lands after the commit, so a crash between the
// two under-counts rather than double-counts. Nil instruments are a no-op.
func recordDerivedScopesPurged(ctx context.Context, instruments *telemetry.Instruments, count int64) {
	if count == 0 || instruments == nil || instruments.DerivedScopesPurged == nil {
		return
	}
	instruments.DerivedScopesPurged.Add(ctx, count)
}

// recordRetirementPurged counts one completed operator retirement on
// eshu_dp_repository_retirements_purged_total (#7766). Nil instruments are
// a no-op. The count lands after the purge commit, so a crash between
// commit and emission under-counts rather than double-counts.
func recordRetirementPurged(ctx context.Context, instruments *telemetry.Instruments) {
	if instruments == nil || instruments.OperatorRepositoryRetirementsPurged == nil {
		return
	}
	instruments.OperatorRepositoryRetirementsPurged.Add(ctx, 1)
}
