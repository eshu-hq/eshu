// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestSupplyChainImpactWinnersNarrowProbeMatchesMissingKeyLive is the inverse
// of the #9975 missing-key exclusion pin (Refs #11691, ruled shape (b)): the
// keyed probes read a missing triple key and a JSON-null triple key as ”,
// matching the staged coalesced identifier, on both the shipped
// payload-expression probe and the plain-column probe. The exclusion the old
// pin guarded moved downstream: an operator-scope row still matches its
// coalesced identifier at the probe, but the winner select's eligibility
// (scope_id <> operator:vulnerability_suppressions) keeps it out of the
// winners, which this test proves by running the shipped supports oracle over
// the same fixture.
//
// The fixture mirrors the suppression store's free-form map payloads (see
// seedOperator): operator-scope finding facts may lack package_id and
// subject_digest keys. The c0 and advisory legs were already lenient and stay
// out of scope.
func TestSupplyChainImpactWinnersNarrowProbeMatchesMissingKeyLive(t *testing.T) {
	h := newKeyedHarness(t, "11691 missing-key probe leniency")

	const scope = "scope-probe-mk"
	const generation = "scope-probe-mk/g1"
	h.seedScope(scope, generation)

	seed := []struct {
		id      string
		payload string
	}{
		// Explicit '' subject_digest: the npm-normal shape, matched.
		{"probe-mk-e1", `{"package_id":"npm://probe-p1","repository_id":"repo-probe-1","subject_digest":"","cve_id":"CVE-PROBE-1","advisory_id":"GHSA-PROBE-1"}`},
		// Missing subject_digest key: matches the '' component after #11691.
		{"probe-mk-e2", `{"package_id":"npm://probe-p1","repository_id":"repo-probe-1","cve_id":"CVE-PROBE-1","advisory_id":"GHSA-PROBE-1"}`},
		// JSON null subject_digest: matches after #11691 (`->>` yields NULL too).
		{"probe-mk-e3", `{"package_id":"npm://probe-p1","repository_id":"repo-probe-1","subject_digest":null,"cve_id":"CVE-PROBE-1","advisory_id":"GHSA-PROBE-1"}`},
		// Clean control: no '' anywhere, matched by its own ident.
		{"probe-mk-e4", `{"package_id":"go://probe-clean","repository_id":"repo-probe-clean","subject_digest":"sha256:clean","cve_id":"CVE-PROBE-CLEAN","advisory_id":"GHSA-PROBE-CLEAN"}`},
	}
	for _, row := range seed {
		h.exec(`
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ($1, $2, $3, $4, $1, '1.0.0', 'vulnerability_intelligence', $1, $5, $5, false, $6::jsonb)`,
			row.id+"/"+generation, scope, generation, facts.ReducerSupplyChainImpactFindingFactKind, keyedNow, row.payload)
	}
	// Operator-like free-form row, triple keys absent, in the operator scope:
	// it matches the '' operator ident at the probe and is excluded
	// downstream by the winner select's operator-scope eligibility.
	h.seedOperator(generation+"-op", "", map[string]map[string]any{
		"probe-mk-e5": {"cve_id": "CVE-PROBE-1", "advisory_id": "GHSA-PROBE-1"},
	})
	h.syncSupports()

	const operatorScope = "operator:vulnerability_suppressions"

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
  ('npm://probe-p1', 'repo-probe-1', '', 'CVE-PROBE-1', 'GHSA-PROBE-1'),
  ('go://probe-clean', 'repo-probe-clean', 'sha256:clean', 'CVE-PROBE-CLEAN', 'GHSA-PROBE-CLEAN'),
  ('', '', '', 'CVE-PROBE-1', 'GHSA-PROBE-1')`); err != nil {
		t.Fatalf("stage identifiers: %v", err)
	}

	stripBuild := func(build string) string {
		return strings.Replace(build, "CREATE TEMP TABLE winners_keyed_narrow ON COMMIT DROP AS", "", 1)
	}
	probes := map[string]string{
		"shipped supports": stripBuild(postgres.WinnersKeyedNarrowBuildFromSupportsSQLForTest),
		"plain-column":     stripBuild(postgres.WinnersKeyedNarrowKeyPresenceBuildSQLForTest),
	}
	for name, probe := range probes {
		rows, err := tx.QueryContext(h.ctx, probe)
		if err != nil {
			t.Fatalf("run %s probe: %v", name, err)
		}
		got := map[string]bool{}
		for rows.Next() {
			var factID, scopeID, generationID string
			if err := rows.Scan(&factID, &scopeID, &generationID); err != nil {
				_ = rows.Close()
				t.Fatalf("scan %s probe row: %v", name, err)
			}
			got[factID] = true
		}
		_ = rows.Close()
		if err := rows.Err(); err != nil {
			t.Fatalf("iterate %s probe rows: %v", name, err)
		}
		// Explicit-'', missing-key, JSON-null, and clean copies all match
		// their coalesced identifiers.
		for _, id := range []string{"probe-mk-e1", "probe-mk-e2", "probe-mk-e3", "probe-mk-e4"} {
			if !got[id+"/"+generation] {
				t.Errorf("%s probe misses %s: the lenient triple legs must match missing and JSON-null keys as '' (Refs #11691)", name, id)
			}
		}
		// The operator-scope row matches its coalesced '' identifier at the
		// probe: scope is not a probe input, and exclusion happens
		// downstream.
		if opID := "probe-mk-e5/" + generation + "-op"; !got[opID] {
			t.Errorf("%s probe misses %s: the operator-scope row must reach the probe before downstream exclusion (Refs #11691)", name, opID)
		}
	}

	// Downstream: the shipped supports oracle crowns the e1 key (exactly one
	// winner across its explicit-''/missing/JSON-null copies) and the clean
	// key, and writes no winner for the operator-scope row.
	if _, err := h.db.ExecContext(h.ctx, `CREATE TEMP TABLE winners_downstream AS `+postgres.WinnerSelectFromSupportsSQLForTest); err != nil {
		t.Fatalf("run the supports oracle select: %v", err)
	}
	defer func() { _, _ = h.db.ExecContext(h.ctx, `DROP TABLE winners_downstream`) }()
	var keyed, clean, operator int
	if err := h.db.QueryRowContext(h.ctx, `SELECT count(*) FROM winners_downstream WHERE winner_fact_id IN ($1, $2, $3)`,
		"probe-mk-e1/"+generation, "probe-mk-e2/"+generation, "probe-mk-e3/"+generation).Scan(&keyed); err != nil {
		t.Fatalf("count e1-key winners: %v", err)
	}
	if err := h.db.QueryRowContext(h.ctx, `SELECT count(*) FROM winners_downstream WHERE winner_fact_id = $1`,
		"probe-mk-e4/"+generation).Scan(&clean); err != nil {
		t.Fatalf("count clean-key winners: %v", err)
	}
	if err := h.db.QueryRowContext(h.ctx, `SELECT count(*) FROM winners_downstream WHERE winner_fact_id = $1`,
		"probe-mk-e5/"+generation+"-op").Scan(&operator); err != nil {
		t.Fatalf("count operator-scope winners: %v", err)
	}
	if keyed != 1 {
		t.Errorf("oracle crowns %d winners for the e1 key, want 1: the missing/JSON-null copies must collapse onto the explicit-'' copy", keyed)
	}
	if clean != 1 {
		t.Errorf("oracle crowns %d winners for the clean key, want 1", clean)
	}
	if operator != 0 {
		t.Errorf("oracle crowns %d winners for the operator-scope row, want 0 (scope_id <> '%s' eligibility)", operator, operatorScope)
	}
}
