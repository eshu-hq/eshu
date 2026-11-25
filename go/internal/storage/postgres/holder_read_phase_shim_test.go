// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// #11430 holder-read-phase theory shim.
//
// The SCI service-time decomposition convicted holder Postgres reads at p50
// (~29s of ~51s handler): 16 holder key-range statements at ~2.3s each on QA.
// This lane proves the theory first with ONE shim: replace the 16 ranged
// statements (FindSupplyChainImpactHolderKeyRangeQuery, one per
// HolderKeyRanges range, each carrying cve/advisory btree arms bounded to its
// fact_id range plus its share of foreign-member token buckets) with the
// single shipped holder statement (FindSupplyChainImpactHoldersQuery,
// unbounded, cve/advisory/member arms once).
//
// Threshold (set BEFORE measuring): PROCEED only if the single statement is
// decisively faster, ratio = rangedTotal/singleTotal >= 2.0 on the phase.
// Anything less is STOP: no production implementation follows either way.
//
// Queries, ranges, and token shares derive from the shipped production
// constants (same package / supplychaincore) — never hand-copied — so the
// comparison cannot drift from what the handler runs. The hermetic shape test
// below pins what that derivation assumes. The live proof Skips without
// ESHU_POSTGRES_DSN + ESHU_POSTGRES_DISPOSABLE=1, so the default suite stays
// green; with a DSN it FAILS carrying the STOP verdict (or passes carrying
// PROCEED), the same verdict-signal convention as the #11430 lane shims.

// holderPhaseShimProceedRatio is the pre-declared bar: the single-statement
// shim must run the phase at most half the 16-statement total.
const holderPhaseShimProceedRatio = 2.0

// holderPhaseShimHotCVE is the hot singleton lookup id. Row volume matches
// QA's hottest id (83,642 cve-arm rows, #10643): 84,000 supports carry it.
const holderPhaseShimHotCVE = "CVE-2024-0001"

// TestHolderPhaseShimVerdictStopsBelowTwoX pins the pre-declared threshold:
// a ratio under 2.0 is STOP even when the shim is faster in absolute terms.
func TestHolderPhaseShimVerdictStopsBelowTwoX(t *testing.T) {
	t.Parallel()
	if proceed, ratio := holderPhaseShimVerdict(1500*time.Millisecond, 1000*time.Millisecond); proceed || ratio != 1.5 {
		t.Fatalf("verdict(1.5s ranged, 1.0s single) = (%v, %v), want (false, 1.5)", proceed, ratio)
	}
	if proceed, ratio := holderPhaseShimVerdict(1900*time.Millisecond, 1000*time.Millisecond); proceed || ratio != 1.9 {
		t.Fatalf("verdict(1.9s ranged, 1.0s single) = (%v, %v), want (false, 1.9)", proceed, ratio)
	}
	if proceed, ratio := holderPhaseShimVerdict(2000*time.Millisecond, 1000*time.Millisecond); !proceed || ratio != 2.0 {
		t.Fatalf("verdict(2.0s ranged, 1.0s single) = (%v, %v), want (true, 2.0)", proceed, ratio)
	}
}

// TestHolderPhaseShimShapePinsProductionContract pins what the live
// comparison assumes about production: 16 ranges partitioning the key space,
// the key-range query carrying all three arms, and the token shares
// partitioning every (bucket, id) pair exactly once across the statements.
func TestHolderPhaseShimShapePinsProductionContract(t *testing.T) {
	t.Parallel()
	ranges := supplychaincore.HolderKeyRanges()
	if len(ranges) != 16 {
		t.Fatalf("HolderKeyRanges() has %d ranges, want 16 (one statement each)", len(ranges))
	}
	if ranges[0].Lower != "" {
		t.Fatalf("first range Lower = %q, want unbounded below", ranges[0].Lower)
	}
	if ranges[len(ranges)-1].Upper != "" {
		t.Fatalf("last range Upper = %q, want unbounded above", ranges[len(ranges)-1].Upper)
	}
	for i := 1; i < len(ranges); i++ {
		if ranges[i].Lower != ranges[i-1].Upper {
			t.Fatalf("range %d Lower %q != range %d Upper %q: ranges do not partition the key space", i, ranges[i].Lower, i-1, ranges[i-1].Upper)
		}
	}
	for _, arm := range []string{"cve_id", "advisory_id", "fact_id >="} {
		if !strings.Contains(FindSupplyChainImpactHolderKeyRangeQuery, arm) {
			t.Fatalf("key-range query lacks %q arm", arm)
		}
	}
	if !strings.Contains(FindSupplyChainImpactHolderKeyRangeQuery, "eshu_sci_foreign_member_tokens_v4") {
		t.Fatal("key-range query lacks the foreign-member token arm")
	}
	for _, arm := range []string{"cve_id", "advisory_id", "advisory_group_members"} {
		if !strings.Contains(FindSupplyChainImpactHoldersQuery, arm) {
			t.Fatalf("shipped holder query lacks %q arm", arm)
		}
	}
	// Token shares partition (bucket, id) pairs exactly once across the 16
	// statements, so the union of token arms is exact.
	seen := map[string]int{}
	for s := range ranges {
		for _, tok := range HolderMemberTokens([]string{"CVE-2024-0001", "GHSA-aaaa-bbbb"}, s, len(ranges)) {
			seen[tok]++
		}
	}
	for tok, n := range seen {
		if n != 1 {
			t.Fatalf("token %q carried %d times across statements, want exactly once", tok, n)
		}
	}
	if len(seen) != 2*17 {
		t.Fatalf("token union has %d pairs, want 34 (17 buckets x 2 ids)", len(seen))
	}
}

// holderPhaseShimVerdict applies the pre-declared threshold: PROCEED only
// when the single-statement phase runs in at most half the 16-statement
// total. It lives in the test (not production) because no production code
// may change in this lane regardless of outcome.
func holderPhaseShimVerdict(rangedTotal, singleTotal time.Duration) (bool, float64) {
	ratio := float64(rangedTotal) / float64(singleTotal)
	return ratio >= holderPhaseShimProceedRatio, ratio
}

// TestSupplyChainImpactHolderReadPhaseShimLive runs the head-to-head: 16
// ranged statements in one REPEATABLE READ transaction under
// force_custom_plan (production semantics) against the single shipped holder
// statement, on QA-shaped data (84k supports on the hot CVE, ~120k
// background supports, all generations active). Both shapes must return the
// same holder set; the verdict follows holderPhaseShimProceedRatio.
func TestSupplyChainImpactHolderReadPhaseShimLive(t *testing.T) {
	db, _ := openIsolatedLiveDB(t, "holder_phase_shim", "set ESHU_POSTGRES_DSN to run the #11430 holder-read-phase shim proof")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	mustExec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("shim setup: %v\nquery: %.200s", err, query)
		}
	}
	mustExec(`CREATE TEMP TABLE shim_scopes AS
		SELECT 'scope-hot-' || g AS scope_id, 'gen-hot-' || g AS generation_id FROM generate_series(1, 1680) g
		UNION ALL SELECT 'scope-bg-' || g, 'gen-bg-' || g FROM generate_series(1, 4300) g
		UNION ALL SELECT 'scope-own-1', 'gen-own-1'`)
	mustExec(`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id)
		SELECT scope_id, 'repo', 'test', scope_id, 'test', 'test', now(), now(), 'active', generation_id FROM shim_scopes`)
	mustExec(`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
		SELECT generation_id, scope_id, 'test', now(), now(), 'active' FROM shim_scopes`)
	// Hot CVE: 84,000 supports over 1,680 scopes (50 each), uniform fact_id
	// hex so the 16 key ranges split them evenly (~5,250 per range, the QA
	// per-statement row count from #10643).
	mustExec(`INSERT INTO supply_chain_impact_finding_supports
		(fact_id, canonical_key, scope_id, generation_id, finding_id, has_payload_finding_id, priority_score , source_confidence, suppression_state, cve_id, payload)
		SELECT 'reducer_supply_chain_impact_finding:' || md5('hot-' || g),
			'hot-key-' || (g % 1680), 'scope-hot-' || ((g % 1680) + 1), 'gen-hot-' || ((g % 1680) + 1),
			'finding-hot-' || g, 0, 50, 'high', 'none', '` + holderPhaseShimHotCVE + `', '{}'::jsonb
		FROM generate_series(1, 84000) g`)
	// Background: 120,000 supports over 4,300 scopes on 200 CVEs; 2% carry
	// advisory-group members (never the hot CVE: on QA a hot CVE id has no
	// foreign rows) so both GINs hold real content.
	mustExec(`INSERT INTO supply_chain_impact_finding_supports
		(fact_id, canonical_key, scope_id, generation_id, finding_id, has_payload_finding_id, priority_score , source_confidence, suppression_state, cve_id, advisory_id, payload)
		SELECT 'reducer_supply_chain_impact_finding:' || md5('bg-' || g),
			'bg-key-' || g, 'scope-bg-' || ((g % 4300) + 1), 'gen-bg-' || ((g % 4300) + 1),
			'finding-bg-' || g, 0, 10, 'medium', 'none',
			'CVE-2023-' || LPAD(((g % 200) + 1)::text, 4, '0'),
			CASE WHEN g % 3 = 0 THEN 'GHSA-bg-' || ((g % 100) + 1) ELSE '' END,
			CASE WHEN g % 50 = 0 THEN jsonb_build_object('provenance', jsonb_build_object('advisory_group_members', jsonb_build_array('GHSA-member-' || ((g % 60) + 1), 'GHSA-shared-1')))
				ELSE '{}'::jsonb END
		FROM generate_series(1, 120000) g`)
	// Own scope holds hot rows too; the <> $2 filter must exclude it.
	mustExec(`INSERT INTO supply_chain_impact_finding_supports
		(fact_id, canonical_key, scope_id, generation_id, finding_id, has_payload_finding_id, priority_score , source_confidence, suppression_state, cve_id, payload)
		SELECT 'reducer_supply_chain_impact_finding:' || md5('own-' || g),
			'own-key-' || g, 'scope-own-1', 'gen-own-1',
			'finding-own-' || g, 0, 50, 'high', 'none', '` + holderPhaseShimHotCVE + `', '{}'::jsonb
		FROM generate_series(1, 200) g`)
	mustExec(`SELECT gin_clean_pending_list('supply_chain_impact_finding_supports_members_idx'::regclass)`)
	mustExec(`SELECT gin_clean_pending_list('supply_chain_impact_finding_supports_tokens_idx'::regclass)`)
	mustExec(`ANALYZE supply_chain_impact_finding_supports`)
	mustExec(`ANALYZE ingestion_scopes`)

	lookup := []string{holderPhaseShimHotCVE}
	ownScope := "scope-own-1"
	ranges := supplychaincore.HolderKeyRanges()

	runRanged := func() ([]string, time.Duration, error) {
		start := time.Now()
		tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
		if err != nil {
			return nil, 0, err
		}
		defer func() { _ = tx.Rollback() }()
		if _, err := tx.ExecContext(ctx, `SELECT set_config('statement_timeout', '10000', true)`); err != nil {
			return nil, 0, err
		}
		if _, err := tx.ExecContext(ctx, `SELECT set_config('plan_cache_mode', 'force_custom_plan', true)`); err != nil {
			return nil, 0, err
		}
		seen := map[string]struct{}{}
		for i, r := range ranges {
			tokens := HolderMemberTokens(lookup, i, len(ranges))
			var rows *sql.Rows
			if r.Upper == "" {
				rows, err = tx.QueryContext(ctx, FindSupplyChainImpactHolderOpenKeyRangeQuery, lookup, ownScope, r.Lower, tokens)
			} else {
				rows, err = tx.QueryContext(ctx, FindSupplyChainImpactHolderKeyRangeQuery, lookup, ownScope, r.Lower, r.Upper, tokens)
			}
			if err != nil {
				return nil, 0, fmt.Errorf("range %d: %w", i+1, err)
			}
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err != nil {
					_ = rows.Close()
					return nil, 0, err
				}
				seen[id] = struct{}{}
			}
			_ = rows.Close()
			if err := rows.Err(); err != nil {
				return nil, 0, fmt.Errorf("range %d: %w", i+1, err)
			}
		}
		return sortedKeys(seen), time.Since(start), nil
	}
	runSingle := func() ([]string, time.Duration, error) {
		start := time.Now()
		rows, err := db.QueryContext(ctx, FindSupplyChainImpactHoldersQuery, lookup, ownScope)
		if err != nil {
			return nil, 0, err
		}
		defer func() { _ = rows.Close() }()
		seen := map[string]struct{}{}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return nil, 0, err
			}
			seen[id] = struct{}{}
		}
		if err := rows.Err(); err != nil {
			return nil, 0, err
		}
		return sortedKeys(seen), time.Since(start), nil
	}

	// Warmup (cold cache out of the medians), then 5 timed runs per shape.
	if _, _, err := runRanged(); err != nil {
		t.Fatalf("ranged warmup: %v", err)
	}
	if _, _, err := runSingle(); err != nil {
		t.Fatalf("single warmup: %v", err)
	}
	var rangedScopes []string
	rangedTimes := make([]time.Duration, 0, 5)
	for i := 0; i < 5; i++ {
		scopes, dt, err := runRanged()
		if err != nil {
			t.Fatalf("ranged run %d: %v", i, err)
		}
		rangedScopes = scopes
		rangedTimes = append(rangedTimes, dt)
	}
	var singleScopes []string
	singleTimes := make([]time.Duration, 0, 5)
	for i := 0; i < 5; i++ {
		scopes, dt, err := runSingle()
		if err != nil {
			t.Fatalf("single run %d: %v", i, err)
		}
		singleScopes = scopes
		singleTimes = append(singleTimes, dt)
	}
	if len(rangedScopes) == 0 {
		t.Fatal("ranged shape returned no holders: fixture failed to match the hot CVE")
	}
	if !equalScopeSlices(rangedScopes, singleScopes) {
		t.Fatalf("answer mismatch: ranged %d holders vs single %d holders", len(rangedScopes), len(singleScopes))
	}

	// Operator attribution: one EXPLAIN (ANALYZE, BUFFERS) per shape.
	mid := ranges[7]
	midTokens := HolderMemberTokens(lookup, 7, len(ranges))
	logExplainTop(t, ctx, db, "ranged-mid", FindSupplyChainImpactHolderKeyRangeQuery, lookup, ownScope, mid.Lower, mid.Upper, midTokens)
	logExplainTop(t, ctx, db, "single", FindSupplyChainImpactHoldersQuery, lookup, ownScope)

	sort.Slice(rangedTimes, func(i, j int) bool { return rangedTimes[i] < rangedTimes[j] })
	sort.Slice(singleTimes, func(i, j int) bool { return singleTimes[i] < singleTimes[j] })
	medRanged, medSingle := rangedTimes[2], singleTimes[2]
	proceed, ratio := holderPhaseShimVerdict(medRanged, medSingle)
	t.Logf("holder phase shim: ranged16 median %v (runs %v), single median %v (runs %v), holders %d, ratio %.3f, threshold %.1f",
		medRanged, rangedTimes, medSingle, singleTimes, len(rangedScopes), ratio, holderPhaseShimProceedRatio)
	if !proceed {
		t.Fatalf("STOP: single-statement shim ratio %.3f < %.1f — no implementation follows", ratio, holderPhaseShimProceedRatio)
	}
	t.Logf("PROCEED: single-statement shim ratio %.3f >= %.1f", ratio, holderPhaseShimProceedRatio)
}

func sortedKeys(seen map[string]struct{}) []string {
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func equalScopeSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// logExplainTop runs EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) for one shape
// and logs its three costliest plan nodes, so the evidence note can name the
// expensive operator from observed plans rather than from the query text.
func logExplainTop(t *testing.T, ctx context.Context, db *sql.DB, label, query string, args ...any) {
	t.Helper()
	var raw json.RawMessage
	if err := db.QueryRowContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query, args...).Scan(&raw); err != nil {
		t.Fatalf("explain %s: %v", label, err)
	}
	var plans []struct {
		Plan map[string]any `json:"Plan"`
	}
	if err := json.Unmarshal(raw, &plans); err != nil || len(plans) == 0 {
		t.Fatalf("explain %s: decode: %v", label, err)
	}
	type node struct {
		typ     string
		detail  string
		ms      float64
		buffers int64
	}
	var all []node
	var walk func(m map[string]any)
	walk = func(m map[string]any) {
		typ, _ := m["Node Type"].(string)
		ms, _ := m["Actual Total Time"].(float64)
		var buf int64
		for _, k := range []string{"Shared Hit Blocks", "Shared Read Blocks"} {
			if f, ok := m[k].(float64); ok {
				buf += int64(f)
			}
		}
		detail := ""
		for _, k := range []string{"Index Name", "Relation Name"} {
			if s, ok := m[k].(string); ok {
				detail += " " + s
			}
		}
		all = append(all, node{typ: strings.TrimSpace(typ), detail: strings.TrimSpace(detail), ms: ms, buffers: buf})
		if kids, ok := m["Plans"].([]any); ok {
			for _, k := range kids {
				if km, ok := k.(map[string]any); ok {
					walk(km)
				}
			}
		}
	}
	walk(plans[0].Plan)
	sort.Slice(all, func(i, j int) bool { return all[i].ms > all[j].ms })
	for i := range all {
		t.Logf("explain %s node%d: %s%s %.2fms buffers=%d", label, i+1, all[i].typ, " "+all[i].detail, all[i].ms, all[i].buffers)
	}
}
