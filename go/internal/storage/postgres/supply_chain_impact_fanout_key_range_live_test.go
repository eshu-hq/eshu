// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	keyRangeHot      = "CVE-2026-10643"
	keyRangeOwn      = "kr:own"
	keyRangeLoneLast = "kr:lone-last"
	keyRangeHotRows  = 400
)

// keyRangeHolders are the holder scopes keyRangeSeed makes visible for
// keyRangeHot: five bulk holders and one lone holder per edge the key-range
// read must not lose.
var keyRangeHolders = []string{
	"kr:hold-a", "kr:hold-b", "kr:hold-c", "kr:hold-d", "kr:hold-e",
	keyRangeLoneLast, // one row, keyed in the LAST key range
	"kr:lone-null",   // one row with no impact_status
	"kr:lone-adv",    // one row matching on advisory_id only
	"kr:lone-member", // one row matching on the advisory-group member set only
	"kr:lone-below",  // one row whose fact_id sorts before every bound
	"kr:lone-above",  // one row whose fact_id sorts after every bound
}

// keyRangeSeed seeds the #10643 key-range fixture. Bulk holders carry
// keyRangeHotRows rows each with digest-shaped fact ids, so they spread over
// every key range. Lone holders carry one row each on an edge. Excluded: the
// asking scope, a tombstone-only scope, and a scope whose rows sit only at a
// superseded generation.
func keyRangeSeed(t *testing.T, ctx context.Context, sqldb *sql.DB) {
	t.Helper()
	fanOutSeedScope(t, ctx, sqldb, keyRangeOwn, "g1")
	fanOutSeedScope(t, ctx, sqldb, "kr:tomb-only", "g1")
	fanOutSeedScope(t, ctx, sqldb, "kr:superseded-only", "g2", "g1")
	for _, scope := range keyRangeHolders {
		fanOutSeedScope(t, ctx, sqldb, scope, "g1")
	}
	kind := facts.ReducerSupplyChainImpactFindingFactKind
	insert := func(label, query string, args ...any) {
		t.Helper()
		if _, err := sqldb.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload) `+query, args...); err != nil {
			t.Fatalf("seed %s: %v", label, err)
		}
	}
	insert("bulk holders", `
SELECT $1 || ':' || md5(s.scope_id || ':' || g), s.scope_id, s.active_generation_id, $1, md5(s.scope_id || ':' || g), '1.0.0',
  'synthetic', 'kr', now(), now(), false,
  jsonb_build_object('cve_id', $2::text, 'advisory_id', 'GHSA-kr-' || g,
    'impact_status', (ARRAY['affected_exact', 'possibly_affected', 'not_affected_known_fixed'])[1 + g % 3])
FROM ingestion_scopes s CROSS JOIN generate_series(1, $3) g
WHERE s.scope_id IN ('kr:hold-a', 'kr:hold-b', 'kr:hold-c', 'kr:hold-d', 'kr:hold-e', 'kr:own', 'kr:tomb-only')`,
		kind, keyRangeHot, keyRangeHotRows)
	insert("tombstones", `
SELECT fact_id || ':tomb', scope_id, generation_id, fact_kind, stable_fact_key || ':tomb', schema_version, source_system,
  source_fact_key, observed_at, ingested_at, true, payload FROM fact_records WHERE scope_id = 'kr:tomb-only'`)
	if _, err := sqldb.ExecContext(ctx, `DELETE FROM fact_records WHERE scope_id = 'kr:tomb-only' AND NOT is_tombstone`); err != nil {
		t.Fatalf("keep tombstones only: %v", err)
	}
	insert("superseded-only", `
SELECT $1 || ':' || md5('sup' || g), 'kr:superseded-only', 'kr:superseded-only/g1', $1, md5('sup' || g), '1.0.0',
  'synthetic', 'kr', now(), now(), false, jsonb_build_object('cve_id', $2::text, 'impact_status', 'affected_exact')
FROM generate_series(1, 20) g`, kind, keyRangeHot)
	lone := func(scope, factID, payload string) {
		t.Helper()
		insert(scope, `VALUES ($1, $2, $2 || '/g1', $3, $1, '1.0.0', 'synthetic', 'kr', now(), now(), false, $4::jsonb)`,
			factID, scope, kind, payload)
	}
	hot := `"` + keyRangeHot + `"`
	lone(keyRangeLoneLast, kind+":ffff0001", `{"cve_id": `+hot+`, "impact_status": "affected_exact"}`)
	lone("kr:lone-null", kind+":7777", `{"cve_id": `+hot+`}`)
	lone("kr:lone-adv", kind+":3333", `{"cve_id": "CVE-2026-other", "advisory_id": `+hot+`, "impact_status": "possibly_affected"}`)
	lone("kr:lone-member", kind+":5555", `{"cve_id": "CVE-2026-member", "advisory_id": "GHSA-kr-member", "impact_status": "affected_exact", "provenance": {"advisory_group_members": [`+hot+`]}}`)
	lone("kr:lone-below", "aaa:not-a-digest", `{"cve_id": `+hot+`, "impact_status": "affected_exact"}`)
	lone("kr:lone-above", "zzz:not-a-digest", `{"cve_id": `+hot+`, "impact_status": "affected_exact"}`)
	if _, err := sqldb.ExecContext(ctx, `ANALYZE fact_records`); err != nil {
		t.Fatalf("analyze: %v", err)
	}
}

// TestHolderKeyRangeReadMatchesShippedLive is the #10643 store contract on
// live Postgres: a hot id read as key ranges plus the member arm returns
// exactly the shipped holder read's scopes, edge holders included and
// excluded scopes excluded, and dropping the last range (seeded RED) loses the
// holders keyed there.
func TestHolderKeyRangeReadMatchesShippedLive(t *testing.T) {
	ctx, sqldb := fanOutLiveDB(t)
	keyRangeSeed(t, ctx, sqldb)
	store := fanOutLiveStore(sqldb)
	ranges := supplychaincore.HolderKeyRanges()

	shipped, err := store.FindHolderScopes(ctx, keyRangeOwn, []string{keyRangeHot})
	if err != nil {
		t.Fatalf("FindHolderScopes() error = %v", err)
	}
	want := slices.Sorted(slices.Values(keyRangeHolders))
	if !slices.Equal(shipped, want) {
		t.Fatalf("shipped holders = %v, want %v", shipped, want)
	}
	read, err := store.FindHolderScopesByKeyRange(ctx, keyRangeOwn, []string{keyRangeHot}, ranges)
	if err != nil {
		t.Fatalf("FindHolderScopesByKeyRange() error = %v", err)
	}
	if !slices.Equal(read.Scopes, shipped) {
		t.Fatalf("key-range holders = %v, want the shipped set %v", read.Scopes, shipped)
	}
	if len(read.Statements) != len(ranges)+1 {
		t.Fatalf("statements = %d, want %d ranges + 1 member arm", len(read.Statements), len(ranges))
	}
	for i, stmt := range read.Statements {
		wantKind := telemetry.FanOutReadKindHoldersKeyRange
		if i == len(ranges) {
			wantKind = telemetry.FanOutReadKindHoldersMembers
		}
		if stmt.Kind != wantKind || stmt.Err != nil {
			t.Fatalf("statement %d = %+v, want kind %s and no error", i, stmt, wantKind)
		}
	}

	red, err := store.FindHolderScopesByKeyRange(ctx, keyRangeOwn, []string{keyRangeHot}, ranges[:len(ranges)-1])
	if err != nil {
		t.Fatalf("seeded RED read error = %v", err)
	}
	for _, lost := range []string{keyRangeLoneLast, "kr:lone-above"} {
		if slices.Contains(red.Scopes, lost) {
			t.Fatalf("seeded RED: dropping the last range still found %s; the differential cannot see a lost range", lost)
		}
	}
}

// keyRangeFlipBeginner opens the store's transactions and, inside one, runs
// flip on another connection just before the second key-range statement: a
// generation flip that commits while the key-range read is in flight.
type keyRangeFlipBeginner struct {
	postgres.SQLDB
	flip func()
	// seen records every query and argument the transaction ran.
	seen *[]string
}

func (b keyRangeFlipBeginner) Begin(ctx context.Context) (db.Transaction, error) {
	tx, err := b.SQLDB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &keyRangeFlipTx{Transaction: tx, flip: b.flip, seen: b.seen}, nil
}

type keyRangeFlipTx struct {
	db.Transaction
	flip    func()
	queries int
	seen    *[]string
}

func (tx *keyRangeFlipTx) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	// Query 1 sets the statement timeout and query 2 is the first key range.
	tx.queries++
	if tx.seen != nil {
		*tx.seen = append(*tx.seen, fmt.Sprint(query, args))
	}
	if tx.queries == 3 {
		tx.flip()
	}
	return tx.Transaction.QueryContext(ctx, query, args...)
}

// TestHolderKeyRangeReadHoldsOneSnapshotLive proves the key-range statements
// share one snapshot: the lone holder keyed in the last range flips to a
// rowless generation after the first range ran, and the read still reports
// it, exactly as the shipped single statement taken before the flip did. A
// fresh shipped read after the flip no longer reports it, so the flip really
// committed (the seeded RED for a per-statement snapshot).
func TestHolderKeyRangeReadHoldsOneSnapshotLive(t *testing.T) {
	ctx, sqldb := fanOutLiveDB(t)
	keyRangeSeed(t, ctx, sqldb)
	store := fanOutLiveStore(sqldb)
	before, err := store.FindHolderScopes(ctx, keyRangeOwn, []string{keyRangeHot})
	if err != nil || !slices.Contains(before, keyRangeLoneLast) {
		t.Fatalf("shipped read before the flip = %v, %v; want it to hold %s", before, err, keyRangeLoneLast)
	}
	flipped := false
	var seen []string
	store.Beginner = keyRangeFlipBeginner{SQLDB: postgres.SQLDB{DB: sqldb}, seen: &seen, flip: func() {
		if _, err := sqldb.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
VALUES ($1 || '/g9', $1, 'synthetic', now(), now(), 'pending', now(), '{}'::jsonb)`, keyRangeLoneLast); err != nil {
			t.Fatalf("flip: seed generation: %v", err)
		}
		if _, err := sqldb.ExecContext(ctx, `UPDATE ingestion_scopes SET active_generation_id = $1 || '/g9' WHERE scope_id = $1`, keyRangeLoneLast); err != nil {
			t.Fatalf("flip: activate: %v", err)
		}
		flipped = true
	}}
	read, err := store.FindHolderScopesByKeyRange(ctx, keyRangeOwn, []string{keyRangeHot}, supplychaincore.HolderKeyRanges())
	if err != nil {
		t.Fatalf("FindHolderScopesByKeyRange() error = %v", err)
	}
	if !flipped {
		t.Fatal("the flip never ran: the proof did not interleave")
	}
	pinned := false
	for _, q := range seen {
		if strings.Contains(q, "plan_cache_mode") && strings.Contains(q, postgres.HolderKeyRangePlanCacheMode) {
			pinned = true
		}
	}
	if !pinned {
		t.Fatalf("the key-range read never pinned plan_cache_mode=%s; queries: %v", postgres.HolderKeyRangePlanCacheMode, seen)
	}
	if !slices.Equal(read.Scopes, before) {
		t.Fatalf("key-range read across the flip = %v, want the pre-flip snapshot %v", read.Scopes, before)
	}
	after, err := fanOutLiveStore(sqldb).FindHolderScopes(ctx, keyRangeOwn, []string{keyRangeHot})
	if err != nil {
		t.Fatalf("FindHolderScopes() after flip error = %v", err)
	}
	if slices.Contains(after, keyRangeLoneLast) {
		t.Fatalf("seeded RED: a fresh read after the flip still holds %s; the flip did not commit", keyRangeLoneLast)
	}
}

// TestHolderKeyRangeStatementPlanLive pins the plan the bound rests on. Under
// the plan mode the store sets (HolderKeyRangePlanCacheMode), each key-range
// arm reads its existing partial lookup index with fact_id in the index
// condition: no new index, no sequential scan, no primary-key range. The
// seeded RED is the generic plan: it cannot see the bounds and reads the
// range through fact_records_pkey, which is why the store pins the mode.
func TestHolderKeyRangeStatementPlanLive(t *testing.T) {
	ctx, sqldb := fanOutLiveDB(t)
	keyRangeSeed(t, ctx, sqldb)
	ranges := supplychaincore.HolderKeyRanges()
	mid := ranges[len(ranges)/2]
	conn, err := sqldb.Conn(ctx)
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	defer func() { _ = conn.Close() }()
	explain := func(mode string) string {
		t.Helper()
		if _, err := conn.ExecContext(ctx, `SELECT set_config('plan_cache_mode', $1, false)`, mode); err != nil {
			t.Fatalf("plan_cache_mode: %v", err)
		}
		if _, err := conn.ExecContext(ctx, `PREPARE kr_range(text[], text, text, text) AS `+postgres.FindSupplyChainImpactHolderKeyRangeQuery); err != nil {
			t.Fatalf("prepare: %v", err)
		}
		defer func() { _, _ = conn.ExecContext(ctx, `DEALLOCATE kr_range`) }()
		// EXPLAIN EXECUTE takes no bind parameters, so the test-owned values
		// are quoted literals.
		return keyRangeExplain(t, ctx, conn, `EXPLAIN (ANALYZE, BUFFERS) EXECUTE kr_range(ARRAY[`+
			keyRangeLiteral(keyRangeHot)+`], `+keyRangeLiteral(keyRangeOwn)+`, `+keyRangeLiteral(mid.Lower)+`, `+keyRangeLiteral(mid.Upper)+`)`)
	}
	plan := explain(postgres.HolderKeyRangePlanCacheMode)
	for _, banned := range []string{"Seq Scan on fact_records", "fact_records_pkey"} {
		if strings.Contains(plan, banned) {
			t.Fatalf("pinned plan reads %q:\n%s", banned, plan)
		}
	}
	for _, index := range []string{"fact_records_supply_chain_impact_lookup_idx", "fact_records_supply_chain_impact_advisory_lookup_idx"} {
		if !strings.Contains(plan, index) {
			t.Fatalf("pinned plan does not read %s:\n%s", index, plan)
		}
	}
	for _, line := range strings.Split(plan, "\n") {
		if strings.Contains(line, "Index Cond:") && strings.Contains(line, "payload ->> 'cve_id'") &&
			(!strings.Contains(line, "fact_id >=") || !strings.Contains(line, "fact_id <")) {
			t.Fatalf("fact_id bounds are not in the cve arm's index condition:\n%s", plan)
		}
		if strings.Contains(line, "Filter:") && strings.Contains(line, "fact_id") {
			t.Fatalf("fact_id bounds are a filter, not an index condition:\n%s", plan)
		}
	}
	generic := explain("force_generic_plan")
	t.Logf("seeded RED, generic plan reads fact_records_pkey: %v", strings.Contains(generic, "fact_records_pkey"))
}

// keyRangeExplain runs one EXPLAIN on conn and returns the plan text.
func keyRangeExplain(t *testing.T, ctx context.Context, conn *sql.Conn, stmt string, args ...any) string {
	t.Helper()
	rows, err := conn.QueryContext(ctx, stmt, args...)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan explain: %v", err)
		}
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("explain rows: %v", err)
	}
	plan := strings.Join(lines, "\n")
	t.Logf("plan:\n%s", plan)
	return plan
}

// keyRangeLiteral quotes a test-owned string as an SQL literal.
func keyRangeLiteral(v string) string {
	return "'" + strings.ReplaceAll(v, "'", "''") + "'"
}
