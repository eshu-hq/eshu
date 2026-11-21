// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// The #11117 A audit-gate proof (arbiter addendum item 4): the writer-side
// verify of the staged audit (VerifyStagedCandidates) at the QA backlog shape.
// Under A the first audit pass on QA can start with the whole backlog queued,
// so every key with an unapplied change is a candidate and all are already
// pending. The verify holds the resweep lock from its claim to its commit; a
// keyed tick in that window is Skipped. The bound is one keyed interval: 30 s.
//
// The knob selects the shape: unset or "smoke" is two percent of QA so the
// proof stays in the gate; "qa" is 80,000 stored winners, 50,000 differing
// candidates (all pending), 48,000 dirty-log rows and 374,000 staged rows, plus
// one queued scope mark over the whole generation in each queue; "wave" is the
// #11103 rename wave: 200,000 stored winners and 176,000 pending candidates.
const winnersAuditVerifyScaleEnv = "ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_AUDIT_VERIFY_SCALE"

type winnersAuditVerifyShape struct {
	label                           string
	winners, candidates, log, stage int
}

func winnersAuditVerifyShapeFor(t *testing.T) winnersAuditVerifyShape {
	t.Helper()
	switch knob := winnersScaleKnob(winnersAuditVerifyScaleEnv); knob {
	case "", "smoke":
		return winnersAuditVerifyShape{"smoke", 1600, 1000, 960, 7480}
	case "qa":
		return winnersAuditVerifyShape{"qa", 80000, 50000, 48000, 374000}
	case "wave":
		// The #11103 rename wave queues about 176,000 pending keys.
		return winnersAuditVerifyShape{"wave", 200000, 176000, 48000, 374000}
	default:
		t.Fatalf("%s=%q: want smoke, qa or wave", winnersAuditVerifyScaleEnv, knob)
		return winnersAuditVerifyShape{}
	}
}

// verifyTimingDB records when the transaction's resweep claim ran and when it
// committed, and runs onClaim right after the claim, while the verify holds
// the lock.
type verifyTimingDB struct {
	inner   postgres.SQLDB
	onClaim func()
	mu      *sync.Mutex
	claimAt *time.Time
	commit  *time.Time
}

func (d verifyTimingDB) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	return d.inner.QueryContext(ctx, query, args...)
}

func (d verifyTimingDB) Begin(ctx context.Context) (db.Transaction, error) {
	tx, err := d.inner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return verifyTimingTx{Transaction: tx, d: d}, nil
}

type verifyTimingTx struct {
	db.Transaction
	d verifyTimingDB
}

func (tx verifyTimingTx) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	rows, err := tx.Transaction.QueryContext(ctx, query, args...)
	if err == nil && strings.Contains(query, "pg_try_advisory_xact_lock") {
		if tx.d.onClaim != nil {
			tx.d.onClaim()
		}
		// Stamped after the probe, so the measured hold is the verify's own.
		tx.d.mu.Lock()
		*tx.d.claimAt = time.Now()
		tx.d.mu.Unlock()
	}
	return rows, err
}

func (tx verifyTimingTx) Commit() error {
	err := tx.Transaction.Commit()
	tx.d.mu.Lock()
	*tx.d.commit = time.Now()
	tx.d.mu.Unlock()
	return err
}

// TestSupplyChainImpactWinnersResweepAuditVerifyAtBacklogShapeLive measures
// the staged audit's writer verify from its lock claim to its commit at the
// backlog shape and requires it within one keyed interval (30 s). While the
// verify holds the lock, a keyed tick returns Skipped and a capture insert
// completes unblocked; after the commit the next keyed tick commits.
func TestSupplyChainImpactWinnersResweepAuditVerifyAtBacklogShapeLive(t *testing.T) {
	shape := winnersAuditVerifyShapeFor(t)
	h := newKeyedHarness(t, "#11117 audit verify backlog-shape proof")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
	t.Cleanup(cancel)
	h.ctx = ctx
	h.store.MaxKeyFraction, h.store.MaxIdentifiers = 0, 0

	seedStarted := time.Now()
	scope := "vuln-intel://11117/audit-verify"
	winnersLiveSeedScope(t, ctx, h.db, scope)
	// One fact per key: key g has package p(g/10), repository r(g/1000), CVE g.
	// Like the production writer, every payload carries subject_digest: the
	// keyed probe matches it by equality and cannot find a fact without it.
	h.exec(`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'f-' || g, $1, $1 || '/active', $2, 'f-' || g, '1.0.0', 'synthetic', 'f-' || g, now(), now(), false,
  jsonb_build_object('finding_id', 'finding:' || g, 'cve_id', 'CVE-2026-' || g, 'package_id', 'pkg:npm/p' || (g / 10),
    'impact_status', 'affected_exact', 'repository_id', 'repo-' || (g / 1000), 'subject_digest', '', 'ecosystem', 'npm',
    'priority_score', g % 90, 'observed_version', '1.0.0',
    'service_ids', '["svc-a"]'::jsonb, 'workload_ids', '["wl-a"]'::jsonb, 'environments', '["prod"]'::jsonb)
FROM generate_series(1, $3::int) AS g`, scope, facts.ReducerSupplyChainImpactFindingFactKind, shape.winners)
	h.exec(`ANALYZE fact_records`)
	if r, err := h.store.ResweepWinners(ctx); err != nil || r.Inserted != int64(shape.winners) {
		t.Fatalf("populating full = %+v, %v; want %d winners", r, err, shape.winners)
	}
	// Every candidate's fact changes, and every change is still queued: the
	// log holds the first shape.log keys, staging re-marks every candidate.
	h.exec(`UPDATE fact_records SET payload = jsonb_set(payload, '{priority_score}', '99')
WHERE fact_id IN (SELECT 'f-' || g FROM generate_series(1, $1::int) AS g)`, shape.candidates)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_log`)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_staging`)
	h.exec(`INSERT INTO supply_chain_impact_winners_dirty_log (kind, source, package_id, repository_id, subject_digest, c0, advisory_id)
SELECT 'fact', 'fact_upsert', 'pkg:npm/p' || (g / 10), 'repo-' || (g / 1000), '', 'CVE-2026-' || g, ''
FROM generate_series(1, $1::int) AS g`, shape.log)
	h.exec(`INSERT INTO supply_chain_impact_winners_dirty_staging (kind, source, package_id, repository_id, subject_digest, c0, advisory_id)
SELECT 'fact', 'fact_upsert', 'pkg:npm/p' || (k / 10), 'repo-' || (k / 1000), '', 'CVE-2026-' || k, ''
FROM (SELECT ((g - 1) % $2::int) + 1 AS k FROM generate_series(1, $1::int) AS g) AS marks`, shape.stage, shape.candidates)
	// A queued activation of the whole generation: the pending set expands
	// scope rows to every identifier the generation holds, its worst case.
	h.exec(`INSERT INTO supply_chain_impact_winners_dirty_log (kind, source, scope_id, generation_id) VALUES ('scope', 'activation', $1, $1 || '/active')`, scope)
	h.exec(`INSERT INTO supply_chain_impact_winners_dirty_staging (kind, source, scope_id, generation_id) VALUES ('scope', 'activation', $1, $1 || '/active')`, scope)
	h.exec(`ANALYZE supply_chain_impact_winners_dirty_log`)
	h.exec(`ANALYZE supply_chain_impact_winners_dirty_staging`)
	seeded := time.Since(seedStarted)

	idents := make([]postgres.WinnersAuditIdent, 0, shape.candidates)
	ns := func(v string) sql.NullString { return sql.NullString{String: v, Valid: true} }
	for g := 1; g <= shape.candidates; g++ {
		idents = append(idents, postgres.WinnersAuditIdent{
			PackageID: ns(fmt.Sprintf("pkg:npm/p%d", g/10)), RepositoryID: ns(fmt.Sprintf("repo-%d", g/1000)),
			SubjectDigest: ns(""), C0: ns(fmt.Sprintf("CVE-2026-%d", g)), AdvisoryID: ns(""),
		})
	}

	var mu sync.Mutex
	var claimAt, commitAt time.Time
	var keyedDuring error
	var keyedSkipped bool
	var captureTook time.Duration
	var captureErr error
	timed := verifyTimingDB{inner: postgres.SQLDB{DB: h.db}, mu: &mu, claimAt: &claimAt, commit: &commitAt, onClaim: func() {
		r, err := h.store.ResweepWinnersKeyed(ctx)
		keyedDuring, keyedSkipped = err, r.Skipped
		capCtx, capCancel := context.WithTimeout(ctx, 5*time.Second)
		defer capCancel()
		started := time.Now()
		_, captureErr = h.db.ExecContext(capCtx, `INSERT INTO supply_chain_impact_winners_dirty_staging (kind, source, package_id, repository_id, subject_digest, c0, advisory_id)
VALUES ('fact', 'fact_upsert', 'pkg:npm/p0', 'repo-0', '', 'CVE-2026-1', '')`)
		captureTook = time.Since(started)
	}}
	auditor := h.store
	auditor.DB = timed
	verifyStarted := time.Now()
	audit, err := auditor.VerifyStagedCandidates(ctx, idents, 0)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	total := time.Since(verifyStarted)
	if audit.Skipped {
		t.Fatalf("verify = %+v, want it to hold the lock", audit)
	}
	held := commitAt.Sub(claimAt)
	t.Logf("audit_verify shape=%s winners=%d candidates=%d log=%d staging=%d seed=%s verify_total=%s claim_to_commit=%s (%.3fs) repairs=%d capture_during=%s load=%s",
		shape.label, shape.winners, shape.candidates, shape.log, shape.stage, seeded, total, held, held.Seconds(), audit.Repairs, captureTook, winnersAuditVerifyLoad())

	if audit.Repairs != 0 {
		t.Errorf("repairs = %d, want 0: every candidate is already pending", audit.Repairs)
	}
	if keyedDuring != nil || !keyedSkipped {
		t.Errorf("keyed tick during the verify = skipped %v, %v; want Skipped", keyedSkipped, keyedDuring)
	}
	if captureErr != nil {
		t.Errorf("capture insert during the verify: %v after %s, want it unblocked", captureErr, captureTook)
	}
	if held > 30*time.Second {
		t.Errorf("verify held the resweep lock %s from claim to commit, over one keyed interval (30s)", held)
	}
	after, err := h.store.ResweepWinnersKeyed(ctx)
	if err != nil || after.Skipped {
		t.Fatalf("keyed tick after the verify = %+v, %v; want it to commit", after, err)
	}
	// A keyed probe that missed the seeded facts would delete their winners
	// here and would have made the timing above measure nothing. (The smoke
	// shape's scope mark sends the tick to the over-cap full instead.)
	keyedMissed := after.Path == "keyed" && after.ChunkKeptRows == 0
	if keyedMissed || h.count(`SELECT count(*) FROM supply_chain_impact_canonical_winners`) != shape.winners {
		t.Fatalf("keyed tick after the verify = %+v with %d winners; want kept facts and all %d winners",
			after, h.count(`SELECT count(*) FROM supply_chain_impact_canonical_winners`), shape.winners)
	}
}

// winnersAuditVerifyLoad reports the Linux one-minute load average, or
// "unknown", so a timing is never read without the host load.
func winnersAuditVerifyLoad() string {
	raw, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return "unknown"
	}
	if fields := strings.Fields(string(raw)); len(fields) > 0 {
		return fields[0]
	}
	return "unknown"
}

// TestSupplyChainImpactWinnersResweepAuditOverlappingPassesAreHarmlessLive is
// the #11117 A lease proof (arbiter addendum item 5e). The audit lease has a
// 60 s TTL that runAuditOnce does not renew, so a read phase longer than that
// lets a second instance start its own pass. The two passes can only overlap
// in their reads: a verify that runs while another holds the resweep lock is
// Skipped, and a verify that runs after another committed finds the first
// pass's repairs already pending and queues none. The proof seeds ten capture
// holes, runs a second verify from inside the first one's lock, then a third
// after the first commit, and requires exactly ten audit repairs and a table
// equal to a fresh recompute once the keyed ticks drain them.
func TestSupplyChainImpactWinnersResweepAuditOverlappingPassesAreHarmlessLive(t *testing.T) {
	h := newKeyedHarness(t, "#11117 audit overlap proof")
	scope := "vuln-intel://11117/audit-overlap"
	winnersLiveSeedScope(t, h.ctx, h.db, scope)
	h.exec(`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'f-' || g, $1, $1 || '/active', $2, 'f-' || g, '1.0.0', 'synthetic', 'f-' || g, now(), now(), false,
  jsonb_build_object('finding_id', 'finding:' || g, 'cve_id', 'CVE-2026-' || g, 'package_id', 'pkg:npm/p' || (g / 10),
    'impact_status', 'affected_exact', 'repository_id', 'repo-' || (g / 1000), 'subject_digest', '', 'ecosystem', 'npm',
    'priority_score', g % 90, 'observed_version', '1.0.0',
    'service_ids', '["svc-a"]'::jsonb, 'workload_ids', '["wl-a"]'::jsonb, 'environments', '["prod"]'::jsonb)
FROM generate_series(1, 20) AS g`, scope, facts.ReducerSupplyChainImpactFindingFactKind)
	if _, err := h.store.ResweepWinners(h.ctx); err != nil {
		t.Fatalf("populating full: %v", err)
	}
	// Ten capture holes: the facts change and no mark is queued.
	h.exec(`UPDATE fact_records SET payload = jsonb_set(payload, '{priority_score}', '99')
WHERE fact_id IN (SELECT 'f-' || g FROM generate_series(1, 10) AS g)`)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_log`)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_staging`)
	ns := func(v string) sql.NullString { return sql.NullString{String: v, Valid: true} }
	var idents []postgres.WinnersAuditIdent
	for g := 1; g <= 10; g++ {
		idents = append(idents, postgres.WinnersAuditIdent{
			PackageID: ns(fmt.Sprintf("pkg:npm/p%d", g/10)), RepositoryID: ns(fmt.Sprintf("repo-%d", g/1000)),
			SubjectDigest: ns(""), C0: ns(fmt.Sprintf("CVE-2026-%d", g)), AdvisoryID: ns(""),
		})
	}

	var mu sync.Mutex
	var claimAt, commitAt time.Time
	overlapped := h.store
	var during struct {
		skipped bool
		repairs int64
		err     error
	}
	first := h.store
	first.DB = verifyTimingDB{inner: postgres.SQLDB{DB: h.db}, mu: &mu, claimAt: &claimAt, commit: &commitAt, onClaim: func() {
		a, err := overlapped.VerifyStagedCandidates(h.ctx, idents, 0)
		during.skipped, during.repairs, during.err = a.Skipped, a.Repairs, err
	}}
	got, err := first.VerifyStagedCandidates(h.ctx, idents, 0)
	if err != nil || got.Skipped || got.Repairs != 10 {
		t.Fatalf("first verify = %+v, %v; want 10 repairs", got, err)
	}
	if during.err != nil || !during.skipped || during.repairs != 0 {
		t.Fatalf("overlapping verify = skipped %v repairs %d, %v; want Skipped with nothing queued", during.skipped, during.repairs, during.err)
	}
	again, err := h.store.VerifyStagedCandidates(h.ctx, idents, 0)
	if err != nil || again.Skipped || again.Repairs != 0 {
		t.Fatalf("verify after the first commit = %+v, %v; want 0 repairs (already pending)", again, err)
	}
	if n := h.count(`SELECT count(*) FROM supply_chain_impact_winners_dirty_staging WHERE source = 'audit'`); n != 10 {
		t.Fatalf("audit repairs staged = %d, want exactly 10 across the three passes", n)
	}
	chunkedDrain(h)
	h.assertParity("after the overlapping audit passes")
}
