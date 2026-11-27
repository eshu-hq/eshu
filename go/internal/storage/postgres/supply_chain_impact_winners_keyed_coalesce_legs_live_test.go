// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestWinnersKeyedProbeMatchesCoalesceDefaultLegs pins the COALESCE
// leniency of the shipped keyed probe's c0 and advisory legs (Refs #9975).
//
// Unlike the triple legs (payload->> strict: a missing key never matches,
// pinned by TestWinnersKeyedProbeExcludesMissingKeySupports), the c0 leg
// COALESCEs both raw keys to ” and the advisory leg COALESCEs to ”, so a
// support whose payload lacks the key still matches a staged identifier
// whose component is ”. Concretely: a CVE-only support with no advisory
// key matches the (pkg, repo, digest, cve, ”) identifier, and a support
// with neither key matches the (pkg, repo, digest, ”, ”) identifier.
//
// The round-5 key-presence candidate guarded those legs with presence bits
// and excluded exactly these rows (falsifier: 280 shipped-only rows, classes
// noadv/nulladv/nocveadv, EXCEPT 0 the other way), so the construction was
// killed. This test pins the shipped semantics the candidate broke: any
// future probe rewrite must keep matching these rows.
func TestSupplyChainImpactWinnersNarrowProbeMatchesCoalesceLegsLive(t *testing.T) {
	h := newKeyedHarness(t, "9975 coalesce-legs probe guard")

	const scope = "scope-probe-cl"
	const generation = "scope-probe-cl/g1"
	h.seedScope(scope, generation)

	seed := []struct {
		id      string
		payload string
	}{
		// Advisory key absent: shipped matches the ''-advisory ident.
		{"probe-cl-e1", `{"package_id":"npm://probe-c1","repository_id":"repo-probe-1","subject_digest":"sha256:c1","cve_id":"CVE-PROBE-1"}`},
		// Advisory JSON null: ->> yields NULL, COALESCE defaults to ''.
		{"probe-cl-e2", `{"package_id":"npm://probe-c1","repository_id":"repo-probe-1","subject_digest":"sha256:c1","cve_id":"CVE-PROBE-1","advisory_id":null}`},
		// Advisory present-empty: the writer-normal CVE-only shape.
		{"probe-cl-e3", `{"package_id":"npm://probe-c1","repository_id":"repo-probe-1","subject_digest":"sha256:c1","cve_id":"CVE-PROBE-1","advisory_id":""}`},
		// Both keys absent: shipped matches the ''-c0 ident.
		{"probe-cl-e4", `{"package_id":"npm://probe-c2","repository_id":"repo-probe-2","subject_digest":"sha256:c2"}`},
		// Both keys present-empty: matches under any construction.
		{"probe-cl-e5", `{"package_id":"npm://probe-c2","repository_id":"repo-probe-2","subject_digest":"sha256:c2","cve_id":"","advisory_id":""}`},
		// Triple missing: the negative control. The triple legs are
		// strict, so the ''-triple ident must not match even though the
		// c0/advisory legs default.
		{"probe-cl-e6", `{"cve_id":"CVE-PROBE-3","advisory_id":"GHSA-PROBE-3"}`},
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
    package_id text, repository_id text, subject_digest text, c0 text, advisory_id text)`); err != nil {
		t.Fatalf("stage identifier table: %v", err)
	}
	if _, err := tx.ExecContext(h.ctx, `INSERT INTO `+postgres.WinnersKeyedIdentsTableForTest+` VALUES
  ('npm://probe-c1', 'repo-probe-1', 'sha256:c1', 'CVE-PROBE-1', ''),
  ('npm://probe-c2', 'repo-probe-2', 'sha256:c2', '', ''),
  ('', '', '', 'CVE-PROBE-3', 'GHSA-PROBE-3')`); err != nil {
		t.Fatalf("stage identifiers: %v", err)
	}
	if _, err := tx.ExecContext(h.ctx, postgres.WinnersKeyedNarrowBuildFromSupportsSQLForTest); err != nil {
		t.Fatalf("run shipped narrow probe build: %v", err)
	}
	rows, err := tx.QueryContext(h.ctx, `SELECT fact_id FROM `+postgres.WinnersKeyedNarrowTableForTest)
	if err != nil {
		t.Fatalf("read narrow probe output: %v", err)
	}
	defer func() { _ = rows.Close() }()
	got := map[string]bool{}
	for rows.Next() {
		var factID string
		if err := rows.Scan(&factID); err != nil {
			t.Fatalf("scan narrow row: %v", err)
		}
		got[factID] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate narrow rows: %v", err)
	}

	// The COALESCE legs match missing, JSON-null, and present-empty keys
	// alike for '' identifier components.
	for _, id := range []string{"probe-cl-e1", "probe-cl-e2", "probe-cl-e3", "probe-cl-e4", "probe-cl-e5"} {
		if !got[id+"/"+generation] {
			t.Errorf("narrow probe output misses %s: the shipped COALESCE legs must match missing/null/empty keys for '' idents", id)
		}
	}
	// The triple legs stay strict: a triple-missing row matches nothing.
	if got["probe-cl-e6/"+generation] {
		t.Errorf("narrow probe output contains probe-cl-e6 (triple missing): the shipped triple legs must not match missing-key rows")
	}
}
