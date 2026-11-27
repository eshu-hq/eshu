// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestSupplyChainImpactKeyPresenceFillLive drives the key-presence fill to
// completion on a fixture with stale bits (Refs #9975 round 6): the fill
// sets every row's bits from its payload, a disagreement restarts the sweep
// instead of completing, and the marker flips from incomplete to complete.
// It also pins the fail-closed direction: on the unfilled table the guarded
// probe under-matches the shipped probe (explicit-” rows stay out), which is
// why the tick runs the guarded text only after this marker completes.
func TestSupplyChainImpactSupportsKeyPresenceFillLive(t *testing.T) {
	h := newKeyedHarness(t, "9975 key-presence fill")

	const scope = "scope-probe-kf"
	const generation = "scope-probe-kf/g1"
	h.seedScope(scope, generation)

	seed := []struct {
		id      string
		payload string
		bits    int
	}{
		{"probe-kf-full", `{"package_id":"npm://kf","repository_id":"repo-kf","subject_digest":"sha256:kf","cve_id":"CVE-KF-1","advisory_id":"GHSA-KF-1"}`, 7},
		{"probe-kf-emptydigest", `{"package_id":"npm://kf","repository_id":"repo-kf","subject_digest":"","cve_id":"CVE-KF-1","advisory_id":"GHSA-KF-1"}`, 7},
		{"probe-kf-nodigest", `{"package_id":"npm://kf","repository_id":"repo-kf","cve_id":"CVE-KF-1","advisory_id":"GHSA-KF-1"}`, 3},
		{"probe-kf-nulladv", `{"package_id":"npm://kf","repository_id":"repo-kf","subject_digest":"sha256:kf","cve_id":"CVE-KF-1","advisory_id":null}`, 7},
		{"probe-kf-nopkgrepo", `{"subject_digest":"sha256:kf","cve_id":"CVE-KF-1","advisory_id":"GHSA-KF-1"}`, 4},
	}
	for _, row := range seed {
		h.exec(`
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ($1, $2, $3, $4, $1, '1.0.0', 'vulnerability_intelligence', $1, $5, $5, false, $6::jsonb)`,
			row.id+"/"+generation, scope, generation, facts.ReducerSupplyChainImpactFindingFactKind, keyedNow, row.payload)
	}
	h.syncSupports()

	// The writer fill sets every row's bits.
	for _, row := range seed {
		var bits int
		if err := h.db.QueryRowContext(h.ctx, `SELECT key_presence FROM supply_chain_impact_finding_supports WHERE fact_id = $1`, row.id+"/"+generation).Scan(&bits); err != nil {
			t.Fatalf("read key_presence of %s: %v", row.id, err)
		}
		if bits != row.bits {
			t.Errorf("writer fill key_presence of %s = %d, want %d", row.id, bits, row.bits)
		}
	}

	// Fail-closed direction on the unfilled table: zero the bits (an old
	// writer's rows), and the guarded probe must under-match the shipped one.
	h.exec(`UPDATE supply_chain_impact_finding_supports SET key_presence = 0`)
	conn, err := h.db.Conn(h.ctx)
	if err != nil {
		t.Fatalf("open probe connection: %v", err)
	}
	defer func() { _ = conn.Close() }()
	tx, err := conn.BeginTx(h.ctx, nil)
	if err != nil {
		t.Fatalf("begin probe transaction: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(h.ctx, `CREATE TEMP TABLE `+postgres.WinnersKeyedIdentsTableForTest+` (
    package_id text, repository_id text, subject_digest text, c0 text, advisory_id text)`); err != nil {
		t.Fatalf("stage identifier table: %v", err)
	}
	if _, err := tx.ExecContext(h.ctx, `INSERT INTO `+postgres.WinnersKeyedIdentsTableForTest+` VALUES
  ('npm://kf', 'repo-kf', 'sha256:kf', 'CVE-KF-1', 'GHSA-KF-1'),
  ('npm://kf', 'repo-kf', '', 'CVE-KF-1', 'GHSA-KF-1')`); err != nil {
		t.Fatalf("stage identifiers: %v", err)
	}
	stripBuild := func(build string) string {
		return strings.Replace(build, "CREATE TEMP TABLE winners_keyed_narrow ON COMMIT DROP AS", "", 1)
	}
	count := func(probe string) int {
		var n int
		if err := tx.QueryRowContext(h.ctx, `SELECT count(*) FROM (`+probe+`) AS p`).Scan(&n); err != nil {
			t.Fatalf("count probe rows: %v", err)
		}
		return n
	}
	shippedProbe := stripBuild(postgres.WinnersKeyedNarrowBuildFromSupportsSQLForTest)
	guardedProbe := stripBuild(postgres.WinnersKeyedNarrowKeyPresenceBuildSQLForTest)
	shipped, guarded := count(shippedProbe), count(guardedProbe)
	t.Logf("unfilled table: shipped=%d guarded=%d", shipped, guarded)
	if shipped == 0 {
		t.Fatal("shipped probe matches nothing on the fixture")
	}
	if guarded >= shipped {
		t.Fatalf("guarded probe matches %d rows, want fewer than shipped %d on the unfilled table (fail-closed)", guarded, shipped)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback probe transaction: %v", err)
	}

	// The fill to completion: stale rows get their bits, the marker stamps.
	fill := postgres.SupplyChainImpactKeyPresenceFill{DB: postgres.SQLDB{DB: h.db}, ChunkSize: 2, VerifySliceSize: 2}
	// Corrupt one filled row mid-sweep: chunk size 2 fills the first pair,
	// then the external zeroing forces the verification to restart the sweep.
	steps := 0
	for steps = 0; steps < 30; steps++ {
		report, err := fill.RunStep(h.ctx)
		if err != nil {
			t.Fatalf("fill step %d: %v", steps, err)
		}
		if steps == 0 {
			h.exec(`UPDATE supply_chain_impact_finding_supports SET key_presence = 0 WHERE fact_id = $1`, "probe-kf-full/"+generation)
		}
		if report.Complete {
			break
		}
	}
	if steps >= 30 {
		t.Fatal("the fill did not complete within 30 steps")
	}
	for _, row := range seed {
		var bits int
		if err := h.db.QueryRowContext(h.ctx, `SELECT key_presence FROM supply_chain_impact_finding_supports WHERE fact_id = $1`, row.id+"/"+generation).Scan(&bits); err != nil {
			t.Fatalf("read key_presence of %s: %v", row.id, err)
		}
		if bits != row.bits {
			t.Errorf("filled key_presence of %s = %d, want %d", row.id, bits, row.bits)
		}
	}
	var completed bool
	var sweeps int64
	if err := h.db.QueryRowContext(h.ctx, `SELECT completed_at IS NOT NULL, sweeps FROM supply_chain_impact_finding_supports_backfill WHERE marker_name = 'supply_chain_impact_key_presence_fill_v1'`).Scan(&completed, &sweeps); err != nil {
		t.Fatalf("read fill marker: %v", err)
	}
	if !completed {
		t.Error("the fill marker is not complete")
	}
	t.Logf("fill marker complete, sweeps=%d", sweeps)
	if sweeps < 1 {
		t.Errorf("sweeps = %d, want at least 1: the mid-sweep corruption must have restarted the sweep", sweeps)
	}

	// Post-marker agreement: the guarded probe matches the shipped probe on
	// the filled table.
	conn2, err := h.db.Conn(h.ctx)
	if err != nil {
		t.Fatalf("open probe connection: %v", err)
	}
	defer func() { _ = conn2.Close() }()
	tx2, err := conn2.BeginTx(h.ctx, nil)
	if err != nil {
		t.Fatalf("begin probe transaction: %v", err)
	}
	defer func() { _ = tx2.Rollback() }()
	if _, err := tx2.ExecContext(h.ctx, `CREATE TEMP TABLE `+postgres.WinnersKeyedIdentsTableForTest+` (
    package_id text, repository_id text, subject_digest text, c0 text, advisory_id text)`); err != nil {
		t.Fatalf("stage identifier table: %v", err)
	}
	if _, err := tx2.ExecContext(h.ctx, `INSERT INTO `+postgres.WinnersKeyedIdentsTableForTest+` VALUES
  ('npm://kf', 'repo-kf', 'sha256:kf', 'CVE-KF-1', 'GHSA-KF-1'),
  ('npm://kf', 'repo-kf', '', 'CVE-KF-1', 'GHSA-KF-1')`); err != nil {
		t.Fatalf("stage identifiers: %v", err)
	}
	for _, q := range []struct{ name, a, b string }{
		{"filled shipped-minus-guarded", shippedProbe, guardedProbe},
		{"filled guarded-minus-shipped", guardedProbe, shippedProbe},
	} {
		var n int
		if err := tx2.QueryRowContext(h.ctx, `SELECT count(*) FROM ((`+q.a+`) EXCEPT (`+q.b+`)) AS d`).Scan(&n); err != nil {
			t.Fatalf("except %s: %v", q.name, err)
		}
		if n != 0 {
			t.Errorf("EXCEPT %s = %d on the filled table, want 0", q.name, n)
		}
	}
}

// TestSupplyChainImpactWinnersNarrowTripleGuardIndexBuildsConcurrentlyLive
// proves the 362 covering index builds CONCURRENTLY without blocking impact
// writers: the exact migration statement runs while writer passes land, every
// write commits, and the index comes out valid and ready. It drops the index
// first, so the build is real rather than an IF NOT EXISTS no-op.
func TestSupplyChainImpactWinnersNarrowTripleGuardIndexBuildsConcurrentlyLive(t *testing.T) {
	h := newKeyedHarness(t, "9975 triple-guard concurrent index build")

	const scope = "scope-probe-ci"
	const generation = "scope-probe-ci/g1"
	h.seedScope(scope, generation)

	findings := make([]supplychaincore.SupplyChainImpactFinding, 0, 20)
	for i := 0; i < 20; i++ {
		findings = append(findings, keyedFinding(fmt.Sprintf("CVE-KF-CI-%02d", i), "pkg:npm/ci", 50))
	}
	h.write(scope, generation, findings...)
	// The writer restates one intent's finding set per call: restating fewer
	// findings retracts the missing ones, so every concurrent pass restates
	// the seeded set plus one new finding (additive, production-like).
	for i := 0; i < 10; i++ {
		findings = append(findings, keyedFinding(fmt.Sprintf("CVE-KF-CI-W%02d", i), "pkg:npm/ciw", 50))
	}
	h.exec(`DROP INDEX IF EXISTS supply_chain_impact_finding_supports_keyed_triple_guard_idx`)

	raw, err := os.ReadFile("migrations/363_supply_chain_impact_supports_keyed_triple_guard_idx.sql")
	if err != nil {
		t.Fatalf("read migration 363: %v", err)
	}
	var lines []string
	for _, line := range strings.Split(string(raw), "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" && !strings.HasPrefix(trimmed, "--") {
			lines = append(lines, trimmed)
		}
	}
	buildSQL := strings.Join(lines, " ")

	buildErr := make(chan error, 1)
	go func() {
		_, err := h.db.ExecContext(h.ctx, buildSQL)
		buildErr <- err
	}()
	// Each concurrent pass restates the cumulative set (no retractions) plus
	// its new finding, so the build overlaps real writer upsert load.
	for i := 0; i < 10; i++ {
		h.write(scope, generation, findings[:20+i+1]...)
	}
	if err := <-buildErr; err != nil {
		t.Fatalf("concurrent index build failed while writers wrote: %v", err)
	}
	var valid, ready bool
	if err := h.db.QueryRowContext(h.ctx, `SELECT indisvalid, indisready FROM pg_index WHERE indexrelid = 'supply_chain_impact_finding_supports_keyed_triple_guard_idx'::regclass`).Scan(&valid, &ready); err != nil {
		t.Fatalf("read triple-guard index validity: %v", err)
	}
	if !valid || !ready {
		t.Fatalf("triple-guard index valid=%v ready=%v, want both true after the concurrent build", valid, ready)
	}
	var supports int
	if err := h.db.QueryRowContext(h.ctx, `SELECT count(*) FROM supply_chain_impact_finding_supports`).Scan(&supports); err != nil {
		t.Fatalf("count supports: %v", err)
	}
	if supports < 30 {
		t.Fatalf("supports = %d, want at least the seeded 20 plus the 10 concurrent writer rows", supports)
	}
}
