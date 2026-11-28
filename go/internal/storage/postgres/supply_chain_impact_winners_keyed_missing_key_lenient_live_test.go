// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// Failing-first legs for #11691 shape (b): the keyed narrow probes must read
// a missing triple key and a JSON-null triple key as '', in both the shipped
// payload-expression probe and the triple-guarded plain-column probe. Both
// legs below fail on the strict probes (the missing-key copies match no
// identifier) and pass once the triple legs are lenient.

// TestSupplyChainImpactWinnersNarrowProbeMatchesPureMissingIdentLive is the
// pure-missing leg: every copy whose triple keys are absent (or JSON null)
// matches the identifier its coalesced keys stage, on both probe shapes.
func TestSupplyChainImpactWinnersNarrowProbeMatchesPureMissingIdentLive(t *testing.T) {
	h := newKeyedHarness(t, "11691 pure-missing identifier probe leg")

	const scope = "scope-probe-pm"
	const generation = "scope-probe-pm/g1"
	h.seedScope(scope, generation)

	seed := []struct {
		id      string
		payload string
	}{
		// Explicit-'' subject_digest: matched before and after.
		{"probe-pm-e1", `{"package_id":"npm://probe-m1","repository_id":"repo-probe-m1","subject_digest":"","cve_id":"CVE-PROBE-M1","advisory_id":"GHSA-PROBE-M1","priority_score":10}`},
		// Missing subject_digest key: must match after the fix.
		{"probe-pm-e2", `{"package_id":"npm://probe-m1","repository_id":"repo-probe-m1","cve_id":"CVE-PROBE-M1","advisory_id":"GHSA-PROBE-M1","priority_score":10}`},
		// JSON null subject_digest: must match after the fix.
		{"probe-pm-e3", `{"package_id":"npm://probe-m1","repository_id":"repo-probe-m1","subject_digest":null,"cve_id":"CVE-PROBE-M1","advisory_id":"GHSA-PROBE-M1","priority_score":10}`},
		// Clean control on its own identifier.
		{"probe-pm-e4", `{"package_id":"go://probe-clean","repository_id":"repo-probe-clean","subject_digest":"sha256:clean","cve_id":"CVE-PROBE-CLEAN","advisory_id":"GHSA-PROBE-CLEAN","priority_score":10}`},
	}
	for _, row := range seed {
		h.exec(`
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ($1, $2, $3, $4, $1, '1.0.0', 'vulnerability_intelligence', $1, $5, $5, false, $6::jsonb)`,
			row.id+"/"+generation, scope, generation, facts.ReducerSupplyChainImpactFindingFactKind, keyedNow, row.payload)
	}
	h.syncSupports()

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
    package_id text, repository_id text, subject_digest text, c0 text, advisory_id text
)`); err != nil {
		t.Fatalf("stage identifier table: %v", err)
	}
	if _, err := tx.ExecContext(h.ctx, `INSERT INTO `+postgres.WinnersKeyedIdentsTableForTest+` VALUES
  ('npm://probe-m1', 'repo-probe-m1', '', 'CVE-PROBE-M1', 'GHSA-PROBE-M1'),
  ('go://probe-clean', 'repo-probe-clean', 'sha256:clean', 'CVE-PROBE-CLEAN', 'GHSA-PROBE-CLEAN')`); err != nil {
		t.Fatalf("stage identifiers: %v", err)
	}

	runProbe := func(build, name string) map[string]bool {
		if _, err := tx.ExecContext(h.ctx, `DROP TABLE IF EXISTS `+postgres.WinnersKeyedNarrowTableForTest); err != nil {
			t.Fatalf("drop narrow table before %s: %v", name, err)
		}
		if _, err := tx.ExecContext(h.ctx, build); err != nil {
			t.Fatalf("run %s narrow probe build: %v", name, err)
		}
		rows, err := tx.QueryContext(h.ctx, `SELECT fact_id FROM `+postgres.WinnersKeyedNarrowTableForTest)
		if err != nil {
			t.Fatalf("read %s narrow probe output: %v", name, err)
		}
		defer func() { _ = rows.Close() }()
		got := map[string]bool{}
		for rows.Next() {
			var factID string
			if err := rows.Scan(&factID); err != nil {
				t.Fatalf("scan %s narrow row: %v", name, err)
			}
			got[factID] = true
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("iterate %s narrow rows: %v", name, err)
		}
		return got
	}

	probes := map[string]string{
		"shipped supports": postgres.WinnersKeyedNarrowBuildFromSupportsSQLForTest,
		"key-presence":     postgres.WinnersKeyedNarrowKeyPresenceBuildSQLForTest,
	}
	for name, build := range probes {
		got := runProbe(build, name)
		for _, id := range []string{"probe-pm-e1", "probe-pm-e2", "probe-pm-e3", "probe-pm-e4"} {
			if !got[id+"/"+generation] {
				t.Errorf("%s probe misses %s: the lenient triple legs must match missing and JSON-null keys as '' (Refs #11691)", name, id)
			}
		}
	}
}

// TestSupplyChainImpactWinnersMissingKeyCopyOutranksExplicitLive is the mixed
// leg: one identifier, two copies, the missing-key copy scores higher. The
// supports oracle (the full select the chunked full converges to) already
// crowns the missing-key copy; the keyed narrow probes must reach that same
// copy, on both probe shapes.
func TestSupplyChainImpactWinnersMissingKeyCopyOutranksExplicitLive(t *testing.T) {
	h := newKeyedHarness(t, "11691 mixed identifier outrank probe leg")

	const scope = "scope-probe-mx"
	const generation = "scope-probe-mx/g1"
	h.seedScope(scope, generation)

	seed := []struct {
		id      string
		payload string
	}{
		// Explicit-'' subject_digest, lower priority: the pre-fix winner.
		{"probe-mx-low", `{"package_id":"npm://probe-x1","repository_id":"repo-probe-x1","subject_digest":"","cve_id":"CVE-PROBE-X1","advisory_id":"GHSA-PROBE-X1","priority_score":10}`},
		// Missing subject_digest key, higher priority: the oracle winner.
		{"probe-mx-high", `{"package_id":"npm://probe-x1","repository_id":"repo-probe-x1","cve_id":"CVE-PROBE-X1","advisory_id":"GHSA-PROBE-X1","priority_score":90}`},
	}
	for _, row := range seed {
		h.exec(`
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ($1, $2, $3, $4, $1, '1.0.0', 'vulnerability_intelligence', $1, $5, $5, false, $6::jsonb)`,
			row.id+"/"+generation, scope, generation, facts.ReducerSupplyChainImpactFindingFactKind, keyedNow, row.payload)
	}
	h.syncSupports()

	// The oracle crowns the missing-key copy: both copies share the
	// coalesced canonical key, priority decides.
	var oracleWinner string
	if err := h.db.QueryRowContext(h.ctx, `WITH oracle AS (`+postgres.WinnerSelectFromSupportsSQLForTest+`)
SELECT winner_fact_id FROM oracle WHERE winner_fact_id IN ($1, $2)`,
		"probe-mx-low/"+generation, "probe-mx-high/"+generation).Scan(&oracleWinner); err != nil {
		t.Fatalf("read the oracle winner: %v", err)
	}
	if want := "probe-mx-high/" + generation; oracleWinner != want {
		t.Fatalf("oracle winner = %s, want %s: the missing-key copy must outrank the explicit-'' copy", oracleWinner, want)
	}

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
    package_id text, repository_id text, subject_digest text, c0 text, advisory_id text
)`); err != nil {
		t.Fatalf("stage identifier table: %v", err)
	}
	if _, err := tx.ExecContext(h.ctx, `INSERT INTO `+postgres.WinnersKeyedIdentsTableForTest+` VALUES
  ('npm://probe-x1', 'repo-probe-x1', '', 'CVE-PROBE-X1', 'GHSA-PROBE-X1')`); err != nil {
		t.Fatalf("stage identifiers: %v", err)
	}

	for name, build := range map[string]string{
		"shipped supports": postgres.WinnersKeyedNarrowBuildFromSupportsSQLForTest,
		"key-presence":     postgres.WinnersKeyedNarrowKeyPresenceBuildSQLForTest,
	} {
		if _, err := tx.ExecContext(h.ctx, `DROP TABLE IF EXISTS `+postgres.WinnersKeyedNarrowTableForTest); err != nil {
			t.Fatalf("drop narrow table before %s: %v", name, err)
		}
		if _, err := tx.ExecContext(h.ctx, build); err != nil {
			t.Fatalf("run %s narrow probe build: %v", name, err)
		}
		var n int
		if err := tx.QueryRowContext(h.ctx, `SELECT count(*) FROM `+postgres.WinnersKeyedNarrowTableForTest+` WHERE fact_id = $1`,
			oracleWinner).Scan(&n); err != nil {
			t.Fatalf("count the oracle winner in the %s probe output: %v", name, err)
		}
		if n != 1 {
			t.Errorf("%s probe holds %d copies of the oracle winner %s, want 1: the keyed path must reach the copy the oracle crowns (Refs #11691)", name, n, oracleWinner)
		}
	}
}
