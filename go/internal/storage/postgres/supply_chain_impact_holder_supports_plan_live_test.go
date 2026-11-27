// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

const (
	holderCveIdx      = "supply_chain_impact_finding_supports_cve_idx"
	holderAdvisoryIdx = "supply_chain_impact_finding_supports_advisory_idx"
	holderMembersIdx  = "supply_chain_impact_finding_supports_members_idx"
	holderTokensIdx   = "supply_chain_impact_finding_supports_tokens_idx"
	holderSupportsTbl = "supply_chain_impact_finding_supports"
)

// holderPlanViolation returns why plan does not read the supports table through
// the named indexes, or "" when it does (#11197). Every named index must appear in
// an Index Scan, Index Only Scan or Bitmap Index Scan node, and the plan must hold
// no sequential scan of the supports table and no primary-key scan of it: what a
// missing or unusable index turns into (B1 measured 3.3 s for one key-range
// statement without them, against 4 ms with).
func holderPlanViolation(plan string, indexes ...string) string {
	for _, line := range strings.Split(plan, "\n") {
		if strings.Contains(line, "Seq Scan on "+holderSupportsTbl) {
			return "sequential scan of the supports table: " + strings.TrimSpace(line)
		}
		if strings.Contains(line, "Scan using "+holderSupportsTbl+"_pkey") {
			return "primary-key scan of the supports table: " + strings.TrimSpace(line)
		}
	}
	for _, index := range indexes {
		if !strings.Contains(plan, "Scan on "+index) && !strings.Contains(plan, "Scan using "+index) {
			return "the plan never reads " + index
		}
	}
	return ""
}

// holderPlanCorpus seeds finding facts with a hot CVE, a hot advisory, a
// repeated foreign group member and a tail of rare ids, fills the supports through
// the production backfill, and analyzes, so the planner has the statistics under
// which every arm must choose its own index.
func holderPlanCorpus(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	fanOutSeedScope(t, ctx, db, "plan-scope", "g1")
	// The corpus plant is frozen finding history, so it runs in a scoped
	// postgresproof.SeedFindingFacts window: the proof database stays armed
	// and the planner reads under test run armed.
	postgresproof.SeedFindingFacts(t, db, func() {
		if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT $2 || ':' || md5(i::text), 'plan-scope', $1, $2, 'k' || i, '1.0.0', 'synthetic', 'k' || i, now(), now(), FALSE,
  jsonb_build_object('cve_id', CASE WHEN i % 50 = 0 THEN 'CVE-HOT' ELSE 'CVE-' || (i % 9000) END,
                     'advisory_id', CASE WHEN i % 5 = 0 THEN '' WHEN i % 7 = 0 THEN 'GHSA-HOT' ELSE 'GHSA-' || (i % 6000) END,
                     'package_id', 'pkg:' || (i % 500), 'repository_id', 'repository:r' || (i % 2000),
                     'impact_status', 'affected_exact',
                     'provenance', jsonb_build_object('advisory_group_members',
                         jsonb_build_array('adv-own-' || i, 'adv-own-' || i || '-b', CASE WHEN i % 10 = 3 THEN 'adv-hot-foreign' ELSE 'adv-x-' || (i % 700) END)))
FROM generate_series(1, 30000) AS i`, fanOutGen("plan-scope", "g1"), facts.ReducerSupplyChainImpactFindingFactKind); err != nil {
			t.Fatalf("seed plan corpus: %v", err)
		}
	})
	settleSupplyChainImpactSupports(t, ctx, db)
	// VACUUM flushes the GIN pending lists the backfill left behind: the planner
	// prices a GIN scan from the pending-list size, and autovacuum does the same
	// in production before the holder reads run at scale.
	if _, err := db.ExecContext(ctx, `VACUUM (ANALYZE) `+holderSupportsTbl); err != nil {
		t.Fatalf("vacuum analyze supports: %v", err)
	}
}

// holderPlanRunner is the connection surface shared by *sql.Conn and *sql.Tx.
type holderPlanRunner interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// holderPlanStatement is one production statement with the plan mode it runs
// under, its parameter types, literal arguments, and the indexes its arms must
// read.
type holderPlanStatement struct {
	name, query, mode string
	types, literals   []string
	indexes           []string
}

func quotedArray(values ...string) string {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = "'" + strings.ReplaceAll(v, "'", "''") + "'"
	}
	return "ARRAY[" + strings.Join(quoted, ", ") + "]::text[]"
}

// holderPlanStatements derives the production statements from the shipped
// constants. The 5-id statement, the routing-mass count, and the mass probe
// run under a generic plan (the default for a prepared statement after five
// executions), the key-range statements under the custom plan the store pins.
func holderPlanStatements() []holderPlanStatement {
	five := quotedArray("CVE-HOT", "CVE-5", "GHSA-HOT", "GHSA-7", "adv-hot-foreign")
	tokens := quotedArray(postgres.HolderMemberTokens([]string{"adv-hot-foreign"}, 0, 16)...)
	hot := quotedArray("CVE-HOT")
	return []holderPlanStatement{
		{
			"5-id holder statement, generic plan", postgres.FindSupplyChainImpactHoldersQuery, "force_generic_plan",
			[]string{"text[]", "text"},
			[]string{five, "'other-scope'"},
			[]string{holderCveIdx, holderAdvisoryIdx, holderMembersIdx},
		},
		{
			"key-range statement, custom plan", postgres.FindSupplyChainImpactHolderKeyRangeQuery, "force_custom_plan",
			[]string{"text[]", "text", "text", "text", "text[]"},
			[]string{hot, "'other-scope'", "'reducer_supply_chain_impact_finding:0'", "'reducer_supply_chain_impact_finding:8'", tokens},
			[]string{holderCveIdx, holderAdvisoryIdx, holderTokensIdx},
		},
		{
			"open key-range statement, custom plan", postgres.FindSupplyChainImpactHolderOpenKeyRangeQuery, "force_custom_plan",
			[]string{"text[]", "text", "text", "text[]"},
			[]string{hot, "'other-scope'", "'reducer_supply_chain_impact_finding:f'", tokens},
			[]string{holderCveIdx, holderAdvisoryIdx, holderTokensIdx},
		},
		{
			"routing-mass count, generic plan", postgres.HolderRoutingMassQuery, "force_generic_plan",
			[]string{"text[]"},
			[]string{five},
			[]string{holderCveIdx, holderAdvisoryIdx, holderMembersIdx},
		},
		{
			"mass probe, generic plan", postgres.HolderMassProbeQuery, "force_generic_plan",
			[]string{"text[]"},
			[]string{five},
			[]string{holderCveIdx, holderAdvisoryIdx, holderMembersIdx},
		},
	}
}

// explainHolderStatement returns the plan of one statement on conn. Preparing it
// keeps a generic plan's parameters unknown to the planner, as in production.
func explainHolderStatement(t *testing.T, ctx context.Context, conn holderPlanRunner, st holderPlanStatement, analyze bool) string {
	t.Helper()
	if _, err := conn.ExecContext(ctx, "SET plan_cache_mode = "+st.mode); err != nil {
		t.Fatalf("set plan mode: %v", err)
	}
	if _, err := conn.ExecContext(ctx, fmt.Sprintf("PREPARE holder_plan_stmt(%s) AS %s", strings.Join(st.types, ", "), st.query)); err != nil {
		t.Fatalf("%s: prepare: %v", st.name, err)
	}
	defer func() { _, _ = conn.ExecContext(ctx, "DEALLOCATE holder_plan_stmt") }()
	explain := "EXPLAIN "
	if analyze {
		explain = "EXPLAIN (ANALYZE, BUFFERS) "
	}
	rows, err := conn.QueryContext(ctx, explain+"EXECUTE holder_plan_stmt("+strings.Join(st.literals, ", ")+")")
	if err != nil {
		t.Fatalf("%s: explain: %v", st.name, err)
	}
	defer func() { _ = rows.Close() }()
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// TestSupplyChainImpactSupportsHolderPlanShapeLive pins every holder statement to
// the supports indexes by name, with no sequential or primary-key scan of the
// table, and proves the checker is not vacuous: for each of the four indexes,
// dropping it inside a rolled-back transaction makes every statement pinned to it
// fail the check (#11197, arbiter-b2b).
func TestSupplyChainImpactSupportsHolderPlanShapeLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	// The corpus plants frozen finding facts, so it runs in a scoped
	// postgresproof.SeedFindingFacts window: the proof database stays armed
	// and the holder reads under test run armed.
	postgresproof.SeedFindingFacts(t, db, func() {
		holderPlanCorpus(t, ctx, db)
	})
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

	for _, st := range holderPlanStatements() {
		plan := explainHolderStatement(t, ctx, conn, st, true)
		if v := holderPlanViolation(plan, st.indexes...); v != "" {
			t.Errorf("%s: %s\n%s", st.name, v, plan)
		}
		if strings.Contains(st.name, "key-range") {
			if v := holderKeyRangePlanViolation(plan, strings.Contains(st.name, "open")); v != "" {
				t.Errorf("%s: %s\n%s", st.name, v, plan)
			}
		}
		t.Logf("%s:\n%s", st.name, plan)
	}

	// Seeded RED for the plan-mode pin: without it a prepared key-range statement
	// gets the generic plan, which cannot see the fact_id bounds and reads the
	// range through the primary key (why the store pins HolderKeyRangePlanCacheMode).
	for _, st := range holderPlanStatements() {
		// Only the bounded statement: its generic plan cannot see both bounds. The
		// open statement carries one bound and keeps its indexes either way.
		if st.query != postgres.FindSupplyChainImpactHolderKeyRangeQuery {
			continue
		}
		generic := st
		generic.mode = "force_generic_plan"
		if holderPlanViolation(explainHolderStatement(t, ctx, conn, generic, false), st.indexes...) == "" {
			t.Errorf("%s: the generic plan passed the check; re-check why the store pins %s",
				st.name, postgres.HolderKeyRangePlanCacheMode)
		}
	}

	for _, drop := range []string{holderCveIdx, holderAdvisoryIdx, holderMembersIdx, holderTokensIdx} {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(ctx, "DROP INDEX "+drop); err != nil {
			_ = tx.Rollback()
			t.Fatalf("drop %s: %v", drop, err)
		}
		rejected := 0
		for _, st := range holderPlanStatements() {
			pinned := false
			for _, ix := range st.indexes {
				pinned = pinned || ix == drop
			}
			if !pinned {
				continue
			}
			plan := explainHolderStatement(t, ctx, tx, st, false)
			if holderPlanViolation(plan, st.indexes...) == "" {
				_ = tx.Rollback()
				t.Fatalf("%s: the check accepted a plan with %s dropped:\n%s", st.name, drop, plan)
			}
			rejected++
		}
		_ = tx.Rollback()
		if rejected == 0 {
			t.Fatalf("no statement is pinned to %s", drop)
		}
	}
	// The drops were rolled back: the indexes are still there.
	var present int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pg_indexes WHERE indexname = ANY($1)`,
		quotedPgArray(holderCveIdx, holderAdvisoryIdx, holderMembersIdx, holderTokensIdx)).Scan(&present); err != nil || present != 4 {
		t.Fatalf("indexes after the rolled-back drops = %d, %v, want 4", present, err)
	}
}

func quotedPgArray(values ...string) string {
	return "{" + strings.Join(values, ",") + "}"
}
