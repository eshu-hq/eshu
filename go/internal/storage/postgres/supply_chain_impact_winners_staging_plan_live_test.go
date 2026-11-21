// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11129 staging-read plan pins: no keyed per-tick staging statement, gauge sample, or rebuild-stamp minimum plans a Seq Scan of the staging table at a seeded backlog, under fresh and pre-burst stats, custom and generic plans
//eshu:readiness:env ESHU_POSTGRES_TEST_DSN ESHU_SUPPLY_CHAIN_IMPACT_PROOF_DSN ESHU_SUPPLY_CHAIN_IMPACT_PROOF_DISPOSABLE

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// winnersStagingPlanBacklog is the seeded staging backlog of the plan pins:
// six collapse budgets of fact marks plus scope marks, large enough that a
// statement whose cost follows the table instead of the batch plans a Seq
// Scan (the #11129 defect: three whole-table reads and an unranged collapse
// delete per tick).
const (
	winnersStagingPlanFactRows  = 300000
	winnersStagingPlanScopeRows = 2000
)

// TestSupplyChainImpactWinnersStagingStatementsAvoidSeqScanLive pins #11129:
// every staging statement a keyed tick runs, the pre-lease gauge sample, and
// the rebuild stamp's minimum read the staging table through an index at a
// backlog, in both stats states a burst meets (fresh ANALYZE, and stats taken
// on an empty table followed by the load with no ANALYZE), under the custom
// plan and the generic plan the services get through database/sql.
func TestSupplyChainImpactWinnersStagingStatementsAvoidSeqScanLive(t *testing.T) {
	for _, state := range []string{"fresh_stats", "pre_burst_stats"} {
		t.Run(state, func(t *testing.T) {
			ctx, database := winnersStagingPlanDB(t)
			conn, err := database.Conn(ctx)
			if err != nil {
				t.Fatalf("pin a connection: %v", err)
			}
			defer func() { _ = conn.Close() }()
			if state == "pre_burst_stats" {
				winnersStagingPlanExec(t, ctx, conn, `ALTER TABLE supply_chain_impact_winners_dirty_staging SET (autovacuum_enabled = false)`)
				winnersStagingPlanExec(t, ctx, conn, `ANALYZE supply_chain_impact_winners_dirty_staging`)
			}
			winnersStagingPlanSeed(t, ctx, conn)
			if state == "fresh_stats" {
				winnersStagingPlanExec(t, ctx, conn, `ANALYZE supply_chain_impact_winners_dirty_staging`)
			}
			for _, stmt := range winnersStagingPlanStatements(t, ctx, conn) {
				for _, mode := range []string{"force_custom_plan", "force_generic_plan"} {
					if scans := winnersStagingPlanSeqScans(t, ctx, conn, stmt, mode); scans > 0 {
						t.Errorf("%s under %s: %d Seq Scan node(s) on %s", stmt.name, mode, scans, winnersStagingTable)
					}
				}
			}
		})
	}
}

// winnersStagingPlanStatement is one shipped statement with the arguments a
// tick passes it. args are SQL literals so a PREPARE ... EXECUTE pair can
// replay them under the generic plan.
type winnersStagingPlanStatement struct {
	name string
	sql  string
	args []string
}

// winnersStagingPlanStatements lists the shipped staging statements with the
// arguments the first tick over the seeded backlog would pass them.
func winnersStagingPlanStatements(t *testing.T, ctx context.Context, conn *sql.Conn) []winnersStagingPlanStatement {
	t.Helper()
	var bound int64
	if err := conn.QueryRowContext(ctx, selectWinnersStagingCollapseBoundSQL, int64(defaultWinnersStagingCollapseMaxRows-1)).Scan(&bound); err != nil {
		t.Fatalf("read collapse bound: %v", err)
	}
	b := fmt.Sprint(bound)
	stamp := "'" + time.Now().UTC().Format(time.RFC3339Nano) + "'::timestamptz"
	return []winnersStagingPlanStatement{
		{name: "collapse stats", sql: selectWinnersStagingCollapseStatsSQL},
		{name: "collapse bound", sql: selectWinnersStagingCollapseBoundSQL, args: []string{fmt.Sprint(defaultWinnersStagingCollapseMaxRows - 1)}},
		{name: "collapse fact arm", sql: collapseWinnersStagingFactSQL, args: []string{b}},
		{name: "collapse scope arm", sql: collapseWinnersStagingScopeSQL, args: []string{b}},
		{name: "staging residual", sql: selectWinnersStagingResidualSQL},
		{name: "dirty log gauge sample", sql: selectWinnersDirtyLogStatsSQL},
		{name: "rebuild stamp", sql: rebuildSupplyChainImpactWinnersSQL, args: []string{stamp}},
	}
}

// winnersStagingPlanSeq numbers the prepared statements so each plan gets a
// fresh name without disturbing the driver's own statement cache.
var winnersStagingPlanSeq int

// winnersStagingPlanSeqScans plans stmt (EXPLAIN only, nothing executes)
// under mode and counts the Seq Scan nodes on the staging table.
func winnersStagingPlanSeqScans(t *testing.T, ctx context.Context, conn *sql.Conn, stmt winnersStagingPlanStatement, mode string) int {
	t.Helper()
	winnersStagingPlanExec(t, ctx, conn, `SET plan_cache_mode = `+mode)
	winnersStagingPlanSeq++
	name := fmt.Sprintf("winners_staging_plan_%d", winnersStagingPlanSeq)
	winnersStagingPlanExec(t, ctx, conn, `PREPARE `+name+` AS `+stmt.sql)
	explain := `EXPLAIN (FORMAT JSON) EXECUTE ` + name
	if len(stmt.args) > 0 {
		explain += `(` + strings.Join(stmt.args, ", ") + `)`
	}
	var doc string
	if err := conn.QueryRowContext(ctx, explain).Scan(&doc); err != nil {
		t.Fatalf("explain %s under %s: %v", stmt.name, mode, err)
	}
	var plans []struct {
		Plan map[string]any `json:"Plan"`
	}
	if err := json.Unmarshal([]byte(doc), &plans); err != nil || len(plans) != 1 {
		t.Fatalf("decode %s plan: %v\n%s", stmt.name, err, doc)
	}
	scans := winnersStagingPlanCountSeqScans(plans[0].Plan)
	if scans > 0 {
		t.Logf("%s under %s plan:\n%s", stmt.name, mode, doc)
	}
	return scans
}

// winnersStagingPlanCountSeqScans walks one JSON plan node and its children
// (Plans, which also carries InitPlan and SubPlan nodes) and counts Seq Scan
// nodes, parallel or not, whose relation is the staging table.
func winnersStagingPlanCountSeqScans(node map[string]any) int {
	n := 0
	if node["Node Type"] == "Seq Scan" && node["Relation Name"] == winnersStagingTable {
		n++
	}
	children, _ := node["Plans"].([]any)
	for _, child := range children {
		if c, ok := child.(map[string]any); ok {
			n += winnersStagingPlanCountSeqScans(c)
		}
	}
	return n
}

// winnersStagingPlanSeed stages the backlog the way a burst lands: fact marks
// over distinct keys in arrival order, then scope marks, one row per mark,
// with the column defaults (seq from the sequence, captured_at from the clock).
func winnersStagingPlanSeed(t *testing.T, ctx context.Context, conn *sql.Conn) {
	t.Helper()
	winnersStagingPlanExec(t, ctx, conn, fmt.Sprintf(`
INSERT INTO supply_chain_impact_winners_dirty_staging (kind, source, package_id, repository_id, subject_digest, c0, advisory_id)
SELECT 'fact', 'fact_upsert', 'pkg:npm/plan-' || k, 'repository:r_' || (k %% 977), '', 'CVE-2096-' || k, 'CVE-2096-' || k
FROM generate_series(1, %d) AS k`, winnersStagingPlanFactRows))
	winnersStagingPlanExec(t, ctx, conn, fmt.Sprintf(`
INSERT INTO supply_chain_impact_winners_dirty_staging (kind, source, scope_id, generation_id)
SELECT 'scope', 'activation', 'scope:plan:' || k, 'gen:plan:' || k
FROM generate_series(1, %d) AS k`, winnersStagingPlanScopeRows))
}

// winnersStagingPlanExec runs one statement on the pinned connection.
func winnersStagingPlanExec(t *testing.T, ctx context.Context, conn *sql.Conn, stmt string) {
	t.Helper()
	if _, err := conn.ExecContext(ctx, stmt); err != nil {
		t.Fatalf("%s: %v", strings.TrimSpace(stmt), err)
	}
}

// winnersStagingPlanDB clones a migrated disposable database for one plan
// pin. An unset DSN skips unless the supply-chain proof is required.
func winnersStagingPlanDB(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	if os.Getenv("ESHU_POSTGRES_TEST_DSN") == "" {
		if os.Getenv("ESHU_REQUIRE_SUPPLY_CHAIN_IMPACT_PROOF") == "1" {
			t.Fatal("ESHU_POSTGRES_TEST_DSN is unset with ESHU_REQUIRE_SUPPLY_CHAIN_IMPACT_PROOF=1: the #11129 staging plan pins would skip")
		}
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the #11129 staging plan pins on a disposable database")
	}
	adminDSN := os.Getenv("ESHU_SUPPLY_CHAIN_IMPACT_PROOF_DSN")
	optIn := os.Getenv("ESHU_SUPPLY_CHAIN_IMPACT_PROOF_DISPOSABLE")
	template := postgresproof.OpenBootstrapTemplate(t, adminDSN, optIn, 10*time.Minute,
		postgresproof.FlavourPublicWithoutContentSearchIndexes,
		func(ctx context.Context, database *sql.DB) error {
			return ApplyBootstrapWithoutContentSearchIndexes(ctx, SQLDB{DB: database})
		})
	return postgresproof.OpenBootstrapTemplateClone(t, adminDSN, optIn, 10*time.Minute, template)
}
