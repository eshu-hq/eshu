// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

// STAGE-2 THEORY SHIM for #10706 (arbiter ruling
// docs/internal/arbiter/10706-gatea-ruling-2026-11-15.md). This file is NOT a
// production edit: it EXPLAIN ANALYZEs the current identifier build + chunk
// select versus the proposed aged (oldest-first) variant at the 5,000
// admitted-row and 50,000 post-expansion worst cases. Kept untracked as the
// method record behind the stage numbers; the production edit reuses its
// aged-identifier shape.
//
// Current-arm SQL replicates supply_chain_impact_winners_keyed_sql.go verbatim
// (admitWinnersKeyedWindowSQL, buildWinnersKeyedScopeIdentsSQL,
// buildWinnersKeyedIdentsSQL, selectWinnersKeyedChunkIdentsSQL) with temp
// table names namespaced per arm so both arms run in one database. The shim
// arm adds the ruling's age columns (MIN(captured_at), MIN(seq)) through the
// consumed table, which is the only schema shape the ruling leaves open.

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

const shimProof = "#10706 chunk oldest-first age-cost shim"

// shimExec runs q and fails the test on error.
func shimExec(t *testing.T, ctx context.Context, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(ctx, q, args...); err != nil {
		t.Fatalf("shim exec %q: %v", q[:min(120, len(q))], err)
	}
}

var shimExecTimeRe = regexp.MustCompile(`Execution Time:\s+([0-9.]+)\s+ms`)

// shimExplain runs EXPLAIN (ANALYZE, BUFFERS) stmt inside tx and returns the
// execution time in ms plus the full plan.
func shimExplain(t *testing.T, ctx context.Context, tx *sql.Tx, stmt string, args ...any) (float64, string) {
	t.Helper()
	rows, err := tx.QueryContext(ctx, "EXPLAIN (ANALYZE, BUFFERS) "+stmt, args...)
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("explain %q: %v", stmt[:min(120, len(stmt))], err)
	}
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			_ = rows.Close()
			_ = tx.Rollback()
			t.Fatalf("scan plan: %v", err)
		}
		lines = append(lines, line)
	}
	if err := rows.Close(); err != nil {
		_ = tx.Rollback()
		t.Fatalf("close plan: %v", err)
	}
	plan := strings.Join(lines, "\n")
	m := shimExecTimeRe.FindStringSubmatch(plan)
	if m == nil {
		_ = tx.Rollback()
		t.Fatalf("no Execution Time in plan:\n%s", plan)
	}
	ms, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("parse execution time: %v", err)
	}
	return ms, plan
}

func shimMedian(xs []float64) float64 {
	cp := append([]float64(nil), xs...)
	sort.Float64s(cp)
	return cp[len(cp)/2]
}

// shimFixtureA loads the 5,000 admitted-row worst case: 5,000 fact rows
// yielding 5,000 distinct identifiers (the repo component varies per row, so
// UNION dedups nothing — the widest identifier set 5,000 rows can produce).
// Ages are staggered so the oldest identifier sorts LAST alphabetically, plus
// one planted late-commit inversion pair (INV-A: lower seq, newest capture;
// INV-B: higher seq, oldest capture of all). Real multi-row MIN grouping is
// exercised by the scope arm of fixture B.
func shimFixtureA(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	shimExec(t, ctx, db, `DELETE FROM supply_chain_impact_winners_dirty_log`)
	shimExec(t, ctx, db, `
INSERT INTO supply_chain_impact_winners_dirty_log
    (kind, source, package_id, repository_id, subject_digest, c0, advisory_id, captured_at)
SELECT 'fact', 'fact_upsert',
    'pkg:npm/p' || (g % 2499), 'repo-' || (g % 100), 'd' || (g % 2499),
    'CVE-2026-' || (g % 2499), 'ADV-' || (g % 2499),
    now() - ((((g % 2499) + 1) * 60) + (g / 2499) || ' seconds')::interval
FROM generate_series(1, 4998) g`)
	shimExec(t, ctx, db, `
INSERT INTO supply_chain_impact_winners_dirty_log
    (kind, source, package_id, repository_id, subject_digest, c0, advisory_id, captured_at)
VALUES ('fact', 'fact_upsert', 'pkg:npm/inv-a', 'repo-inv', 'd-inv-a', 'CVE-2026-INV-A', 'ADV-INV-A', now())`)
	shimExec(t, ctx, db, `
INSERT INTO supply_chain_impact_winners_dirty_log
    (kind, source, package_id, repository_id, subject_digest, c0, advisory_id, captured_at)
VALUES ('fact', 'fact_upsert', 'pkg:npm/inv-b', 'repo-inv', 'd-inv-b', 'CVE-2026-INV-B', 'ADV-INV-B', now() - interval '3 days')`)
}

// shimFixtureB loads the 50,000 post-expansion worst case that still takes the
// keyed path (over-cap is strict >): one generation holding 49,990 findings,
// two scope rows over it with different captures (forces MIN grouping in the
// scope arm), plus 10 fact rows, for exactly 50,000 distinct identifiers.
func shimFixtureB(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	scope, gen := "vuln-intel://10706/shim-scale", "vuln-intel://10706/shim-scale/g1"
	now := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	shimExec(t, ctx, db, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, payload, active_generation_id)
VALUES ($1, 'vulnerability_intelligence', 'synthetic', $1, 'synthetic', $1, $2, $2, 'active', '{}'::jsonb, NULL)`,
		scope, now)
	shimExec(t, ctx, db, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
VALUES ($1, $2, 'synthetic', $3::timestamptz, $3::timestamptz, 'active', $3::timestamptz, '{}'::jsonb)`,
		gen, scope, now)
	shimExec(t, ctx, db, `UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`, scope, gen)
	shimExec(t, ctx, db, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'shim-f' || g, $1, $2, 'reducer_supply_chain_impact_finding', 'shim-f' || g,
  'synthetic', 'k' || g, now(), now(), FALSE,
  jsonb_build_object('package_id', 'pkg:npm/bp' || (g % 5000),
    'repository_id', 'repo-' || (g % 100), 'subject_digest', 'bd' || g,
    'cve_id', 'CVE-2026-B-' || g, 'advisory_id', 'ADV-B-' || g)
FROM generate_series(1, 49990) g`, scope, gen)
	shimExec(t, ctx, db, `DELETE FROM supply_chain_impact_winners_dirty_log`)
	shimExec(t, ctx, db, `
INSERT INTO supply_chain_impact_winners_dirty_log (kind, source, scope_id, generation_id, captured_at)
VALUES ('scope', 'activation', $1, $2, now() - interval '2 hours'),
       ('scope', 'activation', $1, $2, now())`, scope, gen)
	shimExec(t, ctx, db, `
INSERT INTO supply_chain_impact_winners_dirty_log
    (kind, source, package_id, repository_id, subject_digest, c0, advisory_id, captured_at)
SELECT 'fact', 'fact_upsert',
    'pkg:npm/bf' || g, 'repo-bf', 'bfd' || g, 'CVE-2026-BF-' || g, 'ADV-BF-' || g,
    now() - ((g * 7) || ' minutes')::interval
FROM generate_series(1, 10) g`)
}

// shimMeasure runs one arm (current or aged) three times in rolled-back
// transactions and returns median per-statement execution ms. Setup statements
// (plain DDL/DML, identical in shape to production Exec calls) run
// unmeasured; only the identifier CTAS build and the chunk select are
// EXPLAINed, since those carry the added group/sort cost under test.
func shimMeasure(t *testing.T, ctx context.Context, db *sql.DB, label string, setup, explained []string, chunkSelect string) map[string]float64 {
	t.Helper()
	times := map[string][]float64{}
	var lastPlan string
	for i := 0; i < 3; i++ {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin %s iter %d: %v", label, i, err)
		}
		func() {
			defer func() { _ = tx.Rollback() }()
			for _, s := range setup {
				if _, err := tx.ExecContext(ctx, s); err != nil {
					_ = tx.Rollback()
					t.Fatalf("setup %s %q: %v", label, s[:min(60, len(s))], err)
				}
			}
			for _, s := range explained {
				if strings.HasPrefix(s, "ANALYZE ") {
					if _, err := tx.ExecContext(ctx, s); err != nil {
						_ = tx.Rollback()
						t.Fatalf("analyze %s: %v", label, err)
					}
					continue
				}
				ms, _ := shimExplain(t, ctx, tx, s)
				key := s[:min(60, len(s))]
				times[key] = append(times[key], ms)
			}
			ms, plan := shimExplain(t, ctx, tx, chunkSelect, 201)
			times["chunk-select"] = append(times["chunk-select"], ms)
			lastPlan = plan
		}()
	}
	medians := map[string]float64{}
	for k, v := range times {
		medians[k] = shimMedian(v)
	}
	t.Logf("%s chunk-select plan (last iter):\n%s", label, lastPlan)
	return medians
}

func shimTotal(m map[string]float64) float64 {
	var total float64
	for _, v := range m {
		total += v
	}
	return total
}

// shimFirstRow returns the first five-tuple of an ORDER BY variant over the
// named identifier table inside tx.
func shimFirstRow(t *testing.T, ctx context.Context, tx *sql.Tx, table, order string) string {
	t.Helper()
	var pkg, repo, digest, c0, adv string
	q := fmt.Sprintf(`SELECT package_id, repository_id, subject_digest, c0, advisory_id FROM %s %s LIMIT 1`, table, order)
	if err := tx.QueryRowContext(ctx, q).Scan(&pkg, &repo, &digest, &c0, &adv); err != nil {
		_ = tx.Rollback()
		t.Fatalf("first row %s %s: %v", table, order, err)
	}
	return pkg + "|" + repo + "|" + digest + "|" + c0 + "|" + adv
}

func TestSupplyChainImpactWinnersChunkAgeShimLive(t *testing.T) {
	_ = supplyChainImpactLiveDSN(t, shimProof)
	db := openAdvisorySuppressionIsolatedDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	for _, scale := range []struct {
		name    string
		fixture func(*testing.T, context.Context, *sql.DB)
	}{
		{"5000-admitted-rows", shimFixtureA},
		{"50000-post-expansion-idents", shimFixtureB},
	} {
		t.Run(scale.name, func(t *testing.T) {
			scale.fixture(t, ctx, db)

			current := map[string]string{
				"consumed": "shim_c_consumed",
				"scope":    "shim_c_scope_idents",
				"idents":   "shim_c_idents",
			}
			aged := map[string]string{
				"consumed": "shim_a_consumed",
				"scope":    "shim_a_scope_idents",
				"idents":   "shim_a_idents",
			}
			currentStmts := []string{
				fmt.Sprintf(`CREATE TEMP TABLE %s (
    seq bigint, kind text, source text, package_id text, repository_id text, subject_digest text,
    c0 text, advisory_id text, scope_id text, generation_id text) ON COMMIT DROP`, current["consumed"]),
				fmt.Sprintf(`INSERT INTO %s
SELECT seq, kind, source, package_id, repository_id, subject_digest, c0, advisory_id, scope_id, generation_id
FROM supply_chain_impact_winners_dirty_log ORDER BY seq LIMIT 5000`, current["consumed"]),
				fmt.Sprintf(`CREATE TEMP TABLE %s ON COMMIT DROP AS
SELECT pending.scope_id, pending.generation_id,
       COALESCE(fact.payload->>'package_id', '') AS package_id,
       COALESCE(fact.payload->>'repository_id', '') AS repository_id,
       COALESCE(fact.payload->>'subject_digest', '') AS subject_digest,
       COALESCE(NULLIF(fact.payload->>'cve_id', ''), NULLIF(fact.payload->>'advisory_id', ''), '') AS c0,
       COALESCE(fact.payload->>'advisory_id', '') AS advisory_id
FROM (SELECT DISTINCT scope_id, generation_id FROM %s WHERE kind = 'scope') AS pending
JOIN fact_records AS fact
  ON fact.scope_id = pending.scope_id
 AND fact.generation_id = pending.generation_id
WHERE fact.fact_kind = 'reducer_supply_chain_impact_finding'
  AND fact.is_tombstone = FALSE`, current["scope"], current["consumed"]),
				fmt.Sprintf(`CREATE TEMP TABLE %s ON COMMIT DROP AS
SELECT package_id, repository_id, subject_digest, c0, advisory_id
FROM %s WHERE kind = 'fact'
UNION
SELECT package_id, repository_id, subject_digest, c0, advisory_id
FROM %s`, current["idents"], current["consumed"], current["scope"]),
				`ANALYZE ` + current["idents"],
			}
			currentSelect := fmt.Sprintf(`SELECT package_id, repository_id, subject_digest, c0, advisory_id
FROM %s ORDER BY package_id, repository_id, subject_digest, c0, advisory_id LIMIT $1`, current["idents"])

			agedStmts := []string{
				fmt.Sprintf(`CREATE TEMP TABLE %s (
    seq bigint, kind text, source text, package_id text, repository_id text, subject_digest text,
    c0 text, advisory_id text, scope_id text, generation_id text,
    captured_at timestamptz) ON COMMIT DROP`, aged["consumed"]),
				fmt.Sprintf(`INSERT INTO %s
SELECT seq, kind, source, package_id, repository_id, subject_digest, c0, advisory_id, scope_id, generation_id, captured_at
FROM supply_chain_impact_winners_dirty_log ORDER BY seq LIMIT 5000`, aged["consumed"]),
				fmt.Sprintf(`CREATE TEMP TABLE %s ON COMMIT DROP AS
SELECT pending.scope_id, pending.generation_id,
       COALESCE(fact.payload->>'package_id', '') AS package_id,
       COALESCE(fact.payload->>'repository_id', '') AS repository_id,
       COALESCE(fact.payload->>'subject_digest', '') AS subject_digest,
       COALESCE(NULLIF(fact.payload->>'cve_id', ''), NULLIF(fact.payload->>'advisory_id', ''), '') AS c0,
       COALESCE(fact.payload->>'advisory_id', '') AS advisory_id
FROM (SELECT DISTINCT scope_id, generation_id FROM %s WHERE kind = 'scope') AS pending
JOIN fact_records AS fact
  ON fact.scope_id = pending.scope_id
 AND fact.generation_id = pending.generation_id
WHERE fact.fact_kind = 'reducer_supply_chain_impact_finding'
  AND fact.is_tombstone = FALSE`, aged["scope"], aged["consumed"]),
				fmt.Sprintf(`CREATE TEMP TABLE %s ON COMMIT DROP AS
SELECT package_id, repository_id, subject_digest, c0, advisory_id,
       MIN(captured_at) AS age_captured_at, MIN(seq) AS age_seq
FROM (
  SELECT package_id, repository_id, subject_digest, c0, advisory_id, captured_at, seq
  FROM %s WHERE kind = 'fact'
  UNION ALL
  SELECT si.package_id, si.repository_id, si.subject_digest, si.c0, si.advisory_id, c.captured_at, c.seq
  FROM %s AS c JOIN %s AS si
    ON si.scope_id = c.scope_id AND si.generation_id = c.generation_id
  WHERE c.kind = 'scope'
) AS aged
GROUP BY package_id, repository_id, subject_digest, c0, advisory_id`,
					aged["idents"], aged["consumed"], aged["consumed"], aged["scope"]),
				`ANALYZE ` + aged["idents"],
			}
			agedSelectCaptured := fmt.Sprintf(`SELECT package_id, repository_id, subject_digest, c0, advisory_id
FROM %s ORDER BY age_captured_at, age_seq, package_id, repository_id, subject_digest, c0, advisory_id LIMIT $1`, aged["idents"])
			agedSelectSeq := fmt.Sprintf(`SELECT package_id, repository_id, subject_digest, c0, advisory_id
FROM %s ORDER BY age_seq, package_id, repository_id, subject_digest, c0, advisory_id LIMIT $1`, aged["idents"])

			cur := shimMeasure(t, ctx, db, "current", currentStmts[:3], currentStmts[3:], currentSelect)
			shm := shimMeasure(t, ctx, db, "aged-shim", agedStmts[:3], agedStmts[3:], agedSelectCaptured)
			seq := shimMeasure(t, ctx, db, "aged-shim-seq-only", agedStmts[:3], agedStmts[3:], agedSelectSeq)

			// Same-set guard: the shim must derive the same identifier set,
			// only ordered differently. Rebuild both in one tx and compare.
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatalf("begin same-set tx: %v", err)
			}
			func() {
				defer func() { _ = tx.Rollback() }()
				for _, s := range append(append([]string{}, currentStmts...), agedStmts...) {
					if _, err := tx.ExecContext(ctx, s); err != nil {
						t.Fatalf("rebuild %q: %v", s[:min(60, len(s))], err)
					}
				}
				var nCur, nAged int
				if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM `+current["idents"]).Scan(&nCur); err != nil {
					t.Fatalf("count current idents: %v", err)
				}
				if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM `+aged["idents"]).Scan(&nAged); err != nil {
					t.Fatalf("count aged idents: %v", err)
				}
				if nCur != nAged {
					t.Errorf("identifier set changed: current %d, aged %d", nCur, nAged)
				}
				t.Logf("%s: identifiers current=%d aged=%d", scale.name, nCur, nAged)
				firstCur := shimFirstRow(t, ctx, tx, current["idents"],
					`ORDER BY package_id, repository_id, subject_digest, c0, advisory_id`)
				firstAged := shimFirstRow(t, ctx, tx, aged["idents"],
					`ORDER BY age_captured_at, age_seq, package_id, repository_id, subject_digest, c0, advisory_id`)
				firstSeq := shimFirstRow(t, ctx, tx, aged["idents"],
					`ORDER BY age_seq, package_id, repository_id, subject_digest, c0, advisory_id`)
				t.Logf("%s: first current=%s first aged(captured)=%s first aged(seq)=%s",
					scale.name, firstCur, firstAged, firstSeq)
				if scale.name == "5000-admitted-rows" {
					if !strings.Contains(firstAged, "inv-b") {
						t.Errorf("aged(captured) first pick = %s, want the inversion oldest inv-b", firstAged)
					}
					if firstSeq == firstAged {
						t.Errorf("MIN(seq) order agrees with MIN(captured_at) here; inversion case not exercised (seq=%s)", firstSeq)
					} else {
						t.Logf("inversion: MIN(seq) alone would pick %s, leaving the captured-oldest %s queued", firstSeq, firstAged)
					}
					if firstCur == firstAged {
						t.Errorf("current order already picks the oldest; defect shape not exercised")
					}
				}
			}()

			t.Logf("%s current medians ms: %+v total=%.2f", scale.name, cur, shimTotal(cur))
			t.Logf("%s aged-shim medians ms: %+v total=%.2f", scale.name, shm, shimTotal(shm))
			t.Logf("%s aged-shim-seq-only medians ms: %+v total=%.2f", scale.name, seq, shimTotal(seq))
			added := shimTotal(shm) - shimTotal(cur)
			t.Logf("%s added group/sort cost = %.2f ms against the 15000 ms per-tick bar (%.4f%%)",
				scale.name, added, 100*added/15000)
		})
	}
}
