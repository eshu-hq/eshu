// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// identityPagePlan is what the plan guard reads from one EXPLAIN (FORMAT JSON)
// of the identity page query.
type identityPagePlan struct {
	raw       string
	nodeTypes []string
	// identityIndexCond and identityIndexFilter are the Index Cond and Filter
	// text of the Index Scan node on fact_records_identity_epoch_idx_v2, the
	// node the page rides when the plan is healthy.
	identityIndexScan   bool
	identityIndexCond   string
	identityIndexFilter string
	hashedSubPlan       bool
	seqScanOnFacts      bool
	sorts               bool
}

// identityPageGenericStatement names the prepared statement the generic-plan
// guard plans.
const identityPageGenericStatement = "identity_page_generic_probe"

// explainIdentityPage plans the production page query with the given keyset
// cursor (a nil cursor is the first page) and walks the plan tree. This is the
// custom plan a fresh statement gets, planned with the cursor values visible.
func explainIdentityPage(
	ctx context.Context, t *testing.T, db *sql.DB, cursor any, cursorFactID string,
) identityPagePlan {
	t.Helper()
	var raw []byte
	if err := db.QueryRowContext(ctx, `EXPLAIN (FORMAT JSON) `+listActiveContainerImageIdentityFactsQuery,
		cursor, cursorFactID, listFactsByKindPageSize).Scan(&raw); err != nil {
		t.Fatalf("explain page query: %v", err)
	}
	return decodeIdentityPagePlan(t, raw)
}

// explainIdentityPageGeneric plans the page query as a prepared statement under
// plan_cache_mode = force_generic_plan. The production store runs this one
// statement about 1,100 times per full load through a driver that caches
// prepared statements, so PostgreSQL stops replanning with the bound cursor
// after five executions and serves the generic plan, which cannot see whether
// the cursor is NULL. A bare EXPLAIN never shows that plan; PREPARE and
// EXECUTE do. The cursor is passed as a literal to EXECUTE; under
// force_generic_plan the plan still references $1/$2, which the helper checks
// so a silently custom plan cannot pass as the generic one.
func explainIdentityPageGeneric(
	ctx context.Context, t *testing.T, db *sql.DB, cursor *time.Time, cursorFactID string,
) identityPagePlan {
	t.Helper()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("open connection: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, "SET plan_cache_mode = force_generic_plan"); err != nil {
		t.Fatalf("force generic plan: %v", err)
	}
	defer func() { _, _ = conn.ExecContext(context.Background(), "RESET plan_cache_mode") }()
	prepare := "PREPARE " + identityPageGenericStatement + "(timestamptz, text, int) AS " +
		listActiveContainerImageIdentityFactsQuery
	if _, err := conn.ExecContext(ctx, prepare); err != nil {
		t.Fatalf("prepare page query: %v", err)
	}
	defer func() {
		_, _ = conn.ExecContext(context.Background(), "DEALLOCATE "+identityPageGenericStatement)
	}()
	cursorLiteral := "NULL"
	if cursor != nil {
		cursorLiteral = "'" + cursor.UTC().Format(time.RFC3339Nano) + "'"
	}
	factLiteral := "'" + strings.ReplaceAll(cursorFactID, "'", "''") + "'"
	execute := fmt.Sprintf("EXECUTE %s(%s, %s, %d)", identityPageGenericStatement,
		cursorLiteral, factLiteral, listFactsByKindPageSize)
	var raw []byte
	if err := conn.QueryRowContext(ctx, "EXPLAIN (FORMAT JSON) "+execute).Scan(&raw); err != nil {
		t.Fatalf("explain generic page query: %v", err)
	}
	if !strings.Contains(string(raw), "$1") {
		t.Fatalf("plan_cache_mode = force_generic_plan produced a custom plan (no $1):\n%s", raw)
	}
	return decodeIdentityPagePlan(t, raw)
}

// decodeIdentityPagePlan walks an EXPLAIN (FORMAT JSON) document of the page
// query.
func decodeIdentityPagePlan(t *testing.T, raw []byte) identityPagePlan {
	t.Helper()
	var plans []struct {
		Plan map[string]any `json:"Plan"`
	}
	if err := json.Unmarshal(raw, &plans); err != nil || len(plans) != 1 {
		t.Fatalf("decode plan: %v (%d plans)\n%s", err, len(plans), raw)
	}
	plan := identityPagePlan{raw: string(raw)}
	var walk func(node map[string]any)
	walk = func(node map[string]any) {
		nodeType, _ := node["Node Type"].(string)
		plan.nodeTypes = append(plan.nodeTypes, nodeType)
		relation, _ := node["Relation Name"].(string)
		index, _ := node["Index Name"].(string)
		filter, _ := node["Filter"].(string)
		cond, _ := node["Index Cond"].(string)
		if nodeType == "Index Scan" && index == "fact_records_identity_epoch_idx_v2" {
			plan.identityIndexScan = true
			plan.identityIndexCond = cond
			plan.identityIndexFilter = filter
		}
		if nodeType == "Seq Scan" && relation == "fact_records" {
			plan.seqScanOnFacts = true
		}
		if nodeType == "Sort" {
			plan.sorts = true
		}
		if strings.Contains(filter, "hashed SubPlan") {
			plan.hashedSubPlan = true
		}
		if children, ok := node["Plans"].([]any); ok {
			for _, child := range children {
				if m, ok := child.(map[string]any); ok {
					walk(m)
				}
			}
		}
	}
	walk(plans[0].Plan)
	return plan
}

// TestIdentityPageQueryPlanRidesOrderedIndexLive pins the plan shape of the
// identity page query on a real server (#7805), for the first page and for a
// mid-load page. Both must read fact_records through the ordered partial index
// fact_records_identity_epoch_idx_v2 under the LIMIT, with the
// active-generation restriction as a hashed SubPlan filter, and must not sort
// the active set or sequentially scan fact_records. The mid-load page must also
// carry the keyset comparison as an Index Cond: as a Filter every page would
// rescan from the start of the index and the load would be quadratic again,
// which a first-page-only check cannot see.
//
// The rewrite from a JOIN to that filter depends on planner behavior ("OR
// FALSE" blocks the sublink pull-up). It was verified on PostgreSQL 18; this
// test is what pins it against a server upgrade or a query edit.
//
// It also plans each page as a prepared statement under
// plan_cache_mode = force_generic_plan (PREPARE and EXECUTE, since a bare
// EXPLAIN never shows the generic plan). That plan keeps the index walk, the
// hashed SubPlan and the absence of a sort, but carries the keyset comparison
// as a Filter, so a store pinned to generic plans would rescan from the start
// of the index on every page. The test therefore also walks the whole active
// set through one prepared statement under the default plan_cache_mode = auto
// and requires zero generic plans.
//
// Run locally with ESHU_IDENTITY_EPOCH_PROOF_DSN and
// ESHU_IDENTITY_EPOCH_PROOF_DISPOSABLE=1, or ESHU_POSTGRES_TEST_DSN.
func TestIdentityPageQueryPlanRidesOrderedIndexLive(t *testing.T) {
	db, ctx := openIdentityEpochProofSchema(t)
	exec := func(query string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query); err != nil {
			t.Fatalf("exec %.80q: %v", query, err)
		}
	}
	exec(`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id)
	      SELECT 'scope-'||n, 'src', 'oci_registry', 'key-'||n, 'col', 'p'||n, now(), now(), 'active', 'gen-'||n||'-a' FROM generate_series(1,150) n`)
	exec(`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
	      SELECT 'gen-'||n||'-'||g, 'scope-'||n, 'sched', now(), now(), CASE g WHEN 'a' THEN 'active' ELSE 'superseded' END
	      FROM generate_series(1,150) n, (VALUES ('a'),('s')) gs(g)`)
	exec(`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
	      SELECT 'f-'||n||'-'||g||'-'||i, 'scope-'||n, 'gen-'||n||'-'||g, 'oci_registry.image_tag_observation',
	             'k'||n||'-'||g||'-'||i, 'oci_registry', 'sk'||i, now() - (random() * interval '30 days'), now(), '{}'::jsonb
	      FROM generate_series(1,150) n, (VALUES ('a',500),('s',400)) gs(g,cnt), LATERAL generate_series(1,gs.cnt) i`)
	exec(`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
	      SELECT 'b-'||n||'-'||i, 'scope-'||n, 'gen-'||n||'-a', 'file', 'b'||n||'-'||i, 'git', 'bk'||i, now(), now(), '{}'::jsonb
	      FROM generate_series(1,150) n, generate_series(1,800) i`)
	exec(`ANALYZE ingestion_scopes`)
	exec(`ANALYZE scope_generations`)
	exec(`ANALYZE fact_records`)

	// A mid-load cursor: the 30,000th active identity fact in keyset order.
	var cursorObservedAt time.Time
	var cursorFactID string
	if err := db.QueryRowContext(ctx,
		`SELECT observed_at, fact_id FROM fact_records
		  WHERE fact_kind = 'oci_registry.image_tag_observation' AND generation_id LIKE '%-a'
		  ORDER BY observed_at, fact_id OFFSET 30000 LIMIT 1`).Scan(&cursorObservedAt, &cursorFactID); err != nil {
		t.Fatalf("pick mid-load cursor: %v", err)
	}

	pages := []struct {
		name         string
		cursor       *time.Time
		cursorFactID string
		wantKeyset   bool
	}{
		{name: "first page", cursor: nil, cursorFactID: ""},
		{name: "mid-load page", cursor: &cursorObservedAt, cursorFactID: cursorFactID, wantKeyset: true},
	}
	for _, page := range pages {
		var customCursor any
		if page.cursor != nil {
			customCursor = page.cursor.UTC()
		}
		assertIdentityPageShape(t, page.name+" (custom plan)", page.wantKeyset,
			explainIdentityPage(ctx, t, db, customCursor, page.cursorFactID))

		// The forced generic plan cannot see whether the cursor is NULL, so the
		// keyset comparison "$1 IS NULL OR (observed_at, fact_id) > ($1, $2)" is
		// a Filter on the ordered index scan, not an Index Cond (measured on
		// PostgreSQL 18). Everything else must still hold under it. The next
		// guard proves the production driver's auto mode never reaches this plan.
		generic := explainIdentityPageGeneric(ctx, t, db, page.cursor, page.cursorFactID)
		assertIdentityPageShape(t, page.name+" (generic plan)", false, generic)
		t.Logf("%s (generic plan): keyset Index Cond %q, Filter %q", page.name, generic.identityIndexCond, generic.identityIndexFilter)
	}

	assertIdentityPageAutoModeStaysCustom(ctx, t, db)
}

// assertIdentityPageAutoModeStaysCustom walks the whole active identity set
// page by page through one prepared statement under the default
// plan_cache_mode = auto, the way the production driver (pgx statement cache)
// executes the page query, and requires that PostgreSQL never switches it to a
// generic plan. A generic plan would carry the keyset comparison as a Filter and
// make every page rescan from the start of the index. The guard reads
// pg_prepared_statements.generic_plans, so a planner or cost change that makes
// the generic plan win fails here before it reaches a full load. This guard's
// seed is 75,000 active rows (151 pages) and its walk is capped at 1,000 pages.
// A separate throwaway walk of 525,000 active rows on PostgreSQL 18, outside
// this test, took 1,051 pages with generic_plans = 0.
func assertIdentityPageAutoModeStaysCustom(ctx context.Context, t *testing.T, db *sql.DB) {
	t.Helper()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("open connection: %v", err)
	}
	defer func() { _ = conn.Close() }()
	const statement = "identity_page_auto_probe"
	if _, err := conn.ExecContext(ctx, "PREPARE "+statement+"(timestamptz, text, int) AS "+
		listActiveContainerImageIdentityFactsQuery); err != nil {
		t.Fatalf("prepare page query: %v", err)
	}
	defer func() { _, _ = conn.ExecContext(context.Background(), "DEALLOCATE "+statement) }()
	var mode string
	if err := conn.QueryRowContext(ctx, "SHOW plan_cache_mode").Scan(&mode); err != nil || mode != "auto" {
		t.Fatalf("plan_cache_mode = %q (err %v), want auto", mode, err)
	}

	cursorLiteral, factLiteral := "NULL", "''"
	pages, rowsSeen := 0, 0
	for {
		pages++
		if pages > 1000 {
			t.Fatalf("keyset walk did not finish after %d pages", pages)
		}
		rows, err := conn.QueryContext(ctx, fmt.Sprintf("EXECUTE %s(%s, %s, %d)",
			statement, cursorLiteral, factLiteral, listFactsByKindPageSize))
		if err != nil {
			t.Fatalf("execute page %d: %v", pages, err)
		}
		n := 0
		for rows.Next() {
			var factID string
			var observedAt time.Time
			// fact_id is column 1 and observed_at column 14 of the page select.
			dest := make([]any, 16)
			for i := range dest {
				dest[i] = new(any)
			}
			dest[0], dest[13] = &factID, &observedAt
			if err := rows.Scan(dest...); err != nil {
				_ = rows.Close()
				t.Fatalf("scan page %d: %v", pages, err)
			}
			cursorLiteral = "'" + observedAt.UTC().Format(time.RFC3339Nano) + "'"
			factLiteral = "'" + strings.ReplaceAll(factID, "'", "''") + "'"
			n++
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			t.Fatalf("read page %d: %v", pages, err)
		}
		_ = rows.Close()
		rowsSeen += n
		if n < listFactsByKindPageSize {
			break
		}
	}
	var generic, custom int
	if err := conn.QueryRowContext(ctx,
		"SELECT generic_plans, custom_plans FROM pg_prepared_statements WHERE name = $1", statement,
	).Scan(&generic, &custom); err != nil {
		t.Fatalf("read pg_prepared_statements: %v", err)
	}
	t.Logf("auto mode: %d pages, %d rows, generic_plans=%d custom_plans=%d", pages, rowsSeen, generic, custom)
	if generic != 0 {
		t.Errorf("auto mode switched the page query to a generic plan on %d of %d executions: its keyset comparison is a Filter, so every page rescans from the start of the index", generic, pages)
	}
	// Auto mode only considers the generic plan after five custom ones, so a
	// shorter walk could not have shown a switch.
	if pages <= 5 {
		t.Fatalf("walk too short to prove anything: %d pages, %d rows", pages, rowsSeen)
	}
}

// assertIdentityPageShape fails the test when a page plan loses the ordered
// index walk, the hashed SubPlan active-generation filter, or (for a mid-load
// page) the keyset comparison as an Index Cond.
func assertIdentityPageShape(t *testing.T, name string, wantKeyset bool, plan identityPagePlan) {
	t.Helper()
	if !plan.identityIndexScan {
		t.Errorf("%s: no Index Scan on fact_records_identity_epoch_idx_v2; node types %v\n%s", name, plan.nodeTypes, plan.raw)
	}
	if !plan.hashedSubPlan {
		t.Errorf("%s: active generations are not filtered with a hashed SubPlan; node types %v\n%s", name, plan.nodeTypes, plan.raw)
	}
	if plan.seqScanOnFacts {
		t.Errorf("%s: plan sequentially scans fact_records; node types %v\n%s", name, plan.nodeTypes, plan.raw)
	}
	if plan.sorts {
		t.Errorf("%s: plan sorts the active set before the LIMIT (the quadratic shape); node types %v\n%s", name, plan.nodeTypes, plan.raw)
	}
	if wantKeyset {
		if !strings.Contains(plan.identityIndexCond, "observed_at") {
			t.Errorf("%s: the keyset comparison is not an Index Cond on the identity index (Index Cond %q, Filter %q): every page would rescan from the start\n%s",
				name, plan.identityIndexCond, plan.identityIndexFilter, plan.raw)
		}
		if strings.Contains(plan.identityIndexFilter, "observed_at") {
			t.Errorf("%s: the keyset comparison is evaluated as a Filter (%q)\n%s", name, plan.identityIndexFilter, plan.raw)
		}
	}
}
