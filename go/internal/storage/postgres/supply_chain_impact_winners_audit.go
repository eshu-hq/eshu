// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"time"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// The audit backstop of the keyed winners resweep (#9805). The dirty log is the
// only thing a keyed tick reads, so a change class that leaves no log row would
// leave a stale winner forever. The audit recomputes the whole table outside the
// resweep lock, finds the keys that differ from the stored winners, and then
// verifies each under the lock with a fresh keyed recompute before it counts
// one: a key that changed between the read and the lock is not a capture hole.
// A verified stale key that is not already waiting in the log is queued with
// source "audit" for the next keyed tick, and counted as a repair.

// createWinnersAuditCandidatesSQL reads the full recompute and keeps the
// identifier of every key whose winner differs from the stored row, is missing
// from the table, or is stored but no longer produced. It takes no lock beyond
// MVCC reads, so it never blocks a keyed tick or a writer.
var createWinnersAuditCandidatesSQL = `
CREATE TEMP TABLE winners_audit_candidates ON COMMIT DROP AS
WITH winners_now AS (` + supplyChainImpactWinnerSelectSQL + `)
` + winnersAuditDiffSelect("winners_now", "supply_chain_impact_canonical_winners")

// winnersAuditDiffSelect renders the full-join diff of a recomputed set against
// a stored set and projects the identifier of each differing key.
func winnersAuditDiffSelect(now, stored string) string {
	return `SELECT DISTINCT
    COALESCE(n.package_id, w.package_id) AS package_id,
    COALESCE(n.repository_id, w.repository_id) AS repository_id,
    COALESCE(n.subject_digest, w.subject_digest) AS subject_digest,
    COALESCE(NULLIF(COALESCE(n.cve_id, w.cve_id), ''), NULLIF(COALESCE(n.advisory_id, w.advisory_id), ''), '') AS c0,
    COALESCE(n.advisory_id, w.advisory_id) AS advisory_id
FROM ` + now + ` AS n
FULL JOIN ` + stored + ` AS w
  ON w.canonical_key = n.canonical_key
WHERE n.canonical_key IS NULL
   OR w.canonical_key IS NULL
   OR ` + supplyChainImpactWinnerRowExpr("n") + ` IS DISTINCT FROM ` + supplyChainImpactWinnerRowExpr("w")
}

// seedWinnersAuditIdentsSQL copies the candidates into the identifier table the
// keyed build reads, so the verification reuses the keyed statements unchanged.
const seedWinnersAuditIdentsSQL = `
CREATE TEMP TABLE winners_keyed_idents ON COMMIT DROP AS
SELECT package_id, repository_id, subject_digest, c0, advisory_id FROM ` + winnersAuditCandidatesTable

// createWinnersAuditStaleSQL verifies the candidates under the lock: it
// recomputes only their keys and diffs them against only their stored rows.
var createWinnersAuditStaleSQL = `
CREATE TEMP TABLE winners_audit_stale ON COMMIT DROP AS
WITH winners_now AS (` + winnersKeyedSelectSQL + `),
stored AS (
    SELECT * FROM supply_chain_impact_canonical_winners AS w
    WHERE ` + winnersStoredIdentMatch + `
)
` + winnersAuditDiffSelect("winners_now", "stored")

// createWinnersAuditPendingSQL collects the identifiers already waiting in the
// dirty log: its fact rows directly, and its scope rows through the same
// expansion a keyed tick applies. A stale key in this set is being repaired by
// the next tick, so it is not a capture hole.
var createWinnersAuditPendingSQL = `
CREATE TEMP TABLE winners_audit_pending ON COMMIT DROP AS
SELECT package_id, repository_id, subject_digest, c0, advisory_id
FROM supply_chain_impact_winners_dirty_log
WHERE kind = 'fact'
UNION
` + winnersScopeExpansionSelect("supply_chain_impact_winners_dirty_log")

// queueWinnersAuditRepairsSQL appends the verified stale keys that are not
// already pending to the dirty log as source "audit" and reports how many.
const queueWinnersAuditRepairsSQL = `
INSERT INTO supply_chain_impact_winners_dirty_log
    (kind, source, package_id, repository_id, subject_digest, c0, advisory_id)
SELECT 'fact', 'audit', s.package_id, s.repository_id, s.subject_digest, s.c0, s.advisory_id
FROM ` + winnersAuditStaleTable + ` AS s
WHERE NOT EXISTS (
    SELECT 1 FROM ` + winnersAuditPendingTable + ` AS p
    WHERE (p.package_id, p.repository_id, p.subject_digest, p.c0, p.advisory_id)
        = (s.package_id, s.repository_id, s.subject_digest, s.c0, s.advisory_id)
)
`

// countWinnersAuditCandidatesSQL counts the candidates the read found.
const countWinnersAuditCandidatesSQL = `SELECT count(*) FROM ` + winnersAuditCandidatesTable

// AuditWinners recomputes the whole winners table outside the resweep lock,
// verifies every differing key under the lock, and queues the verified stale
// keys that are not already pending. It returns Skipped, having queued nothing,
// when it cannot take the resweep lock; the next interval retries.
//
// The full recompute read is the cost of a full resweep without its write, and
// it holds no lock a keyed tick or a writer waits on. The lock is held only for
// the keyed verification of the candidates, which is bounded by how many keys
// differ, not by the table.
func (s SupplyChainImpactWinnersStore) AuditWinners(
	ctx context.Context,
) (supplychaincore.SupplyChainImpactWinnersAudit, error) {
	var none supplychaincore.SupplyChainImpactWinnersAudit
	if s.DB == nil {
		return none, fmt.Errorf("supply chain impact winners database is required")
	}
	beginner, ok := s.DB.(db.Beginner)
	if !ok {
		return none, fmt.Errorf("supply chain impact winners database must support transactions")
	}
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return none, fmt.Errorf("begin supply chain impact winners audit: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	started := time.Now()
	if _, err := tx.ExecContext(ctx, createWinnersAuditCandidatesSQL); err != nil {
		return none, fmt.Errorf("read supply chain impact winners audit candidates: %w", err)
	}
	read := time.Since(started)
	candidates, err := scanWinnersInt64(ctx, tx, countWinnersAuditCandidatesSQL, "count audit candidates")
	if err != nil {
		return none, err
	}
	if candidates == 0 {
		return supplychaincore.SupplyChainImpactWinnersAudit{ReadDuration: read}, nil
	}
	claimed, err := claimSupplyChainImpactWinnersResweep(ctx, tx)
	if err != nil {
		return none, err
	}
	if !claimed {
		return supplychaincore.SupplyChainImpactWinnersAudit{Skipped: true, ReadDuration: read}, nil
	}
	for _, stmt := range []string{
		seedWinnersAuditIdentsSQL, analyzeWinnersKeyedIdentsSQL,
		buildWinnersKeyedActiveFactsSQL, addWinnersKeyedOperatorFactsSQL, analyzeWinnersKeyedActiveFactsSQL,
		createWinnersAuditStaleSQL, createWinnersAuditPendingSQL,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return none, fmt.Errorf("verify supply chain impact winners audit candidates: %w", err)
		}
	}
	queued, err := tx.ExecContext(ctx, queueWinnersAuditRepairsSQL)
	if err != nil {
		return none, fmt.Errorf("queue supply chain impact winners audit repairs: %w", err)
	}
	repairs, err := queued.RowsAffected()
	if err != nil {
		return none, fmt.Errorf("read supply chain impact winners audit repairs: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return none, fmt.Errorf("commit supply chain impact winners audit: %w", err)
	}
	return supplychaincore.SupplyChainImpactWinnersAudit{Repairs: repairs, ReadDuration: read}, nil
}

var _ supplychaincore.SupplyChainImpactWinnersAuditor = SupplyChainImpactWinnersStore{}
