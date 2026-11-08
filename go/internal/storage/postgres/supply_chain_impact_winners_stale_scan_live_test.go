// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	pgaccess "github.com/eshu-hq/eshu/go/internal/runtime/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// Live proofs for the #10416 slice-1 winners-audit split scan: the slow
// candidate SELECT rides a stale reader while verify, repair queueing, and
// commit stay on the writer transaction unchanged.

// cannedStaleRows replays fabricated candidate rows: idents the stale side
// saw but the writer side no longer holds (or never held), proving the
// under-lock verification drops what changed before the lock.
type cannedStaleRows struct {
	rows [][]any
	pos  int
}

func (r *cannedStaleRows) Next() bool { return r.pos < len(r.rows) }

func (r *cannedStaleRows) Scan(dest ...any) error {
	if r.pos >= len(r.rows) {
		return sql.ErrNoRows
	}
	row := r.rows[r.pos]
	r.pos++
	if len(dest) != len(row) {
		return errors.New("canned stale candidate row width mismatch")
	}
	for i, d := range dest {
		nullable := d.(*sql.NullString)
		if row[i] == nil {
			*nullable = sql.NullString{}
			continue
		}
		*nullable = sql.NullString{String: row[i].(string), Valid: true}
	}
	return nil
}

func (r *cannedStaleRows) Err() error { return nil }

func (r *cannedStaleRows) Close() error { return nil }

// cannedStaleQueryer is a stubbed stale candidate source.
type cannedStaleQueryer struct {
	rows    [][]any
	err     error
	queries int
}

func (f *cannedStaleQueryer) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	f.queries++
	if f.err != nil {
		return nil, f.err
	}
	return &cannedStaleRows{rows: f.rows}, nil
}

// staleScanBeginner counts writer-transaction opens and logs writer Exec
// statements: the split scan must not begin one when there is nothing to
// verify or when the stale read refuses.
type staleScanBeginner struct {
	inner  postgres.SQLDB
	begins int
	execs  []string
}

func (c *staleScanBeginner) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	return c.inner.QueryContext(ctx, query, args...)
}

func (c *staleScanBeginner) Begin(ctx context.Context) (db.Transaction, error) {
	c.begins++
	tx, err := c.inner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &staleScanTx{inner: tx, log: &c.execs}, nil
}

type staleScanTx struct {
	inner db.Transaction
	log   *[]string
}

func (t *staleScanTx) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	return t.inner.QueryContext(ctx, query, args...)
}

func (t *staleScanTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	*t.log = append(*t.log, query)
	return t.inner.ExecContext(ctx, query, args...)
}

func (t *staleScanTx) Commit() error { return t.inner.Commit() }

func (t *staleScanTx) Rollback() error { return t.inner.Rollback() }

// staleAuditStore wires one harness database with a stale candidate source
// and an optional recording writer.
func staleAuditStore(h *keyedHarness, stale db.Queryer, writer db.Queryer) postgres.SupplyChainImpactWinnersStore {
	if writer == nil {
		writer = postgres.SQLDB{DB: h.db}
	}
	return postgres.NewSupplyChainImpactWinnersStore(writer).WithStaleCandidates(stale)
}

// TestSupplyChainImpactWinnersResweepStaleScanQueuesVerifiedTrueLive proves the
// split scan's positive case: a missed capture found by the stale candidate
// scan, verified under the writer lock, is queued exactly once with source
// "audit" and repaired by the next keyed tick.
func TestSupplyChainImpactWinnersResweepStaleScanQueuesVerifiedTrueLive(t *testing.T) {
	h := newKeyedHarness(t, "#10416 slice-1 stale scan positive proof")
	scopeA, _ := keyedSeedSettled(h)

	h.exec(`UPDATE fact_records SET payload = jsonb_set(payload, '{priority_score}', '99')
WHERE scope_id = $1 AND payload->>'cve_id' = 'CVE-2026-2002'`, scopeA)
	audit, err := staleAuditStore(h, postgres.SQLDB{DB: h.db}, nil).AuditWinners(h.ctx)
	if err != nil || audit.Skipped || audit.Deferred {
		t.Fatalf("stale audit = %+v, %v", audit, err)
	}
	if audit.Repairs != 1 {
		t.Fatalf("stale audit repairs = %d, want exactly the one stale key", audit.Repairs)
	}
	if depth := h.dirtyDepth(); depth != 1 {
		t.Fatalf("dirty log depth after the stale audit = %d, want the one repaired key queued", depth)
	}
	if got := h.count(`SELECT count(*) FROM supply_chain_impact_winners_dirty_log WHERE source = 'audit'`); got != 1 {
		t.Fatalf("audit-sourced dirty rows = %d, want 1", got)
	}
	h.keyed()
	h.assertParity("after the stale audit repair")
	again, err := staleAuditStore(h, postgres.SQLDB{DB: h.db}, nil).AuditWinners(h.ctx)
	if err != nil || again.Repairs != 0 {
		t.Fatalf("second stale audit = %+v, %v; want zero repairs once the keyed tick repaired the key", again, err)
	}
}

// TestSupplyChainImpactWinnersResweepStaleScanDropsChangedBeforeLockLive proves the
// verify gate: a candidate the stale side saw but the writer side no longer
// holds is dropped, not counted. The canned row stands in for a key that
// changed between the stale read and the writer lock.
func TestSupplyChainImpactWinnersResweepStaleScanDropsChangedBeforeLockLive(t *testing.T) {
	h := newKeyedHarness(t, "#10416 slice-1 stale scan verify proof")
	keyedSeedSettled(h)

	stale := &cannedStaleQueryer{rows: [][]any{
		{"pkg:nope/nothing", "repo-9805", "digest-nope", "CVE-2099-0001", "ADV-NOPE"},
	}}
	before := h.dirtyDepth()
	audit, err := staleAuditStore(h, stale, nil).AuditWinners(h.ctx)
	if err != nil || audit.Skipped || audit.Deferred {
		t.Fatalf("stale audit = %+v, %v", audit, err)
	}
	if audit.Repairs != 0 {
		t.Fatalf("stale audit repairs = %d, want 0: the fabricated candidate is clean under the lock", audit.Repairs)
	}
	if after := h.dirtyDepth(); after != before {
		t.Fatalf("dirty log depth moved from %d to %d: the dropped candidate must queue nothing", before, after)
	}
	if stale.queries != 1 {
		t.Fatalf("stale statements = %d, want exactly the candidate SELECT", stale.queries)
	}
}

// TestSupplyChainImpactWinnersResweepStaleScanSkipsPendingLive proves an already
// pending key is not a capture hole on the split path either.
func TestSupplyChainImpactWinnersResweepStaleScanSkipsPendingLive(t *testing.T) {
	h := newKeyedHarness(t, "#10416 slice-1 stale scan pending proof")
	scopeA, scopeB := keyedSeedSettled(h)

	h.write(scopeA, scopeA+"/g1", keyedFinding("CVE-2026-2001", "pkg:npm/x", 50), keyedFinding("CVE-2026-2002", "pkg:npm/y", 5))
	h.addGeneration(scopeB, scopeB+"/g2", "pending")
	h.write(scopeB, scopeB+"/g2", keyedFinding("CVE-2026-2003", "pkg:npm/z", 1))
	h.activate(scopeB, scopeB+"/g1", scopeB+"/g2")
	pending := h.dirtyDepth()
	audit, err := staleAuditStore(h, postgres.SQLDB{DB: h.db}, nil).AuditWinners(h.ctx)
	if err != nil || audit.Skipped || audit.Deferred {
		t.Fatalf("stale audit over pending changes = %+v, %v", audit, err)
	}
	if audit.Repairs != 0 {
		t.Fatalf("stale audit repairs = %d, want 0: pending keys are not capture holes", audit.Repairs)
	}
	if after := h.dirtyDepth(); after != pending {
		t.Fatalf("dirty log depth moved from %d to %d: pending keys must not be requeued", pending, after)
	}
	h.keyed()
	h.assertParity("after pending keys")
}

// TestSupplyChainImpactWinnersResweepStaleScanZeroCandidatesOpensNoWriterTxLive
// proves an empty stale read ends the tick before any writer transaction
// begins.
func TestSupplyChainImpactWinnersResweepStaleScanZeroCandidatesOpensNoWriterTxLive(t *testing.T) {
	h := newKeyedHarness(t, "#10416 slice-1 stale scan empty proof")
	keyedSeedSettled(h)

	writer := &staleScanBeginner{inner: postgres.SQLDB{DB: h.db}}
	audit, err := staleAuditStore(h, postgres.SQLDB{DB: h.db}, writer).AuditWinners(h.ctx)
	if err != nil || audit.Skipped || audit.Deferred {
		t.Fatalf("stale audit of a settled table = %+v, %v", audit, err)
	}
	if audit.Repairs != 0 {
		t.Fatalf("stale audit repairs = %d, want 0", audit.Repairs)
	}
	if writer.begins != 0 {
		t.Fatalf("writer begins = %d, want none: zero candidates must not open a writer tx", writer.begins)
	}
	if len(writer.execs) != 0 {
		t.Fatalf("writer execs = %d, want none", len(writer.execs))
	}
}

// TestSupplyChainImpactWinnersResweepStaleDeferralEndsTickLive proves a refused stale
// read ends the tick as a typed deferred outcome with no writer contact: no
// writer tx begins, nothing is queued, and the outcome is neither skipped nor
// an error.
func TestSupplyChainImpactWinnersResweepStaleDeferralEndsTickLive(t *testing.T) {
	h := newKeyedHarness(t, "#10416 slice-1 stale deferral proof")
	keyedSeedSettled(h)

	stale := &cannedStaleQueryer{err: pgaccess.StaleDeferral{
		Family: pgaccess.StaleFamilyAudits,
		Reason: pgaccess.StaleDeferralBudgetExceeded,
	}}
	before := h.dirtyDepth()
	writer := &staleScanBeginner{inner: postgres.SQLDB{DB: h.db}}
	audit, err := staleAuditStore(h, stale, writer).AuditWinners(h.ctx)
	if err != nil {
		t.Fatalf("deferred stale audit error = %v, want nil: a deferral is an outcome, not a failure", err)
	}
	if !audit.Deferred || audit.Skipped || audit.Repairs != 0 {
		t.Fatalf("deferred stale audit = %+v, want a deferred outcome only", audit)
	}
	if writer.begins != 0 || len(writer.execs) != 0 {
		t.Fatalf("writer contact begins=%d execs=%d, want none", writer.begins, len(writer.execs))
	}
	if after := h.dirtyDepth(); after != before {
		t.Fatalf("dirty log depth moved from %d to %d: a deferral queues nothing", before, after)
	}
	if stale.queries != 1 {
		t.Fatalf("stale statements = %d, want exactly the refused candidate SELECT", stale.queries)
	}
}

// TestSupplyChainImpactWinnersResweepStaleScanOverOneSeedBatchLive proves batching:
// more candidates than one seed batch all queue, across one INSERT per batch.
func TestSupplyChainImpactWinnersResweepStaleScanOverOneSeedBatchLive(t *testing.T) {
	h := newKeyedHarness(t, "#10416 slice-1 stale seed batch proof")
	keyedSeedSettled(h)

	const bogus = 1001
	h.exec(`INSERT INTO supply_chain_impact_canonical_winners (
    canonical_key, winner_fact_id, winner_scope_id, finding_id, priority_score,
    source_count, impact_status, ecosystem, severity_bucket, repository_id,
    cve_id, advisory_id, package_id, subject_digest, image_ref, priority_bucket,
    detection_profile, observed_version, match_reason, suppression_state, suppression_expires_at,
    service_ids, workload_ids, environments, materialized_at)
SELECT 'stale-batch-' || g,
    winner_fact_id, winner_scope_id, finding_id, priority_score,
    source_count, impact_status, ecosystem, severity_bucket, repository_id,
    cve_id, advisory_id, 'pkg-batch-' || g, subject_digest, image_ref, priority_bucket,
    detection_profile, observed_version, match_reason, suppression_state, suppression_expires_at,
    service_ids, workload_ids, environments, now()
FROM (SELECT winner_fact_id, winner_scope_id, finding_id, priority_score,
    source_count, impact_status, ecosystem, severity_bucket, repository_id,
    cve_id, advisory_id, package_id, subject_digest, image_ref, priority_bucket,
    detection_profile, observed_version, match_reason, suppression_state, suppression_expires_at,
    service_ids, workload_ids, environments
    FROM supply_chain_impact_canonical_winners LIMIT 1) AS one
CROSS JOIN generate_series(1, $1) AS g`, bogus)

	writer := &staleScanBeginner{inner: postgres.SQLDB{DB: h.db}}
	audit, err := staleAuditStore(h, postgres.SQLDB{DB: h.db}, writer).AuditWinners(h.ctx)
	if err != nil || audit.Skipped || audit.Deferred {
		t.Fatalf("stale audit over %d bogus rows = %+v, %v", bogus, audit, err)
	}
	if audit.Repairs != bogus {
		t.Fatalf("stale audit repairs = %d, want all %d bogus rows queued", audit.Repairs, bogus)
	}
	var seeds int
	for _, stmt := range writer.execs {
		if strings.Contains(stmt, "INSERT INTO winners_keyed_idents") {
			seeds++
		}
	}
	if seeds != 2 {
		t.Fatalf("ident seed batches = %d, want 2 for %d candidates: each INSERT is batch-bounded", seeds, bogus)
	}
	if got := h.count(`SELECT count(*) FROM supply_chain_impact_winners_dirty_log WHERE source = 'audit'`); got != bogus {
		t.Fatalf("audit-sourced dirty rows = %d, want %d", got, bogus)
	}
	// The winners table holds advisory_id NOT NULL, so no NULL flows from
	// this table; the seed's NULL mapping (invalid NullString to a nil
	// argument, matching the legacy temp-table copy) is proven at the
	// mapping level by the hermetic batch test. Transport of nil-bearing
	// unnest arrays through pgx is a tracked follow-up on #10416, proven
	// before stale-audits enables.
}

// TestSupplyChainImpactWinnersResweepStaleScanCandidateExplainLive runs EXPLAIN
// (ANALYZE, BUFFERS) of the split-scan candidate SELECT on fixture data. The
// plan is quoted in the slice-1 handoff as the read-shape evidence: the scan
// must stay a recompute-and-diff over the stored winners, not a new join
// constellation.
func TestSupplyChainImpactWinnersResweepStaleScanCandidateExplainLive(t *testing.T) {
	h := newKeyedHarness(t, "#10416 slice-1 candidate scan plan proof")
	keyedSeedSettled(h)

	rows, err := h.db.QueryContext(h.ctx, "EXPLAIN (ANALYZE, BUFFERS) "+postgres.WinnersAuditCandidateSelectSQLForTest)
	if err != nil {
		t.Fatalf("explain candidate SELECT: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan explain line: %v", err)
		}
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("explain rows: %v", err)
	}
	plan := strings.Join(lines, "\n")
	t.Logf("candidate SELECT plan on fixture data:\n%s", plan)
	if !strings.Contains(plan, "supply_chain_impact_canonical_winners") {
		t.Fatalf("candidate plan never touches the stored winners:\n%s", plan)
	}
}
