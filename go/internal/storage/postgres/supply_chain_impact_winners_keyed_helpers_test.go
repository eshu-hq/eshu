// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Helpers for the #9805 keyed-resweep live proofs. Every change class goes
// through the production path (the impact writer, the production activation
// statements, the scanner tables) so the capture under test is the capture
// production runs.

var keyedFenceTokens atomic.Int64

var keyedNow = time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)

const keyedOperatorScope = "operator:vulnerability_suppressions"

// keyedHarness bundles one disposable database, the winners store and the
// production writer over it.
type keyedHarness struct {
	t      *testing.T
	ctx    context.Context
	db     *sql.DB
	store  postgres.SupplyChainImpactWinnersStore
	writer supplychaincore.PostgresSupplyChainImpactWriter
	clock  time.Time
}

func newKeyedHarness(t *testing.T, proof string) *keyedHarness {
	t.Helper()
	_ = supplyChainImpactLiveDSN(t, proof)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)
	db := openAdvisorySuppressionIsolatedDB(t)
	return &keyedHarness{
		t: t, ctx: ctx, db: db,
		store: keyedHarnessStore(db),
		writer: supplychaincore.PostgresSupplyChainImpactWriter{
			DB:  postgres.SupplyChainImpactBeginner{Beginner: postgres.SQLDB{DB: db}},
			Now: func() time.Time { return keyedNow },
		},
		clock: keyedNow,
	}
}

// keyedHarnessStore builds the store with the key-fraction cap out of reach. The
// fixtures hold a handful of winners, so any change exceeds half of them and
// every tick would take the full path; the cap tests set the fraction back.
func keyedHarnessStore(db *sql.DB) postgres.SupplyChainImpactWinnersStore {
	store := postgres.NewSupplyChainImpactWinnersStore(postgres.SQLDB{DB: db})
	store.MaxKeyFraction = 1000
	return store
}

// tick advances the harness clock by one maintainer interval.
func (h *keyedHarness) tick() time.Time {
	h.clock = h.clock.Add(30 * time.Second)
	return h.clock
}

func (h *keyedHarness) exec(query string, args ...any) {
	h.t.Helper()
	winnersLiveExec(h.t, h.ctx, h.db, query, args...)
}

func (h *keyedHarness) count(query string, args ...any) int {
	h.t.Helper()
	var n int
	if err := h.db.QueryRowContext(h.ctx, query, args...).Scan(&n); err != nil {
		h.t.Fatalf("count %q: %v", query, err)
	}
	return n
}

func (h *keyedHarness) dirtyDepth() int {
	h.t.Helper()
	return h.count(`SELECT count(*) FROM supply_chain_impact_winners_dirty_log`)
}

// seedScope creates a source scope with one active generation.
func (h *keyedHarness) seedScope(scope, generation string) {
	h.t.Helper()
	h.exec(`
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, payload, active_generation_id)
VALUES ($1, 'vulnerability_intelligence', 'synthetic', $1, 'synthetic', $1, $2, $2, 'active', '{}'::jsonb, NULL)`, scope, keyedNow)
	h.addGeneration(scope, generation, "active")
	h.exec(`UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`, scope, generation)
}

func (h *keyedHarness) addGeneration(scope, generation, status string) {
	h.t.Helper()
	h.exec(`
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
VALUES ($1, $2, 'synthetic', $3::timestamptz, $3::timestamptz, $4::text, CASE WHEN $4::text = 'pending' THEN NULL ELSE $3::timestamptz END, '{}'::jsonb)`,
		generation, scope, keyedNow, status)
}

// activate flips a scope from its active generation to a pending one the way
// the projector Ack does: supersede, activate, then move the pointer with the
// production Ack statement. The pointer trigger captures the old and new
// generation.
func (h *keyedHarness) activate(scope, from, to string) {
	h.t.Helper()
	h.flip(scope, from, to, postgres.ActivationPointerMoveSQLForTest)
}

func (h *keyedHarness) flip(scope, from, to, pointerSQL string) {
	h.t.Helper()
	tx, err := h.db.BeginTx(h.ctx, nil)
	if err != nil {
		h.t.Fatalf("begin activation: %v", err)
	}
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`UPDATE scope_generations SET status = 'superseded', superseded_at = now() WHERE scope_id = $1 AND generation_id = $2`, []any{scope, from}},
		{`UPDATE scope_generations SET status = 'active', activated_at = now() WHERE scope_id = $1 AND generation_id = $2`, []any{scope, to}},
		{pointerSQL, []any{h.clock, scope, to}},
	} {
		if _, err := tx.ExecContext(h.ctx, q.sql, q.args...); err != nil {
			_ = tx.Rollback()
			h.t.Fatalf("activation statement %q: %v", q.sql, err)
		}
	}
	if err := tx.Commit(); err != nil {
		h.t.Fatalf("commit activation: %v", err)
	}
}

func keyedFinding(cve, pkg string, priority int) supplychaincore.SupplyChainImpactFinding {
	return supplychaincore.SupplyChainImpactFinding{
		CVEID: cve, PackageID: pkg, Ecosystem: "npm", ObservedVersion: "1.0.0",
		MatchReason: "npm_semver_affected_range", Status: supplychaincore.SupplyChainImpactAffectedExact,
		Confidence: "exact", RepositoryID: "repo-9805", PriorityScore: priority, CanonicalWrites: 1,
		EvidenceFactIDs: []string{"cve-evidence"},
	}
}

func keyedAdvisoryFinding(advisory, pkg string, priority int) supplychaincore.SupplyChainImpactFinding {
	finding := keyedFinding("", pkg, priority)
	finding.AdvisoryID = advisory
	return finding
}

// write runs one production writer pass for (scope, generation).
func (h *keyedHarness) write(scope, generation string, findings ...supplychaincore.SupplyChainImpactFinding) supplychaincore.SupplyChainImpactWriteResult {
	h.t.Helper()
	result, err := h.writer.WriteSupplyChainImpactFindings(h.ctx, supplychaincore.SupplyChainImpactWrite{
		IntentID: "intent-9805", ScopeID: scope, GenerationID: generation,
		SourceSystem: "vulnerability_intelligence", Cause: "9805 live proof",
		Findings: findings, FencingToken: keyedFenceTokens.Add(1),
	})
	if err != nil {
		h.t.Fatalf("write %s/%s: %v", scope, generation, err)
	}
	return result
}

// seedOperator writes an operator-scope generation of raw suppression facts
// and activates it. The payloads are free-form on purpose: a payload may lack
// package_id and subject_digest, like the suppression store's map payloads.
func (h *keyedHarness) seedOperator(generation string, from string, payloads map[string]map[string]any) {
	h.t.Helper()
	h.writeOperator(generation, from, payloads)
}

func (h *keyedHarness) writeOperator(generation string, from string, payloads map[string]map[string]any) {
	h.t.Helper()
	if from == "" {
		h.exec(`
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, payload, active_generation_id)
VALUES ($1, 'vulnerability_suppression', 'eshu_policy', $1, 'vulnerability_intelligence', $1, $2, $2, 'active', '{}'::jsonb, NULL)`,
			keyedOperatorScope, keyedNow)
		h.addGeneration(keyedOperatorScope, generation, "active")
	} else {
		h.addGeneration(keyedOperatorScope, generation, "pending")
	}
	for id, payload := range payloads {
		body, err := json.Marshal(payload)
		if err != nil {
			h.t.Fatalf("marshal operator payload: %v", err)
		}
		h.exec(`
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ($1, $2, $3, $4, $1, '1.0.0', 'eshu_policy', $1, now(), now(), false, $5::jsonb)`,
			id+"/"+generation, keyedOperatorScope, generation, facts.ReducerSupplyChainImpactFindingFactKind, string(body))
	}
	if from == "" {
		h.exec(`UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`, keyedOperatorScope, generation)
		return
	}
	h.activate(keyedOperatorScope, from, generation)
}

// keyed runs one keyed resweep and requires it to have committed on the keyed
// path with a drained dirty log.
func (h *keyedHarness) keyed() supplychaincore.SupplyChainImpactWinnersResweep {
	h.t.Helper()
	result, err := h.store.ResweepWinnersKeyed(h.ctx, h.tick())
	// A cancelled tick's rollback runs on database/sql's own goroutine and can
	// still hold the single-flight lock for a moment; retry a skip briefly.
	for retry := 0; err == nil && result.Skipped && retry < 100; retry++ {
		time.Sleep(50 * time.Millisecond)
		result, err = h.store.ResweepWinnersKeyed(h.ctx, h.tick())
	}
	if err != nil || result.Skipped {
		h.t.Fatalf("keyed resweep = %+v, %v", result, err)
	}
	if result.Path != telemetry.SupplyChainImpactWinnersPathKeyed {
		h.t.Fatalf("keyed resweep ran on path %q, want %q (result %+v)", result.Path, telemetry.SupplyChainImpactWinnersPathKeyed, result)
	}
	if depth := h.dirtyDepth(); depth != 0 {
		h.t.Fatalf("dirty log depth after a keyed tick = %d, want 0", depth)
	}
	return result
}

// assertParity requires the winners table to equal a fresh full recompute.
func (h *keyedHarness) assertParity(label string) {
	h.t.Helper()
	before := h.t.Failed()
	winnersLiveAssertEqualsRecompute(h.t, h.ctx, h.db, h.store, h.tick())
	if !before && h.t.Failed() {
		h.t.Fatalf("%s: the keyed table differs from a fresh recompute", label)
	}
}

// dropForeignKeysTo removes every foreign key that references table, so a test
// can change or delete a parent row without cascading into the facts the
// keyed consumer must still read. The database is disposable.
func (h *keyedHarness) dropForeignKeysTo(table string) {
	h.t.Helper()
	rows, err := h.db.QueryContext(h.ctx, `
SELECT conrelid::regclass::text, conname FROM pg_constraint
WHERE contype = 'f' AND conparentid = 0 AND confrelid = $1::regclass`, table)
	if err != nil {
		h.t.Fatalf("list foreign keys to %s: %v", table, err)
	}
	type fk struct{ rel, name string }
	var fks []fk
	for rows.Next() {
		var f fk
		if err := rows.Scan(&f.rel, &f.name); err != nil {
			_ = rows.Close()
			h.t.Fatalf("scan foreign key: %v", err)
		}
		fks = append(fks, f)
	}
	if err := rows.Close(); err != nil {
		h.t.Fatalf("close foreign keys: %v", err)
	}
	for _, f := range fks {
		h.exec(`ALTER TABLE ` + f.rel + ` DROP CONSTRAINT "` + f.name + `"`)
	}
}

// dropForeignKeysOf removes every foreign key the table itself declares, so a
// test can insert scanner rows for a finding without building the publication
// and target rows the real publish function creates. The database is
// disposable.
func (h *keyedHarness) dropForeignKeysOf(table string) {
	h.t.Helper()
	rows, err := h.db.QueryContext(h.ctx, `
SELECT conname FROM pg_constraint WHERE contype = 'f' AND conparentid = 0 AND conrelid = $1::regclass`, table)
	if err != nil {
		h.t.Fatalf("list foreign keys of %s: %v", table, err)
	}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			_ = rows.Close()
			h.t.Fatalf("scan foreign key: %v", err)
		}
		names = append(names, name)
	}
	if err := rows.Close(); err != nil {
		h.t.Fatalf("close foreign keys: %v", err)
	}
	for _, name := range names {
		h.exec(`ALTER TABLE ` + table + ` DROP CONSTRAINT "` + name + `"`)
	}
}

// findingFactID returns the fact id of the live finding for cve in scope.
func (h *keyedHarness) findingFactID(scope, cve string) string {
	h.t.Helper()
	var id string
	if err := h.db.QueryRowContext(h.ctx, `
SELECT fact_id FROM fact_records
WHERE scope_id = $1 AND fact_kind = $2 AND NOT is_tombstone AND payload->>'cve_id' = $3`,
		scope, facts.ReducerSupplyChainImpactFindingFactKind, cve).Scan(&id); err != nil {
		h.t.Fatalf("find fact id for %s in %s: %v", cve, scope, err)
	}
	return id
}
