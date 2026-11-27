// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestWinnersKeyedProbeExcludesMissingKeySupports pins the NULL-miss
// semantics the five keyed probe equalities rely on (Refs #9975).
//
// A payload equality `(payload->>'k') = ident.k` is never true when the row's
// payload lacks the key or carries a JSON null: `->>` yields SQL NULL and the
// comparison is unknown, so the row is not probed. A plain-column rewrite
// (`supports.k = ident.k`, with the column populated as
// `COALESCE(payload->>'k', ”)`) erases that missing-vs-empty distinction:
// the same row then matches every staged identifier whose component is ”.
// Staged ” components are the normal case, not the exception -- every
// language-package finding stages subject_digest ” (live QA: the whole
// queued backlog) -- so the rewrite silently widens the probe to
// missing-key rows instead of only trading heap fetches for an index-only
// scan. The c0 and advisory legs are exactly equivalent and stay out of
// scope: this guard covers the triple only.
//
// The fixture mirrors the suppression store's free-form map payloads (see
// seedOperator): operator-scope finding facts may lack package_id and
// subject_digest keys. The expected narrow set holds the explicit-” row
// and the clean control only; the missing-key, JSON-null, and operator-like
// rows must stay out. A plain-column probe text fails this test by pulling
// the missing-key rows in (demonstrated against the #9975 round-3
// construction, then reverted); the hermetic text pin
// TestWinnersKeyedProbeKeepsIndexExpressions guards the statement shape.
func TestSupplyChainImpactWinnersNarrowProbeExcludesMissingKeyLive(t *testing.T) {
	h := newKeyedHarness(t, "9975 missing-key probe guard")

	const scope = "scope-probe-mk"
	const generation = "scope-probe-mk/g1"
	h.seedScope(scope, generation)

	seed := []struct {
		id      string
		payload string
	}{
		// Explicit '' subject_digest: the npm-normal shape, matched.
		{"probe-mk-e1", `{"package_id":"npm://probe-p1","repository_id":"repo-probe-1","subject_digest":"","cve_id":"CVE-PROBE-1","advisory_id":"GHSA-PROBE-1"}`},
		// Missing subject_digest key: must stay out.
		{"probe-mk-e2", `{"package_id":"npm://probe-p1","repository_id":"repo-probe-1","cve_id":"CVE-PROBE-1","advisory_id":"GHSA-PROBE-1"}`},
		// JSON null subject_digest: must stay out (`->>` yields NULL too).
		{"probe-mk-e3", `{"package_id":"npm://probe-p1","repository_id":"repo-probe-1","subject_digest":null,"cve_id":"CVE-PROBE-1","advisory_id":"GHSA-PROBE-1"}`},
		// Clean control: no '' anywhere, matched by its own ident.
		{"probe-mk-e4", `{"package_id":"go://probe-clean","repository_id":"repo-probe-clean","subject_digest":"sha256:clean","cve_id":"CVE-PROBE-CLEAN","advisory_id":"GHSA-PROBE-CLEAN"}`},
		// Operator-like free-form row: triple keys absent, matched by no
		// populated ident, and specifically not by the '' operator ident.
		{"probe-mk-e5", `{"cve_id":"CVE-PROBE-1","advisory_id":"GHSA-PROBE-1"}`},
	}
	for _, row := range seed {
		h.exec(`
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ($1, $2, $3, $4, $1, '1.0.0', 'vulnerability_intelligence', $1, $5, $5, false, $6::jsonb)`,
			row.id+"/"+generation, scope, generation, facts.ReducerSupplyChainImpactFindingFactKind, keyedNow, row.payload)
	}
	h.syncSupports()

	// The whole-chunk narrow build over a staged identifier table, exactly
	// the shipped supports twin the tick runs. One transaction on one
	// connection: the build creates its temp table ON COMMIT DROP.
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

	want := map[string]bool{
		"probe-mk-e1/" + generation: true,
		"probe-mk-e4/" + generation: true,
	}
	for factID := range want {
		if !got[factID] {
			t.Errorf("narrow probe output misses %s: the shipped probe must match explicit-'' and clean rows", factID)
		}
	}
	for _, tt := range []struct {
		id, why string
	}{
		{"probe-mk-e2/" + generation, "missing subject_digest key"},
		{"probe-mk-e3/" + generation, "JSON null subject_digest"},
		{"probe-mk-e5/" + generation, "operator-like row under the '' operator ident"},
	} {
		if got[tt.id] {
			t.Errorf("narrow probe output contains %s (%s): the shipped probe must not match missing-key rows", tt.id, tt.why)
		}
	}
}
