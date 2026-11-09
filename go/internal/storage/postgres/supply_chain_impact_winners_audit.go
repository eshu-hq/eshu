// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
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

// selectWinnersAuditCandidatesSQL is the split-scan candidate statement of
// #10416 slice 1: the legacy candidate body as a pure single-statement SELECT,
// with no DDL. The legacy CREATE TEMP TABLE AS cannot ride a read store (its
// surface has no Exec), so the stale reader runs this statement and the
// differing identifiers are drained into memory (O(candidates) idents; see
// winnersAuditStaleSeedBatchSize), then seeded in 1000-row INSERT batches.
// The recompute it diffs is the same
// shipped winner select the legacy path uses, so both branches name the same
// candidate set: legacy path parity (the stale live proof mirrors the legacy
// one) guards the twin.
var selectWinnersAuditCandidatesSQL = `WITH winners_now AS (` + supplyChainImpactWinnerSelectSQL + `)
` + winnersAuditDiffSelect("winners_now", "supply_chain_impact_canonical_winners")

// createWinnersAuditStaleIdentsSQL builds the empty identifier table the split
// scan seeds. The name is literal, like every sibling temp-table statement,
// so it stays greppable; it must stay identical to
// winnersKeyedIdentsTable, which the verification it feeds references. The
// five text columns match the legacy CTAS, so every downstream statement runs
// unchanged.
const createWinnersAuditStaleIdentsSQL = `CREATE TEMP TABLE winners_keyed_idents (
    package_id text, repository_id text, subject_digest text, c0 text, advisory_id text
) ON COMMIT DROP`

// seedWinnersAuditStaleIdentsSQL appends one batch of candidate identifiers to
// the verification table. It mirrors the dirty-log capture's unnest shape; a
// NULL array element seeds SQL NULL, preserving the legacy temp-table copy's
// nulls instead of coercing them to empty.
const seedWinnersAuditStaleIdentsSQL = `
INSERT INTO ` + winnersKeyedIdentsTable + `
    (package_id, repository_id, subject_digest, c0, advisory_id)
SELECT package_id, repository_id, subject_digest, c0, advisory_id
FROM unnest($1::text[], $2::text[], $3::text[], $4::text[], $5::text[])
     AS ident(package_id, repository_id, subject_digest, c0, advisory_id)
`

// winnersAuditStaleSeedBatchSize bounds one ident-seed INSERT's argument
// arrays. It does not bound peak memory: the stale scan drains into one
// slice before any writer contact, so peak is O(candidates) idents — 120
// bytes of headers plus five identifier strings each (100k candidates ≈ 12
// MB of headers, tens of MB with contents; whole-table divergence is the
// unbounded worst case). The drain buys the phase split: standby cursor,
// connection, and permit are released before the writer transaction opens,
// an empty scan never touches the writer, and no lock is held while reading
// the standby. It is a batching bound, not a candidate cap: all batches run,
// so a divergent table can never defer a key forever.
const winnersAuditStaleSeedBatchSize = 1000

// staleReadDeferral matches a refused stale read without importing the access
// package: the access type answers StaleReadDeferred, and errors.As finds it
// through the private failure wrapper. Only a refusal defers; any other stale
// failure stays an error.
type staleReadDeferral interface {
	StaleReadDeferred() bool
}

func asStaleReadDeferral(err error) bool {
	var deferral staleReadDeferral
	return errors.As(err, &deferral) && deferral.StaleReadDeferred()
}

// WithStaleCandidates routes the audit's candidate scan to stale: the
// differing identifiers are drained into memory (O(candidates) idents; see
// winnersAuditStaleSeedBatchSize) instead of a writer temp table,
// then seeded in 1000-row INSERT batches for the unchanged
// under-lock verification. A refused stale read defers the tick with no writer
// contact and no fallback. Set it only on the Auditor instance: the Rebuilder,
// KeyedRebuilder, and DirtyLogReader instances share this struct but must
// never carry a stale source, since their reads feed locks, claims, and
// gauges in the same cycle. It leaves the receiver unchanged.
func (s SupplyChainImpactWinnersStore) WithStaleCandidates(stale db.Queryer) SupplyChainImpactWinnersStore {
	s.staleCandidates = stale
	return s
}

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
	// The split scan (#10416 slice 1) reads candidates from the stale reader
	// before any writer contact; the legacy path below is byte-intact when no
	// stale source is wired.
	if s.staleCandidates != nil {
		return s.auditWinnersSplit(ctx)
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

// WinnersAuditIdent is one candidate identifier for the under-lock keyed
// verification. Nulls are preserved so the seed matches the legacy
// temp-table copy exactly. The staged standby scan (#10451) resolves the
// same identifiers from compacted winners instead of the candidate cursor.
type WinnersAuditIdent struct {
	PackageID     sql.NullString
	RepositoryID  sql.NullString
	SubjectDigest sql.NullString
	C0            sql.NullString
	AdvisoryID    sql.NullString
}

// scanWinnersAuditIdents drains the stale candidate cursor. Like its row-count
// sibling it closes the cursor and reports a cursor failure as a read error.
func scanWinnersAuditIdents(rows db.Rows) ([]WinnersAuditIdent, error) {
	defer func() { _ = rows.Close() }()
	var out []WinnersAuditIdent
	for rows.Next() {
		var ident WinnersAuditIdent
		if err := rows.Scan(&ident.PackageID, &ident.RepositoryID, &ident.SubjectDigest, &ident.C0, &ident.AdvisoryID); err != nil {
			return nil, fmt.Errorf("scan supply chain impact winners audit candidates: %w", err)
		}
		out = append(out, ident)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read supply chain impact winners audit candidates: %w", err)
	}
	return out, nil
}

// nullStringArg renders a scanned identifier column for the unnest seed: an
// invalid value seeds SQL NULL.
func nullStringArg(value sql.NullString) any {
	if !value.Valid {
		return nil
	}
	return value.String
}

// seedWinnersAuditStaleIdents appends every candidate identifier to the
// verification table one bounded batch per INSERT. Only each INSERT's
// argument arrays are batch-bounded; the slice already holds every candidate.
func seedWinnersAuditStaleIdents(ctx context.Context, tx db.Transaction, idents []WinnersAuditIdent) error {
	for start := 0; start < len(idents); start += winnersAuditStaleSeedBatchSize {
		end := min(start+winnersAuditStaleSeedBatchSize, len(idents))
		batch := idents[start:end]
		packageIDs := make([]any, 0, len(batch))
		repositoryIDs := make([]any, 0, len(batch))
		subjectDigests := make([]any, 0, len(batch))
		c0s := make([]any, 0, len(batch))
		advisoryIDs := make([]any, 0, len(batch))
		for _, ident := range batch {
			packageIDs = append(packageIDs, nullStringArg(ident.PackageID))
			repositoryIDs = append(repositoryIDs, nullStringArg(ident.RepositoryID))
			subjectDigests = append(subjectDigests, nullStringArg(ident.SubjectDigest))
			c0s = append(c0s, nullStringArg(ident.C0))
			advisoryIDs = append(advisoryIDs, nullStringArg(ident.AdvisoryID))
		}
		if _, err := tx.ExecContext(ctx, seedWinnersAuditStaleIdentsSQL,
			packageIDs, repositoryIDs, subjectDigests, c0s, advisoryIDs); err != nil {
			return fmt.Errorf("verify supply chain impact winners audit candidates: %w", err)
		}
	}
	return nil
}

// auditWinnersSplit runs the audit with the candidate scan on the stale reader.
// The scan takes no lock; only identifiers the under-lock verification still
// finds stale are queued, so a key that changed between the scan and the lock
// is dropped, never counted. A refused stale read defers with no writer
// contact; an empty scan returns before the writer transaction begins. The
// claim, verification, pending check, queueing, and commit below are the same
// statements the legacy path runs.
func (s SupplyChainImpactWinnersStore) auditWinnersSplit(
	ctx context.Context,
) (supplychaincore.SupplyChainImpactWinnersAudit, error) {
	var none supplychaincore.SupplyChainImpactWinnersAudit
	started := time.Now()
	rows, err := s.staleCandidates.QueryContext(ctx, selectWinnersAuditCandidatesSQL)
	if err != nil {
		if asStaleReadDeferral(err) {
			return supplychaincore.SupplyChainImpactWinnersAudit{Deferred: true, ReadDuration: time.Since(started)}, nil
		}
		return none, fmt.Errorf("read supply chain impact winners audit candidates: %w", err)
	}
	idents, err := scanWinnersAuditIdents(rows)
	if err != nil {
		return none, err
	}
	read := time.Since(started)
	if len(idents) == 0 {
		return supplychaincore.SupplyChainImpactWinnersAudit{ReadDuration: read}, nil
	}
	return s.VerifyStagedCandidates(ctx, idents, read)
}
