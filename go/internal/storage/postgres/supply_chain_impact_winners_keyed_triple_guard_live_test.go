// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestSupplyChainImpactWinnersNarrowTripleGuardProbeMatchesShippedLive pins the round-6
// triple-guard probe rewrite (Refs #9975): the plain-column probe with
// triple-only key-presence guards and unguarded COALESCE c0/advisory legs
// must return exactly the shipped supports probe's rows — EXCEPT 0 in both
// directions — on a fixture co-locating every advisory-absent class
// (noadv/nulladv/nocveadv) with every triple edge shape.
//
// The fixture doubles as the writer-fill proof: each row's stored
// key_presence must equal the bits its payload's key presence implies
// (package_id = 1, repository_id = 2, subject_digest = 4), derived from the
// same SQL expression the backfill shares. Advisory keys carry no bits: the
// c0/advisory legs are plain equalities with no guards, so a missing or
// JSON-null advisory key still matches a ” identifier component, exactly
// like the shipped COALESCE legs.
func TestSupplyChainImpactWinnersNarrowTripleGuardProbeMatchesShippedLive(t *testing.T) {
	h := newKeyedHarness(t, "9975 triple-guard probe agreement")

	const scope = "scope-probe-tg2"
	const generation = "scope-probe-tg2/g1"
	h.seedScope(scope, generation)

	seed := []struct {
		id      string
		payload string
		bits    int
	}{
		{"probe-tg-clean", `{"package_id":"npm://probe-tg","repository_id":"repo-tg","subject_digest":"sha256:tg","cve_id":"CVE-TG-1","advisory_id":"GHSA-TG-1"}`, 7},
		{"probe-tg-npm", `{"package_id":"npm://probe-tg","repository_id":"repo-tg","subject_digest":"","cve_id":"CVE-TG-1","advisory_id":"GHSA-TG-1"}`, 7},
		{"probe-tg-cveonly", `{"package_id":"npm://probe-tg","repository_id":"repo-tg","subject_digest":"sha256:tg","cve_id":"CVE-TG-1","advisory_id":""}`, 7},
		{"probe-tg-noadv", `{"package_id":"npm://probe-tg","repository_id":"repo-tg","subject_digest":"sha256:tg","cve_id":"CVE-TG-1"}`, 7},
		{"probe-tg-nulladv", `{"package_id":"npm://probe-tg","repository_id":"repo-tg","subject_digest":"sha256:tg","cve_id":"CVE-TG-1","advisory_id":null}`, 7},
		{"probe-tg-nocveadv", `{"package_id":"npm://probe-tg2","repository_id":"repo-tg2","subject_digest":"sha256:tg2"}`, 7},
		{"probe-tg-bothempty", `{"package_id":"npm://probe-tg2","repository_id":"repo-tg2","subject_digest":"sha256:tg2","cve_id":"","advisory_id":""}`, 7},
		{"probe-tg-misssubj", `{"package_id":"npm://probe-tg","repository_id":"repo-tg","cve_id":"CVE-TG-1","advisory_id":"GHSA-TG-1"}`, 3},
		{"probe-tg-nullsubj", `{"package_id":"npm://probe-tg","repository_id":"repo-tg","subject_digest":null,"cve_id":"CVE-TG-1","advisory_id":"GHSA-TG-1"}`, 3},
		{"probe-tg-opmiss", `{"cve_id":"CVE-TG-9","advisory_id":"GHSA-TG-9"}`, 0},
		{"probe-tg-allempty", `{"package_id":"","repository_id":"","subject_digest":"","cve_id":"","advisory_id":""}`, 7},
		{"probe-tg-scalarpkg", `{"package_id":42,"repository_id":"repo-tg","subject_digest":"sha256:tg","cve_id":"CVE-TG-1","advisory_id":"GHSA-TG-1"}`, 7},
	}
	for _, row := range seed {
		h.exec(`
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ($1, $2, $3, $4, $1, '1.0.0', 'vulnerability_intelligence', $1, $5, $5, false, $6::jsonb)`,
			row.id+"/"+generation, scope, generation, facts.ReducerSupplyChainImpactFindingFactKind, keyedNow, row.payload)
	}
	h.syncSupports()

	// Writer fill: every support's stored bits must match its payload.
	for _, row := range seed {
		var bits int
		if err := h.db.QueryRowContext(h.ctx, `SELECT key_presence FROM supply_chain_impact_finding_supports WHERE fact_id = $1`, row.id+"/"+generation).Scan(&bits); err != nil {
			t.Fatalf("read key_presence of %s: %v", row.id, err)
		}
		if bits != row.bits {
			t.Errorf("key_presence of %s = %d, want %d", row.id, bits, row.bits)
		}
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
    package_id text, repository_id text, subject_digest text, c0 text, advisory_id text)`); err != nil {
		t.Fatalf("stage identifier table: %v", err)
	}
	if _, err := tx.ExecContext(h.ctx, `INSERT INTO `+postgres.WinnersKeyedIdentsTableForTest+` VALUES
  ('npm://probe-tg', 'repo-tg', 'sha256:tg', 'CVE-TG-1', 'GHSA-TG-1'),
  ('npm://probe-tg', 'repo-tg', '', 'CVE-TG-1', 'GHSA-TG-1'),
  ('npm://probe-tg', 'repo-tg', 'sha256:tg', 'CVE-TG-1', ''),
  ('npm://probe-tg2', 'repo-tg2', 'sha256:tg2', '', ''),
  ('', '', '', 'CVE-TG-9', 'GHSA-TG-9'),
  ('', '', '', '', ''),
  ('42', 'repo-tg', 'sha256:tg', 'CVE-TG-1', 'GHSA-TG-1')`); err != nil {
		t.Fatalf("stage identifiers: %v", err)
	}

	runProbe := func(probe string) map[string]bool {
		rows, err := tx.QueryContext(h.ctx, probe)
		if err != nil {
			t.Fatalf("run probe: %v", err)
		}
		defer func() { _ = rows.Close() }()
		got := map[string]bool{}
		for rows.Next() {
			var factID, scopeID, generationID string
			if err := rows.Scan(&factID, &scopeID, &generationID); err != nil {
				t.Fatalf("scan probe row: %v", err)
			}
			got[factID] = true
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("iterate probe rows: %v", err)
		}
		return got
	}

	stripBuild := func(build string) string {
		return strings.Replace(build, "CREATE TEMP TABLE winners_keyed_narrow ON COMMIT DROP AS", "", 1)
	}
	shippedProbe := stripBuild(postgres.WinnersKeyedNarrowBuildFromSupportsSQLForTest)
	candidateProbe := stripBuild(postgres.WinnersKeyedNarrowKeyPresenceBuildSQLForTest)
	shipped := runProbe(shippedProbe)
	candidate := runProbe(candidateProbe)

	// Exact expected set: the leniency classes match their '' idents, the
	// missing-key triple rows and the operator-like row match nothing, the
	// all-empty row matches only the operator ident.
	want := map[string]bool{
		"probe-tg-clean/" + generation:     true,
		"probe-tg-npm/" + generation:       true,
		"probe-tg-cveonly/" + generation:   true,
		"probe-tg-noadv/" + generation:     true,
		"probe-tg-nulladv/" + generation:   true,
		"probe-tg-nocveadv/" + generation:  true,
		"probe-tg-bothempty/" + generation: true,
		"probe-tg-allempty/" + generation:  true,
		"probe-tg-scalarpkg/" + generation: true,
	}
	for name, arms := range map[string]map[string]bool{"shipped": shipped, "candidate": candidate} {
		for factID := range want {
			if !arms[factID] {
				t.Errorf("%s probe misses %s", name, factID)
			}
		}
		for _, id := range []string{"probe-tg-misssubj", "probe-tg-nullsubj", "probe-tg-opmiss"} {
			if arms[id+"/"+generation] {
				t.Errorf("%s probe contains %s: missing-key rows must stay out", name, id)
			}
		}
		if len(arms) != len(want) {
			t.Errorf("%s probe returns %d rows, want %d", name, len(arms), len(want))
		}
	}

	// EXCEPT both ways is empty: agreement beyond the pinned set.
	for _, q := range []struct{ name, a, b string }{
		{"shipped-minus-candidate", shippedProbe, candidateProbe},
		{"candidate-minus-shipped", candidateProbe, shippedProbe},
	} {
		var n int
		if err := tx.QueryRowContext(h.ctx, `SELECT count(*) FROM ((`+q.a+`) EXCEPT (`+q.b+`)) AS d`).Scan(&n); err != nil {
			t.Fatalf("except %s: %v", q.name, err)
		}
		if n != 0 {
			t.Errorf("EXCEPT %s = %d, want 0", q.name, n)
		}
	}
}
