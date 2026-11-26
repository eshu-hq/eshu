// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/facts"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// The CTE-streaming keyed winners body build (#9805 C5 / #9975 C4) replaces
// the wide active_facts temp (SELECT fact.*) with a narrow temp (copy
// identity plus the once-extracted detoast fields, no payload) and streams
// the winner body through CTEs, fetching the full payload by fact_id only
// for the emitted winner and override rows. These are the TDD acceptance
// tests: the differential proves the streamed body emits byte-identical
// winners to the legacy select, and the buffer-budget test proves the narrow
// temp plus single-evaluation stream stays within budget at QA-shaped fanout.
// Both fail before the build lands (the streamed statements do not exist).

// streamLegacySelect composes the legacy shipped winner select body over an
// arbitrary wide temp table: the production row source the streamed body must
// match.
func streamLegacySelect(table string) string {
	return `WITH active_facts AS (SELECT * FROM ` + table + `)` + postgres.WinnerBodySQLForTest
}

// streamNewSelect composes the streamed winner body over an arbitrary narrow
// temp table: the stage-1 output the tick builds.
func streamNewSelect(table string) string {
	return `WITH active_facts AS (SELECT * FROM ` + table + `)` + postgres.WinnersKeyedStreamBodySQLForTest
}

// streamWinnerColumns is the compared output column list: the canonical key
// plus every content column the winners table stores.
func streamWinnerColumns() string {
	return "canonical_key, " + strings.Join(postgres.WinnersProofDataColumns(), ", ")
}

// streamExceptCount returns the row count of (a EXCEPT ALL b) over the winner
// columns of two temp result tables.
func streamExceptCount(t *testing.T, h *keyedHarness, tx *sql.Tx, a, b string) int {
	t.Helper()
	var n int
	cols := streamWinnerColumns()
	if err := tx.QueryRowContext(h.ctx,
		`SELECT count(*) FROM (SELECT `+cols+` FROM `+a+` EXCEPT ALL SELECT `+cols+` FROM `+b+`) AS d`).Scan(&n); err != nil {
		t.Fatalf("except all %s vs %s: %v", a, b, err)
	}
	return n
}

// streamExec runs a statement in the arm transaction, failing the test with
// the named arm on error.
func streamExec(t *testing.T, h *keyedHarness, tx *sql.Tx, arm, stmt string, args ...any) {
	t.Helper()
	if _, err := tx.ExecContext(h.ctx, stmt, args...); err != nil {
		t.Fatalf("%s exec: %v", arm, err)
	}
}

// streamFactID resolves a seeded finding's fact id from the supports table:
// the writer stores findings in supports only (#10880 PR-C2), so the
// fact_records lookup the older helper uses sees no writer rows.
func streamFactID(t *testing.T, h *keyedHarness, scope, cve string) string {
	t.Helper()
	var id string
	if err := h.db.QueryRowContext(h.ctx, `SELECT fact_id FROM supply_chain_impact_finding_supports
WHERE scope_id = $1 AND payload->>'cve_id' = $2`, scope, cve).Scan(&id); err != nil {
		t.Fatalf("find support fact id for %s in %s: %v", cve, scope, err)
	}
	return id
}

// seedStreamCorpus writes the differential corpus through the production
// writer. One canonical key spans three scopes (three candidates: winner
// choice by priority plus source_count), plus an advisory-only key, every
// cvss bucket including the label path, service/workload/environment array
// states, an excluded classification and association, a derived-image scope,
// and operator overrides.
func seedStreamCorpus(t *testing.T, h *keyedHarness) {
	t.Helper()
	const scopeA = "vuln-intel://stream/a"
	const scopeB = "vuln-intel://stream/b"
	const scopeC = "vuln-intel://stream/c"
	for _, scope := range []string{scopeA, scopeB, scopeC} {
		h.seedScope(scope, scope+"/g1")
	}
	// K1 spans three scopes: the scopeB priority-20 row must win over the
	// scopeA priority-5 and scopeC priority-3 rows, with source_count 3. A
	// generation write replaces its whole set, so scopeA's K1 row rides in
	// scopeA's single write.
	f1a := keyedFinding("CVE-2026-9001", "pkg:npm/stream", 5)
	h.write(scopeB, scopeB+"/g1", keyedFinding("CVE-2026-9001", "pkg:npm/stream", 20))
	h.write(scopeC, scopeC+"/g1", keyedFinding("CVE-2026-9001", "pkg:npm/stream", 3))
	f3 := keyedFinding("CVE-2026-9003", "pkg:npm/stream-cvss", 4)
	f3.CVSSScore = 9.5
	f4 := keyedFinding("CVE-2026-9004", "pkg:npm/stream-cvss", 4)
	f4.CVSSScore = 7.2
	f5 := keyedFinding("CVE-2026-9005", "pkg:npm/stream-cvss", 4)
	f5.CVSSScore = 4.5
	f6 := keyedFinding("CVE-2026-9006", "pkg:npm/stream-cvss", 4)
	f6.CVSSScore = 0.3
	f7 := keyedFinding("CVE-2026-9007", "pkg:npm/stream-cvss", 4)
	f7.SeverityLabel = "moderate"
	f9 := keyedFinding("CVE-2026-9009", "pkg:npm/stream-cvss", 4)
	h.write(scopeA, scopeA+"/g1", f1a,
		keyedAdvisoryFinding("GHSA-stream-9002", "pkg:npm/stream-adv", 7),
		f3, f4, f5, f6, f7, f9)
	// Array states on the supports payloads (the writer's truth): present
	// arrays stay, one row keeps the explicit JSON null the encoder writes,
	// one row drops the keys entirely.
	patch := func(cve, expr string) {
		t.Helper()
		h.exec(`UPDATE supply_chain_impact_finding_supports SET payload = `+expr+`
WHERE scope_id = $1 AND payload->>'cve_id' = $2`, scopeA, cve)
	}
	patch("CVE-2026-9003", `jsonb_set(payload, '{service_ids}', '["svc-a","svc-b"]'::jsonb)`)
	patch("CVE-2026-9004", `jsonb_set(jsonb_set(payload, '{workload_ids}', '["w1"]'::jsonb), '{environments}', '["prod"]'::jsonb)`)
	patch("CVE-2026-9005", `(payload - 'service_ids' - 'workload_ids' - 'environments')`)
	// Exclusions: a classified static/active row, an associated row, and a
	// derived-image scope row all leave the candidate set in both arms. The
	// classified row would otherwise win its key with a none bucket, so its
	// absence proves the class filter, not an empty corpus.
	h.exec(`INSERT INTO scanner_image_result_fact_classifications (finding_fact_id, source_class, lifecycle)
VALUES ($1, 'static', 'active')`, streamFactID(t, h, scopeA, "CVE-2026-9009"))
	// Associations carry publication FKs the proof never builds; drop them
	// the way the trigger proof does, on the disposable database.
	h.dropForeignKeysOf("scanner_image_result_fact_associations")
	h.exec(`INSERT INTO scanner_image_result_fact_associations
(finding_fact_id, publication_id, scan_scope_id, scan_generation_id, tenant_id, workspace_id)
VALUES ($1, 'pub-stream', 'scan-stream', 'scan-gen', 'tenant', 'workspace')`,
		streamFactID(t, h, scopeA, "CVE-2026-9005"))
	const derived = "image://derived/stream"
	h.seedScope(derived, derived+"/g1")
	h.write(derived, derived+"/g1", keyedFinding("CVE-2026-9010", "pkg:npm/derived", 9))
	// Operator overrides: one live suppression with an expiry, one expired
	// suppression, and noise for a key no source holds.
	h.seedOperator("op-stream-g1", "", map[string]map[string]any{
		"op-k1": {
			"cve_id": "CVE-2026-9001", "package_id": "pkg:npm/stream",
			"impact_status": "affected_exact", "observed_version": "1.0.0",
			"repository_id": "repo-9805", "suppression_state": "accepted_risk",
			"suppression": map[string]any{"expires_at": "2099-01-01T00:00:00Z"},
		},
		"op-k4": {
			"cve_id": "CVE-2026-9004", "package_id": "pkg:npm/stream-cvss",
			"impact_status": "affected_exact", "observed_version": "1.0.0",
			"repository_id": "repo-9805", "suppression_state": "accepted_risk",
			"suppression": map[string]any{"expires_at": "2020-01-01T00:00:00Z"},
		},
		"op-noise": {
			"cve_id": "CVE-2026-0999", "suppression_state": "accepted_risk",
			"repository_id": "repo-9805",
		},
	})
	mirrorStreamFactsFromSupports(t, h)
}

// mirrorStreamFactsFromSupports seeds the legacy fact_records oracle from the
// supports truth the writer wrote: the legacy arm's statements read
// fact_records, which the stopped writer (#10880 PR-C2) no longer fills. The
// mirror carries exactly the columns the legacy probe, fetch, and body read,
// including source_confidence: the streamed arm reads it from supports, so a
// mirror that dropped it would split the arms before the derivation runs.
func mirrorStreamFactsFromSupports(t *testing.T, h *keyedHarness) {
	t.Helper()
	h.exec(`INSERT INTO fact_records
(fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
 source_fact_key, source_confidence, observed_at, ingested_at, is_tombstone, payload)
SELECT fact_id, scope_id, generation_id, $1, fact_id, 'vulnerability_intelligence',
 fact_id, source_confidence, now(), now(), false, payload
FROM supply_chain_impact_finding_supports
ON CONFLICT (fact_id) DO NOTHING`, facts.ReducerSupplyChainImpactFindingFactKind)
}

// seedStreamScalarRow appends a non-object-payload finding after the mirror:
// the h.exec trigger holiday plants the fact, and its auto-sync derives the
// support row through the writer's own upsert, so both row sources carry the
// scalar through production statements. The guarded lateral must read it as
// all-NULLs in both arms instead of aborting the build.
func seedStreamScalarRow(t *testing.T, h *keyedHarness) {
	t.Helper()
	const scope = "vuln-intel://stream/a"
	h.exec(`INSERT INTO fact_records
(fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
 source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ('scalar-fact-1', $1, $2, $3, 'scalar-fact-1', '1.0.0',
 'vulnerability_intelligence', 'scalar-fact-1', now(), now(), false, '"just-a-string"'::jsonb)`,
		scope, scope+"/g1", facts.ReducerSupplyChainImpactFindingFactKind)
	var n int
	if err := h.db.QueryRowContext(h.ctx, `SELECT count(*) FROM supply_chain_impact_finding_supports
WHERE fact_id = 'scalar-fact-1'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("scalar support row = %d, err = %v: the writer upsert must carry the scalar fact", n, err)
	}
}

// TestSupplyChainImpactWinnersKeyedStreamBodyMatchesLegacySelectLive is the
// byte-identity differential for the streaming body: over one corpus
// covering winner choice, source_count, evidence ids, jsonb-null array
// semantics, every cvss bucket, operator facts with expiry, assoc/class
// exclusion, and a non-object payload row, the streamed select over the
// narrow temp must return exactly the legacy select over the wide temp, in
// both EXCEPT ALL directions.
func TestSupplyChainImpactWinnersKeyedStreamBodyMatchesLegacySelectLive(t *testing.T) {
	h := newKeyedHarness(t, "#9805 C5 / #9975 C4 streaming body differential")
	seedStreamCorpus(t, h)
	seedStreamScalarRow(t, h)

	tx, err := h.db.BeginTx(h.ctx, nil)
	if err != nil {
		t.Fatalf("begin differential transaction: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Both arms read the same live set: every live support row by fact_id,
	// exactly what the keyed probe persists for a fully-dirtied set. The
	// scalar row rides a separate parity proof below (its payload aborts the
	// final merge in both arms identically), so the winner differential runs
	// over object payloads only.
	streamExec(t, h, tx, "setup", `CREATE TEMP TABLE `+postgres.WinnersKeyedNarrowTableForTest+
		` AS SELECT fact_id, scope_id, generation_id FROM supply_chain_impact_finding_supports
WHERE fact_id <> 'scalar-fact-1'`)
	streamExec(t, h, tx, "setup", `ANALYZE `+postgres.WinnersKeyedNarrowTableForTest)

	// Legacy arm: the shipped wide temp build plus the shipped operator
	// insert, then the shipped body over the wide temp.
	streamExec(t, h, tx, "legacy", postgres.WinnersKeyedActiveFactsSQLForTest)
	streamExec(t, h, tx, "legacy", postgres.WinnersKeyedOperatorFactsSQLForTest)
	streamExec(t, h, tx, "legacy",
		`CREATE TEMP TABLE legacy_winners AS `+streamLegacySelect(postgres.WinnersKeyedActiveFactsTableForTest))

	var legacyCount, suppressed, critical, noneBucket int
	if err := tx.QueryRowContext(h.ctx, `SELECT count(*) FROM legacy_winners`).Scan(&legacyCount); err != nil {
		t.Fatalf("count legacy winners: %v", err)
	}
	if err := tx.QueryRowContext(h.ctx,
		`SELECT count(*) FROM legacy_winners WHERE suppression_state = 'accepted_risk'`).Scan(&suppressed); err != nil {
		t.Fatalf("count suppressed legacy winners: %v", err)
	}
	if err := tx.QueryRowContext(h.ctx,
		`SELECT count(*) FROM legacy_winners WHERE severity_bucket = 'critical'`).Scan(&critical); err != nil {
		t.Fatalf("count critical legacy winners: %v", err)
	}
	if err := tx.QueryRowContext(h.ctx,
		`SELECT count(*) FROM legacy_winners WHERE severity_bucket = 'none'`).Scan(&noneBucket); err != nil {
		t.Fatalf("count none-bucket legacy winners: %v", err)
	}
	t.Logf("legacy arm: %d winners (%d suppressed, %d critical, %d none)",
		legacyCount, suppressed, critical, noneBucket)
	// Seven object-payload winners: K1, the advisory-only K2, K3, K4, K6,
	// K7, and the unscored K9. K5 stays out through its association, the
	// derived key through its scope, and the scalar row rides the separate
	// parity proof below.
	if legacyCount != 7 {
		t.Fatalf("legacy arm produced %d winners, want 7: the corpus does not cover the aspects", legacyCount)
	}
	if suppressed == 0 {
		t.Fatalf("legacy arm applied no operator override: the corpus does not cover operator facts")
	}
	if critical == 0 || noneBucket == 0 {
		t.Fatalf("legacy arm covers no critical/none bucket: the corpus does not cover the cvss aspects")
	}
	// Winner choice: the scopeB priority-20 row wins K1 over scopeA (5) and
	// scopeC (3), with all three candidates counted.
	var winnerScope string
	var sourceCount int
	if err := tx.QueryRowContext(h.ctx, `SELECT winner_scope_id, source_count FROM legacy_winners
WHERE cve_id = 'CVE-2026-9001'`).Scan(&winnerScope, &sourceCount); err != nil {
		t.Fatalf("read K1 winner: %v", err)
	}
	if winnerScope != "vuln-intel://stream/b" || sourceCount != 3 {
		t.Fatalf("K1 winner scope %s count %d, want scopeB with source_count 3", winnerScope, sourceCount)
	}
	// Exclusions: the associated key and the derived-image key stay out of
	// the winners in both arms.
	for _, cve := range []string{"CVE-2026-9005", "CVE-2026-9010"} {
		var n int
		if err := tx.QueryRowContext(h.ctx,
			`SELECT count(*) FROM legacy_winners WHERE cve_id = $1`, cve).Scan(&n); err != nil {
			t.Fatalf("count excluded %s: %v", cve, err)
		}
		if n != 0 {
			t.Fatalf("excluded key %s produced %d winners, want 0", cve, n)
		}
	}

	// Streamed arm: the narrow stage-1 temp plus the narrow operator insert,
	// then the streamed body with late payload fetch.
	streamExec(t, h, tx, "streamed", `DROP TABLE `+postgres.WinnersKeyedActiveFactsTableForTest)
	streamExec(t, h, tx, "streamed", postgres.WinnersKeyedActiveFactsNarrowBuildFromSupportsSQLForTest)
	streamExec(t, h, tx, "streamed", postgres.WinnersKeyedOperatorFactsNarrowBuildFromSupportsSQLForTest)
	streamExec(t, h, tx, "streamed",
		`CREATE TEMP TABLE stream_winners AS `+streamNewSelect(postgres.WinnersKeyedActiveFactsTableForTest))

	if got := streamExceptCount(t, h, tx, "legacy_winners", "stream_winners"); got != 0 {
		t.Errorf("legacy EXCEPT ALL streamed = %d rows, want 0", got)
	}
	if got := streamExceptCount(t, h, tx, "stream_winners", "legacy_winners"); got != 0 {
		t.Errorf("streamed EXCEPT ALL legacy = %d rows, want 0", got)
	}
}

// streamSQLState extracts the SQLSTATE of an error that wraps a Postgres
// server error, or "" when the error carries none.
func streamSQLState(err error) string {
	var pgErr *pgconn.PgError
	if err != nil && errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

// TestSupplyChainImpactWinnersKeyedStreamScalarRowParityLive is the
// non-object-payload aspect of the streaming differential. The writer always
// encodes object payloads, so production cannot plant this row; a planted
// scalar must still behave identically in both arms. Both stage-1 builds
// carry it with all-NULL extracted columns (the guarded lateral holds), and
// both selects abort the final payload merge with the same SQLSTATE: the
// `-` operator cannot delete from a scalar. Identical abort is identical
// behavior, and the shared choke point is the same expression over the same
// fetched row.
func TestSupplyChainImpactWinnersKeyedStreamScalarRowParityLive(t *testing.T) {
	h := newKeyedHarness(t, "#9805 C5 / #9975 C4 streaming scalar parity")
	seedStreamCorpus(t, h)
	seedStreamScalarRow(t, h)

	// Each arm runs in its own transaction: a failed select aborts its
	// transaction, so the two aborts must not share one.
	buildScalarTemp := func(t *testing.T, name, build, operator string) *sql.Tx {
		t.Helper()
		tx, err := h.db.BeginTx(h.ctx, nil)
		if err != nil {
			t.Fatalf("begin scalar %s transaction: %v", name, err)
		}
		t.Cleanup(func() { _ = tx.Rollback() })
		streamExec(t, h, tx, "setup", `CREATE TEMP TABLE `+postgres.WinnersKeyedNarrowTableForTest+
			` AS SELECT fact_id, scope_id, generation_id FROM supply_chain_impact_finding_supports
WHERE fact_id = 'scalar-fact-1'`)
		streamExec(t, h, tx, name, build)
		streamExec(t, h, tx, name, operator)
		return tx
	}
	expectScalarAbort := func(t *testing.T, tx *sql.Tx, name, selectSQL string) {
		t.Helper()
		rows, err := tx.QueryContext(h.ctx, selectSQL)
		if rows != nil {
			_ = rows.Close()
		}
		if got := streamSQLState(err); got != "22023" {
			t.Fatalf("%s select error state = %q, want 22023 (got err %v)", name, got, err)
		}
	}

	legacyTx := buildScalarTemp(t, "legacy",
		postgres.WinnersKeyedActiveFactsSQLForTest, postgres.WinnersKeyedOperatorFactsSQLForTest)
	// The legacy wide temp carries the payload: re-extract the scalar's
	// fields through the same guarded lateral the build ran and require
	// every field NULL.
	var wideNulls int
	if err := legacyTx.QueryRowContext(h.ctx, `SELECT count(*) FROM (
SELECT d.* FROM `+postgres.WinnersKeyedActiveFactsTableForTest+` AS fact`+
		postgres.WinnersDetoastFactLateralForTest+`
WHERE fact.fact_id = 'scalar-fact-1') AS r
WHERE `+streamAllNullPredicate(postgres.WinnersDetoastFieldsForTest)).Scan(&wideNulls); err != nil {
		t.Fatalf("re-extract scalar fields from the wide temp: %v", err)
	}
	if wideNulls != 1 {
		t.Fatalf("wide extraction of the scalar row matched %d all-NULL rows, want 1", wideNulls)
	}
	expectScalarAbort(t, legacyTx, "legacy", streamLegacySelect(postgres.WinnersKeyedActiveFactsTableForTest))

	streamTx := buildScalarTemp(t, "streamed",
		postgres.WinnersKeyedActiveFactsNarrowBuildFromSupportsSQLForTest,
		postgres.WinnersKeyedOperatorFactsNarrowBuildFromSupportsSQLForTest)
	// The narrow temp carries the already-extracted fields: the scalar's
	// row must read all-NULL there too.
	var narrowNulls int
	cols := make([]string, 0, len(postgres.WinnersDetoastFieldsForTest))
	for _, f := range postgres.WinnersDetoastFieldsForTest {
		cols = append(cols, `"`+f+`"`)
	}
	if err := streamTx.QueryRowContext(h.ctx, `SELECT count(*) FROM `+
		postgres.WinnersKeyedActiveFactsTableForTest+`
WHERE fact_id = 'scalar-fact-1' AND `+streamAllNullPredicate(cols)).Scan(&narrowNulls); err != nil {
		t.Fatalf("read scalar fields from the narrow temp: %v", err)
	}
	if narrowNulls != 1 {
		t.Fatalf("narrow temp holds %d all-NULL scalar rows, want 1", narrowNulls)
	}
	expectScalarAbort(t, streamTx, "streamed", streamNewSelect(postgres.WinnersKeyedActiveFactsTableForTest))
}

// streamAllNullPredicate renders a predicate that holds when every named
// column is NULL.
func streamAllNullPredicate(cols []string) string {
	terms := make([]string, 0, len(cols))
	for _, c := range cols {
		terms = append(terms, c+" IS NULL")
	}
	return "(" + strings.Join(terms, " AND ") + ")"
}

// seedStreamQARatioCorpus writes a QA-shaped fanout corpus through the
// production writer: identifiers with a QA-ratio copy count (~200 copies
// each, the Leg-B QA shape) and realistic payload weight.
func seedStreamQARatioCorpus(t *testing.T, h *keyedHarness, idents, copies int) {
	t.Helper()
	const scope = "vuln-intel://stream-ratio/scope"
	h.seedScope(scope, scope+"/g1")
	// One write: a generation write replaces its whole set, so all
	// identifiers ride in a single pass.
	findings := make([]supplychaincore.SupplyChainImpactFinding, 0, idents*copies)
	for i := 0; i < idents; i++ {
		for c := 0; c < copies; c++ {
			// Same five-tuple (package triple plus c0/advisory), distinct
			// versions: one identifier fans out to `copies` candidate
			// facts the probe scans, the Leg-B QA shape.
			f := keyedFinding(
				fmt.Sprintf("CVE-2026-91%02d", i),
				fmt.Sprintf("pkg:npm/ratio-%d", i),
				(c*7)%100)
			f.RepositoryID = fmt.Sprintf("repo-ratio-%d", i)
			f.ObservedVersion = fmt.Sprintf("1.0.%d", c)
			findings = append(findings, f)
		}
	}
	h.write(scope, scope+"/g1", findings...)
	// The legacy arm reads fact_records: mirror the supports truth the
	// writer wrote, exactly like the differential corpus does.
	mirrorStreamFactsFromSupports(t, h)
}

// streamExplainSharedBuffers runs EXPLAIN (ANALYZE, BUFFERS, TIMING OFF) for
// query in the arm transaction and returns the root node's shared-buffer
// hits plus reads: the plan's true total. Child lines repeat their
// parents' buffers (EXPLAIN prints cumulative counts), so summing every
// line would multiply-count. Temp-block lines are ignored: the temp-size
// bar below measures those separately through pg_relation_size.
func streamExplainSharedBuffers(t *testing.T, h *keyedHarness, tx *sql.Tx, query string) int64 {
	t.Helper()
	rows, err := tx.QueryContext(h.ctx, `EXPLAIN (ANALYZE, BUFFERS, TIMING OFF) `+query)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			_ = rows.Close()
			t.Fatalf("scan plan: %v", err)
		}
		if i := strings.Index(line, "shared "); i >= 0 {
			var hb, rb int64
			if _, err := fmt.Sscanf(line[i:], "shared hit=%d read=%d", &hb, &rb); err == nil {
				return hb + rb
			}
			if _, err := fmt.Sscanf(line[i:], "shared hit=%d", &hb); err == nil {
				return hb
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read plan: %v", err)
	}
	t.Fatalf("explain printed no shared-buffer line")
	return 0
}

// streamTempBytes returns the on-disk byte size of the named temp table in
// the arm transaction.
func streamTempBytes(t *testing.T, h *keyedHarness, tx *sql.Tx, table string) int64 {
	t.Helper()
	var n int64
	if err := tx.QueryRowContext(h.ctx,
		`SELECT pg_relation_size($1)`, table).Scan(&n); err != nil {
		t.Fatalf("temp size %s: %v", table, err)
	}
	return n
}

// streamSelectCount counts the rows a winner select returns in the arm
// transaction.
func streamSelectCount(t *testing.T, h *keyedHarness, tx *sql.Tx, selectSQL string) int {
	t.Helper()
	var n int
	if err := tx.QueryRowContext(h.ctx,
		`SELECT count(*) FROM (`+selectSQL+`) AS w`).Scan(&n); err != nil {
		t.Fatalf("count winners: %v", err)
	}
	return n
}

// streamAnalyzeBaseTables runs VACUUM ANALYZE over the seeded row sources:
// a fresh disposable database has no visibility map and no planner
// statistics, while production carries both through autovacuum. Without
// this the streamed fetch (a fact_id point lookup) plans against an
// unknown table and the bar measures missing statistics, not the build.
func streamAnalyzeBaseTables(t *testing.T, h *keyedHarness) {
	t.Helper()
	h.exec(`VACUUM (ANALYZE) supply_chain_impact_finding_supports`)
	h.exec(`VACUUM (ANALYZE) fact_records`)
}

// measureStreamArm builds one arm's temp and measures it: temp bytes, select
// shared buffers, and select row count. It runs in its own rolled-back
// transaction so both arms read the same committed snapshot.
func measureStreamArm(t *testing.T, h *keyedHarness, name, build, operator, selectSQL string) (tempBytes, buffers int64, winners int) {
	t.Helper()
	tx, err := h.db.BeginTx(h.ctx, nil)
	if err != nil {
		t.Fatalf("begin %s arm: %v", name, err)
	}
	defer func() { _ = tx.Rollback() }()
	streamExec(t, h, tx, name, `CREATE TEMP TABLE `+postgres.WinnersKeyedNarrowTableForTest+
		` AS SELECT fact_id, scope_id, generation_id FROM supply_chain_impact_finding_supports`)
	streamExec(t, h, tx, name, `ANALYZE `+postgres.WinnersKeyedNarrowTableForTest)
	streamExec(t, h, tx, name, build)
	streamExec(t, h, tx, name, operator)
	tempBytes = streamTempBytes(t, h, tx, postgres.WinnersKeyedActiveFactsTableForTest)
	buffers = streamExplainSharedBuffers(t, h, tx, selectSQL)
	winners = streamSelectCount(t, h, tx, selectSQL)
	t.Logf("%s arm: temp %d bytes, select shared buffers %d, winners %d", name, tempBytes, buffers, winners)
	return tempBytes, buffers, winners
}

// TestSupplyChainImpactWinnersKeyedStreamStaysWithinBufferBudgetLive proves
// the narrow stage-1 temp plus the single-evaluation streamed body stays
// within budget at QA-shaped fanout: the narrow temp writes at most half the
// wide temp's bytes, and the streamed evaluation touches no more shared
// buffers than the legacy select over the same snapshot.
func TestSupplyChainImpactWinnersKeyedStreamStaysWithinBufferBudgetLive(t *testing.T) {
	h := newKeyedHarness(t, "#9805 C5 / #9975 C4 streaming body buffer budget")
	seedStreamQARatioCorpus(t, h, 8, 200)
	streamAnalyzeBaseTables(t, h)

	wideBytes, _, wideWinners := measureStreamArm(t, h, "legacy",
		postgres.WinnersKeyedActiveFactsSQLForTest,
		postgres.WinnersKeyedOperatorFactsSQLForTest,
		streamLegacySelect(postgres.WinnersKeyedActiveFactsTableForTest))
	if wideWinners == 0 {
		t.Fatalf("legacy arm produced no winners: the corpus is empty")
	}
	narrowBytes, streamBuffers, streamWinners := measureStreamArm(t, h, "streamed",
		postgres.WinnersKeyedActiveFactsNarrowBuildFromSupportsSQLForTest,
		postgres.WinnersKeyedOperatorFactsNarrowBuildFromSupportsSQLForTest,
		streamNewSelect(postgres.WinnersKeyedActiveFactsTableForTest))
	if streamWinners != wideWinners {
		t.Fatalf("streamed winners %d != legacy winners %d", streamWinners, wideWinners)
	}
	// The temp budget: the payload-free narrow temp must write far less
	// than the wide temp. The fetch budget below is per emitted row: the
	// late payload fetch is a fact_id point lookup, so its buffers grow
	// with winners, never with scanned candidates.
	if narrowBytes*2 > wideBytes {
		t.Errorf("narrow temp %d bytes > wide temp %d bytes / 2: the payload-free temp must write far less",
			narrowBytes, wideBytes)
	}
	const maxBuffersPerWinner = 8
	if perWinner := float64(streamBuffers) / float64(streamWinners); perWinner > maxBuffersPerWinner {
		t.Errorf("streamed select %.1f shared buffers per winner > %d: the late fetch must stay a point lookup",
			perWinner, maxBuffersPerWinner)
	}
	streamAssertPKFetch(t, h)
}

// streamAssertPKFetch pins the streamed body's access path: both late
// payload fetches ride the supports primary key. A plan that scanned the
// supports table per winner would still pass the per-row budget on a tiny
// corpus while exploding at QA scale.
func streamAssertPKFetch(t *testing.T, h *keyedHarness) {
	t.Helper()
	tx, err := h.db.BeginTx(h.ctx, nil)
	if err != nil {
		t.Fatalf("begin fetch plan transaction: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	streamExec(t, h, tx, "plan", `CREATE TEMP TABLE `+postgres.WinnersKeyedNarrowTableForTest+
		` AS SELECT fact_id, scope_id, generation_id FROM supply_chain_impact_finding_supports LIMIT 1`)
	streamExec(t, h, tx, "plan", postgres.WinnersKeyedActiveFactsNarrowBuildFromSupportsSQLForTest)
	rows, err := tx.QueryContext(h.ctx, `EXPLAIN `+
		streamNewSelect(postgres.WinnersKeyedActiveFactsTableForTest))
	if err != nil {
		t.Fatalf("explain streamed select: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			_ = rows.Close()
			t.Fatalf("scan plan: %v", err)
		}
		plan.WriteString(line + "\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read plan: %v", err)
	}
	for _, want := range []string{
		"Index Scan using supply_chain_impact_finding_supports_pkey on supply_chain_impact_finding_supports source_payload",
		"Index Scan using supply_chain_impact_finding_supports_pkey on supply_chain_impact_finding_supports override_payload",
	} {
		if !strings.Contains(plan.String(), want) {
			t.Errorf("streamed plan lost the fetch path %q:\n%s", want, plan.String())
		}
	}
	if strings.Contains(plan.String(), "Seq Scan on supply_chain_impact_finding_supports") {
		t.Errorf("streamed plan scans the supports table: the late fetch must stay a point lookup:\n%s", plan.String())
	}
}
