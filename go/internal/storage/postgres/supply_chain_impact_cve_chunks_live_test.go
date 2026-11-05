// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9994 disposable PostgreSQL 18 proof that the chunked vulnerability_cve_id key probe returns the same rows as the whole statement at every size across chunk boundaries, in every id order, and stays on its anchor index under force_custom_plan and force_generic_plan at every chunk length, at stock random_page_cost and at 1.1; dropping the anchor is the RED control; wall time is logged, not gated
//eshu:readiness:env ESHU_SUPPLY_CHAIN_IMPACT_EVIDENCE_PROOF_DISPOSABLE ESHU_SUPPLY_CHAIN_IMPACT_EVIDENCE_PROOF_DSN

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	factimpact "github.com/eshu-hq/eshu/go/internal/storage/postgres/facts/impact"
)

// cveChunkUniverse is how many CVE ids the fixture holds rows for. Each size under
// test asks for a prefix of it, so every statement also has ids it must not return.
const cveChunkUniverse = 1200

// seedCVEChunkFixture seeds one vulnerability scope. Every id in the universe has
// an active row of each kind the probe reads, and rows the statement must never
// return: a superseded-generation row, a tombstone, and a kind the probe does not
// read. It returns the fact IDs each id should load.
func seedCVEChunkFixture(t *testing.T, ctx context.Context, database *sql.DB) map[string][]string {
	t.Helper()
	supplyChainImpactFixtureScope(t, ctx, database, "vuln:chunks", "vuln:chunks", `{}`, "gen:chunks:active",
		map[string]string{"gen:chunks:old": "superseded"})
	if _, err := database.ExecContext(ctx, `
INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
  collector_kind, fencing_token, source_confidence, source_system, source_fact_key,
  observed_at, ingested_at, is_tombstone, payload
)
SELECT format('chunk:%s:%s:%s', row_kind, kind, n), 'vuln:chunks',
  CASE row_kind WHEN 'old' THEN 'gen:chunks:old' ELSE 'gen:chunks:active' END,
  kind, format('stable:%s:%s:%s', row_kind, kind, n), '1.0.0', 'synthetic', 1, 'reported', 'synthetic',
  format('source:%s:%s:%s', row_kind, kind, n), '2026-10-23T12:00:00Z', '2026-10-23T12:00:00Z',
  row_kind = 'tomb',
  jsonb_build_object('cve_id', format('CVE-2024-%s', lpad((n - 1)::text, 5, '0')))
FROM generate_series(1, $1::integer) AS n
CROSS JOIN unnest(ARRAY['vulnerability.cve', 'vulnerability.affected_package', 'vulnerability.affected_product',
                        'vulnerability.epss_score', 'vulnerability.known_exploited']) AS kind
CROSS JOIN unnest(ARRAY['live', 'old', 'tomb']) AS row_kind`, cveChunkUniverse); err != nil {
		t.Fatalf("seed chunk fixture: %v", err)
	}
	supplyChainImpactFixtureAnalyze(t, ctx, database)
	want := map[string][]string{}
	for index, id := range cveChunkIDs(cveChunkUniverse) {
		for _, kind := range []string{
			"vulnerability.cve", "vulnerability.affected_package", "vulnerability.affected_product",
			"vulnerability.epss_score", "vulnerability.known_exploited",
		} {
			want[id] = append(want[id], fmt.Sprintf("chunk:live:%s:%d", kind, index+1))
		}
	}
	return want
}

// TestSupplyChainImpactCVEChunksMatchTheWholeStatementLive is the #9994 row-set
// proof: the loader's chunked statements and one unchunked statement over the whole
// list return the same rows, the rows the fixture says are active, at every size
// around a chunk boundary and for every id order. It also reruns each load, because
// a retried pass must see the same evidence.
func TestSupplyChainImpactCVEChunksMatchTheWholeStatementLive(t *testing.T) {
	ctx, database := openSupplyChainImpactEvidenceProofDB(t)
	wantByID := seedCVEChunkFixture(t, ctx, database)
	whole := cveChunkBranchSQL(t)
	bound := factimpact.MaxCVEIDsPerStatement
	store := NewFactStore(SQLDB{DB: database})
	for _, size := range []int{1, bound - 1, bound, bound + 1, 180, 239, 240, 241, 1000} {
		ids := cveChunkIDs(size)
		var want []string
		for _, id := range ids {
			want = append(want, wantByID[id]...)
		}
		slices.Sort(want)
		if got := cveChunkWholeStatementIDs(t, ctx, database, whole, ids); !slices.Equal(got, want) {
			t.Fatalf("size %d: the whole statement returned %d rows, want %d: the fixture and the statement disagree", size, len(got), len(want))
		}
		for order, list := range cveChunkOrderings(ids) {
			for attempt := range 2 {
				loaded, stats, err := store.ListActiveSupplyChainImpactFactsWithStats(ctx, supplychaincore.SupplyChainImpactFactFilter{CVEIDs: list})
				if err != nil {
					t.Fatalf("size %d %s attempt %d: %v", size, order, attempt, err)
				}
				got := make([]string, 0, len(loaded))
				for _, envelope := range loaded {
					got = append(got, envelope.FactID)
				}
				slices.Sort(got)
				if !slices.Equal(got, want) {
					t.Fatalf("size %d %s attempt %d: chunked load returned %d rows, want %d", size, order, attempt, len(got), len(want))
				}
				statements := 0
				for _, branch := range stats.Branches {
					if branch.Branch == cveChunkBranch {
						statements++
					}
				}
				if wantStatements := (size + bound - 1) / bound; statements != wantStatements {
					t.Fatalf("size %d %s: statements = %d, want %d", size, order, statements, wantStatements)
				}
			}
		}
	}
}

func cveChunkBranchSQL(t *testing.T) string {
	t.Helper()
	for _, branch := range factimpact.Branches() {
		if branch.Name == cveChunkBranch {
			return branch.SQL
		}
	}
	t.Fatal("catalog has no vulnerability_cve_id branch")
	return ""
}

func cveChunkWholeStatementIDs(t *testing.T, ctx context.Context, database *sql.DB, statement string, ids []string) []string {
	t.Helper()
	rows, err := database.QueryContext(ctx, statement, ids)
	if err != nil {
		t.Fatalf("whole statement: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var id, kind string
		if err := rows.Scan(&id, &kind); err != nil {
			t.Fatalf("whole statement scan: %v", err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("whole statement rows: %v", err)
	}
	slices.Sort(out)
	return out
}

// seedCVEChunkPlanFixture is seedCVEChunkFixture on a heap much larger than the
// rows the probe reads, the shape that decides the plan on QA (20 million heap pages
// against 1.08 million vulnerability rows). On a small heap the estimated heap
// fetches saturate at the table's page count, so the key probe stops getting cheaper
// than a scan of another fact_records index, whatever the list length. Low fillfactor
// builds those pages from few rows, which keeps the seed to seconds.
func seedCVEChunkPlanFixture(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	if _, err := database.ExecContext(ctx, "ALTER TABLE fact_records SET (fillfactor = 10)"); err != nil {
		t.Fatalf("set fillfactor: %v", err)
	}
	seedCVEChunkFixture(t, ctx, database)
	if _, err := database.ExecContext(ctx, `
INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
  collector_kind, fencing_token, source_confidence, source_system, source_fact_key,
  observed_at, ingested_at, payload
)
SELECT format('chunk:filler:%s', n), 'vuln:chunks', 'gen:chunks:active', 'chunk.filler', format('stable:filler:%s', n),
  '1.0.0', 'synthetic', 1, 'reported', 'synthetic', format('source:filler:%s', n),
  '2026-10-23T12:00:00Z', '2026-10-23T12:00:00Z', '{"x":"y"}'::jsonb
FROM generate_series(1, 60000) AS n`); err != nil {
		t.Fatalf("seed filler: %v", err)
	}
	supplyChainImpactFixtureAnalyze(t, ctx, database)
}

// cveKeyProbes returns every key probe on the CVE index. A new arm on that index
// joins the plan sweep below without editing it.
func cveKeyProbes(t *testing.T) []factimpact.Branch {
	t.Helper()
	var probes []factimpact.Branch
	for _, branch := range factimpact.Branches() {
		if branch.Anchor == factimpact.IndexVulnerabilityCVEV2 && branch.Shape == factimpact.ShapeKeyProbe {
			probes = append(probes, branch)
		}
	}
	if len(probes) == 0 {
		t.Fatalf("catalog has no key probe anchored on %s", factimpact.IndexVulnerabilityCVEV2)
	}
	return probes
}

// cveChunkPlan plans one prepared chunk in the session's current settings.
func cveChunkPlan(t *testing.T, ctx context.Context, conn *sql.Conn, length int) string {
	t.Helper()
	var raw []byte
	literal := supplyChainImpactSQLLiteral(t, cveChunkIDs(length))
	if err := conn.QueryRowContext(ctx, "EXPLAIN (FORMAT JSON) EXECUTE cve_chunk("+literal+")").Scan(&raw); err != nil {
		t.Fatalf("explain %d ids: %v", length, err)
	}
	return string(raw)
}

// cveChunkPlanCost returns the total cost of the plan's root node.
func cveChunkPlanCost(t *testing.T, plan string) float64 {
	t.Helper()
	var parsed []struct {
		Plan struct {
			TotalCost float64 `json:"Total Cost"`
		} `json:"Plan"`
	}
	if err := json.Unmarshal([]byte(plan), &parsed); err != nil || len(parsed) != 1 {
		t.Fatalf("parse plan cost: %v (%d plans)", err, len(parsed))
	}
	return parsed[0].Plan.TotalCost
}

// TestSupplyChainImpactCVEChunkPlansStayOnAnchorLive plans every chunk length the
// loader can issue, not only the bound, in both plan modes, at stock
// random_page_cost and at the 1.1 the QA store runs. The bound alone is not the
// proof: a list's last statement can be any shorter length, and on QA the custom
// plan walks the advisory index at 172 to 199 ids while 200 and 240 plan well. This
// fixture does not reproduce that walk, so the control is the anchor itself: with
// the index gone the same check must fail.
func TestSupplyChainImpactCVEChunkPlansStayOnAnchorLive(t *testing.T) {
	ctx, database := openSupplyChainImpactEvidenceProofDB(t)
	seedCVEChunkPlanFixture(t, ctx, database)
	conn, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("dedicated plan session: %v", err)
	}
	defer func() { _ = conn.Close() }()
	set := func(setting string) {
		t.Helper()
		if _, err := conn.ExecContext(ctx, "SET "+setting); err != nil {
			t.Fatalf("set %s: %v", setting, err)
		}
	}
	prepared := false
	prepare := func(statement string) {
		t.Helper()
		if prepared {
			if _, err := conn.ExecContext(ctx, "DEALLOCATE cve_chunk"); err != nil {
				t.Fatalf("deallocate: %v", err)
			}
		}
		if _, err := conn.ExecContext(ctx, "PREPARE cve_chunk(text[]) AS "+statement); err != nil {
			t.Fatalf("prepare: %v", err)
		}
		prepared = true
	}
	probes := cveKeyProbes(t)
	for _, probe := range probes {
		genericCost := map[string]float64{}
		for _, pageCost := range []string{"4", "1.1"} {
			// A prepared statement keeps its generic plan across a SET, so each page cost
			// needs its own PREPARE or its generic arm reads the previous one's plan.
			prepare(probe.SQL)
			set("random_page_cost = " + pageCost)
			for _, mode := range supplyChainImpactPlanModes {
				set("plan_cache_mode = " + mode)
				for length := 1; length <= factimpact.MaxCVEIDsPerStatement; length++ {
					plan := cveChunkPlan(t, ctx, conn, length)
					if problem := supplyChainImpactAnchorProblem(t, plan, factimpact.IndexVulnerabilityCVEV2); problem != "" {
						t.Errorf("%s %d ids %s random_page_cost=%s: %s\n%s", probe.Name, length, mode, pageCost, problem, supplyChainImpactPlanSummary(t, plan))
					}
					if mode == "force_generic_plan" {
						genericCost[pageCost] = cveChunkPlanCost(t, plan)
					}
				}
			}
		}
		if genericCost["4"] == genericCost["1.1"] {
			t.Errorf("%s generic plan cost %.2f at both page costs: the second loop read the first loop's cached plan", probe.Name, genericCost["4"])
		}
	}

	var definition string
	if err := conn.QueryRowContext(ctx, "SELECT pg_get_indexdef($1::regclass)", factimpact.IndexVulnerabilityCVEV2).Scan(&definition); err != nil {
		t.Fatalf("read anchor definition: %v", err)
	}
	if _, err := conn.ExecContext(ctx, "DROP INDEX "+factimpact.IndexVulnerabilityCVEV2); err != nil {
		t.Fatalf("drop anchor: %v", err)
	}
	set("random_page_cost = 4")
	for _, probe := range probes {
		prepare(probe.SQL)
		for _, mode := range supplyChainImpactPlanModes {
			set("plan_cache_mode = " + mode)
			plan := cveChunkPlan(t, ctx, conn, factimpact.MaxCVEIDsPerStatement)
			if supplyChainImpactAnchorProblem(t, plan, factimpact.IndexVulnerabilityCVEV2) == "" {
				t.Errorf("RED %s %s: the plan check passed without the anchor, so it proves nothing", probe.Name, mode)
			}
		}
	}
	if _, err := conn.ExecContext(ctx, definition); err != nil {
		t.Fatalf("rebuild anchor: %v", err)
	}
	if _, err := conn.ExecContext(ctx, "ANALYZE fact_records"); err != nil {
		t.Fatalf("analyze: %v", err)
	}
	for _, probe := range probes {
		prepare(probe.SQL)
		for _, mode := range supplyChainImpactPlanModes {
			set("plan_cache_mode = " + mode)
			plan := cveChunkPlan(t, ctx, conn, factimpact.MaxCVEIDsPerStatement)
			if problem := supplyChainImpactAnchorProblem(t, plan, factimpact.IndexVulnerabilityCVEV2); problem != "" {
				t.Errorf("GREEN %s %s: %s", probe.Name, mode, problem)
			}
		}
	}
}

// TestSupplyChainImpactCVEChunksShareOneCachedStatementLive reads what the driver
// does with the chunks. Under pgx's default exec mode each SQL text becomes one named
// prepared statement per connection, so the chunks of a long list share one plan
// cache entry: the first five executions plan custom and later ones may plan generic.
// Both stay under the bound, which is why both plan modes are proven above.
func TestSupplyChainImpactCVEChunksShareOneCachedStatementLive(t *testing.T) {
	ctx, database := openSupplyChainImpactEvidenceProofDB(t)
	seedCVEChunkPlanFixture(t, ctx, database)
	database.SetMaxOpenConns(1)
	store := NewFactStore(SQLDB{DB: database})
	if _, _, err := store.ListActiveSupplyChainImpactFacts(ctx, supplychaincore.SupplyChainImpactFactFilter{CVEIDs: cveChunkIDs(1000)}); err != nil {
		t.Fatalf("load: %v", err)
	}
	var entries int
	var custom, generic int64
	if err := database.QueryRowContext(ctx, `
SELECT count(*), COALESCE(sum(custom_plans), 0), COALESCE(sum(generic_plans), 0)
FROM pg_prepared_statements WHERE statement = $1`, cveChunkBranchSQL(t)).Scan(&entries, &custom, &generic); err != nil {
		t.Fatalf("read prepared statements: %v", err)
	}
	statements := int64((1000 + factimpact.MaxCVEIDsPerStatement - 1) / factimpact.MaxCVEIDsPerStatement)
	t.Logf("prepared entries=%d custom_plans=%d generic_plans=%d statements=%d", entries, custom, generic, statements)
	if entries != 1 || custom+generic != statements || custom < 5 {
		t.Fatalf("entries=%d custom=%d generic=%d, want one cached statement, %d executions, at least 5 custom", entries, custom, generic, statements)
	}
}

// TestSupplyChainImpactCVEChunkCostLive logs what chunking costs against the one
// whole statement under its generic plan, the steady state it replaces, interleaved
// with the first mover alternating. Wall time on a shared machine is evidence, so it
// logs and never gates.
func TestSupplyChainImpactCVEChunkCostLive(t *testing.T) {
	ctx, database := openSupplyChainImpactEvidenceProofDB(t)
	seedCVEChunkPlanFixture(t, ctx, database)
	conn, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, "SET plan_cache_mode = force_generic_plan"); err != nil {
		t.Fatalf("set plan_cache_mode: %v", err)
	}
	statement := cveChunkBranchSQL(t)
	timed := func(lists [][]string) (time.Duration, int) {
		started := time.Now()
		total := 0
		for _, list := range lists {
			rows, err := conn.QueryContext(ctx, statement, list)
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			for rows.Next() {
				total++
			}
			if err := rows.Err(); err != nil {
				t.Fatalf("rows: %v", err)
			}
			_ = rows.Close()
		}
		return time.Since(started), total
	}
	for _, size := range []int{1000, 2000} {
		ids := cveChunkIDs(size)
		chunks := slices.Collect(slices.Chunk(ids, factimpact.MaxCVEIDsPerStatement))
		var whole, chunked []time.Duration
		for run := range 7 {
			var w, c time.Duration
			var wRows, cRows int
			if run%2 == 0 {
				w, wRows = timed([][]string{ids})
				c, cRows = timed(chunks)
			} else {
				c, cRows = timed(chunks)
				w, wRows = timed([][]string{ids})
			}
			if wRows != cRows {
				t.Fatalf("size %d: whole %d rows, chunked %d", size, wRows, cRows)
			}
			whole, chunked = append(whole, w), append(chunked, c)
		}
		slices.Sort(whole)
		slices.Sort(chunked)
		t.Logf("size %d: whole statement median %s (%s..%s), %d chunks median %s (%s..%s)",
			size, whole[3], whole[0], whole[6], len(chunks), chunked[3], chunked[0], chunked[6])
	}
}
