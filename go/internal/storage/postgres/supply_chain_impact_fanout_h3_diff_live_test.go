// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// holderH0BaselineQuery freezes the pre-H3 holder read (three-arm OR with the
// scope join inline) as of the #10564 base, so the H3 differential below proves
// the shipped MATERIALIZED pair-union shape returns the identical scope set.
// The fact kind rides the shared constant so a kind rename breaks the build
// instead of silently skewing the baseline; the OR shape itself is frozen and
// may only change with a newly proved baseline.
const holderH0BaselineQuery = `
SELECT DISTINCT finding.scope_id
FROM fact_records AS finding
JOIN ingestion_scopes AS scope
  ON scope.scope_id = finding.scope_id
 AND scope.active_generation_id = finding.generation_id
WHERE finding.fact_kind = '` + facts.ReducerSupplyChainImpactFindingFactKind + `'
  AND finding.is_tombstone = FALSE
  AND (finding.payload->>'cve_id' = ANY($1::text[])
       OR finding.payload->>'advisory_id' = ANY($1::text[])
       OR finding.payload #> '{provenance,advisory_group_members}' ?| $1::text[])
  AND finding.scope_id <> $2
ORDER BY finding.scope_id
`

const (
	h3diffOwn   = "h3diff:own"
	h3diffHoldB = "h3diff:holder-b"
	h3diffHoldC = "h3diff:holder-c"
	h3diffTomb  = "h3diff:tomb-only"
	h3diffOld   = "h3diff:old-only"
	h3diffBulkN = 5000
	// h3diffCVEArm, h3diffAdvArm and h3diffMbrArm are each reachable through
	// exactly one lookup arm: no bulk row names them, and the dedicated rows
	// below name each one in a single field only.
	h3diffCVEArm = "H3DIFF-CVE-ONLY-1"
	h3diffAdvArm = "H3DIFF-ADV-ONLY-1"
	h3diffMbrArm = "H3DIFF-MBR-ONLY-1"
)

func h3diffBulkCVEIDs() []string {
	ids := make([]string, 0, 15)
	for i := 1; i <= 15; i++ {
		ids = append(ids, fmt.Sprintf("CVE-2026-%04d", 2*i-1))
	}
	return ids
}

func h3diffBulkAdvisoryIDs() []string {
	ids := make([]string, 0, 15)
	for i := 1; i <= 15; i++ {
		ids = append(ids, fmt.Sprintf("GHSA-0401-%04d", 6*i-3))
	}
	return ids
}

func h3diffBulkMemberIDs() []string {
	ids := make([]string, 0, 10)
	for i := 1; i <= 10; i++ {
		ids = append(ids, fmt.Sprintf("GHSA-0401-%04d", 6*i-4))
	}
	return ids
}

// h3diffCombinedIDs is a 40-id fixture set: 15 CVE-flavored, 15
// advisory-flavored and 10 member-flavored bulk ids, inside the 25-50-id
// design band and above the shipped 25-id chunk, so the proof is
// conservative per statement.
func h3diffCombinedIDs() []string {
	out := append(append(append([]string{}, h3diffBulkCVEIDs()...), h3diffBulkAdvisoryIDs()...), h3diffBulkMemberIDs()...)
	if len(out) != 40 {
		panic("h3diff combined lookup must hold the 40-id production chunk")
	}
	return out
}

func h3diffUnknownIDs() []string {
	ids := make([]string, 0, 10)
	for i := 1; i <= 10; i++ {
		ids = append(ids, fmt.Sprintf("H3DIFF-NOPE-%04d", i))
	}
	return ids
}

// h3diffSeedFixture seeds two holder scopes plus the exclusion edges the
// differential must preserve: an own-scope row (excluded by <>), a scope whose
// only matching row is tombstoned, a scope whose only matching row sits in a
// superseded generation, a duplicate live row (still one scope), and a row
// with no cve_id key (NULL handling must match on both sides). The bulk rows
// mirror the #10408 rekey distribution so the combined 40-id chunk exercises
// all three arms the way production chunks do.
func h3diffSeedFixture(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	for _, scope := range []string{h3diffOwn, h3diffHoldB, h3diffHoldC, h3diffTomb} {
		fanOutSeedScope(t, ctx, db, scope, "g1")
	}
	fanOutSeedScope(t, ctx, db, h3diffOld, "g1", "g0")
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version, collector_kind,
  source_confidence, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'h3diff:bulk:' || s.scope_id || ':' || g, s.scope_id, s.active_generation_id, $1, 'h3diff:bulk:' || g, '1.0.0', 'synthetic',
  'reported', 'synthetic', 'h3diff:bulk', now(), now(), false,
  jsonb_build_object(
    'cve_id', 'CVE-2026-' || lpad((g % 10000)::text, 4, '0'),
    'advisory_id', CASE WHEN g % 3 = 0 THEN 'GHSA-0401-' || lpad((g % 10000)::text, 4, '0')
                       ELSE 'CVE-2026-' || lpad((g % 10000)::text, 4, '0') END,
    'provenance', CASE WHEN g % 2 = 0
      THEN jsonb_build_object('advisory_group_members',
             to_jsonb(ARRAY['CVE-2026-' || lpad((g % 10000)::text, 4, '0'),
                            'GHSA-0401-' || lpad((g % 10000)::text, 4, '0')]))
      ELSE '{}'::jsonb END)
FROM (SELECT scope_id, active_generation_id FROM ingestion_scopes WHERE scope_id LIKE 'h3diff:holder-%') s
CROSS JOIN generate_series(1, $2::integer) g`, facts.ReducerSupplyChainImpactFindingFactKind, h3diffBulkN); err != nil {
		t.Fatalf("seed h3diff bulk findings: %v", err)
	}
	put := func(factID, scope, gen string, cve, adv any, members []string, tomb bool) {
		t.Helper()
		var m any
		if members != nil {
			m = members
		}
		if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version, collector_kind,
  source_confidence, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ($1, $2, $3, $4, $1, '1.0.0', 'synthetic', 'reported', 'synthetic', 'h3diff:dedicated', now(), now(), $5,
  CASE WHEN $8::text[] IS NULL
       THEN jsonb_build_object('cve_id', $6::text, 'advisory_id', $7::text, 'provenance', '{}'::jsonb)
       ELSE jsonb_build_object('cve_id', $6::text, 'advisory_id', $7::text,
              'provenance', jsonb_build_object('advisory_group_members', to_jsonb($8::text[]))) END)`,
			factID, scope, gen, facts.ReducerSupplyChainImpactFindingFactKind, tomb, cve, adv, m); err != nil {
			t.Fatalf("seed h3diff row %s: %v", factID, err)
		}
	}
	genB, genC := fanOutGen(h3diffHoldB, "g1"), fanOutGen(h3diffHoldC, "g1")
	for _, h := range []struct{ scope, gen string }{{h3diffHoldB, genB}, {h3diffHoldC, genC}} {
		put("h3diff:cve:"+h.scope, h.scope, h.gen, h3diffCVEArm, "H3DIFF-OTHER-A", []string{"H3DIFF-MBR-A"}, false)
		put("h3diff:adv:"+h.scope, h.scope, h.gen, "H3DIFF-OTHER-B", h3diffAdvArm, nil, false)
		put("h3diff:mbr:"+h.scope, h.scope, h.gen, "H3DIFF-OTHER-C", "H3DIFF-OTHER-D", []string{h3diffMbrArm}, false)
	}
	put("h3diff:dup:"+h3diffHoldB, h3diffHoldB, genB, h3diffCVEArm, "H3DIFF-OTHER-A2", nil, false)
	put("h3diff:ownrow", h3diffOwn, fanOutGen(h3diffOwn, "g1"), h3diffCVEArm, "H3DIFF-OTHER-A3", nil, false)
	put("h3diff:tombrow", h3diffTomb, fanOutGen(h3diffTomb, "g1"), h3diffBulkCVEIDs()[0], "H3DIFF-OTHER-T", nil, true)
	put("h3diff:oldrow", h3diffOld, fanOutGen(h3diffOld, "g0"), h3diffBulkCVEIDs()[0], "H3DIFF-OTHER-O", nil, false)
	put("h3diff:nullcve", h3diffHoldB, genB, nil, "H3DIFF-OTHER-N", nil, false)
	if _, err := db.ExecContext(ctx, `ANALYZE fact_records`); err != nil {
		t.Fatalf("analyze h3diff fixture: %v", err)
	}
}

// h3diffQuerier covers the query entry points the proof runs through: a
// pinned *sql.Conn for plan-mode control and the pooled *sql.DB for the
// production pgx path.
type h3diffQuerier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func h3diffScopeIDs(t *testing.T, ctx context.Context, q h3diffQuerier, query string, ids []string, own string) []string {
	t.Helper()
	return h3diffScopeIDsArgs(t, ctx, q, query, ids, own)
}

// h3diffScopeIDsArgs runs a scope-id query with explicit args; EXECUTE of a
// prepared statement cannot take $1-style parameters through the pgx stdlib
// driver, so those callers interpolate literals (below) and pass no args.
func h3diffScopeIDsArgs(t *testing.T, ctx context.Context, q h3diffQuerier, query string, args ...any) []string {
	t.Helper()
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		t.Fatalf("holder query: %v\nquery:\n%s", err, query)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var scope string
		if err := rows.Scan(&scope); err != nil {
			t.Fatalf("scan holder scope: %v", err)
		}
		out = append(out, scope)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate holder scopes: %v", err)
	}
	return out
}

// h3diffQuoteLiteral quotes one string literal; h3diffArrayLiteral renders the
// lookup-id array the same way the repo's plan probes do, because EXECUTE
// parameters must be literals, not $n placeholders.
func h3diffQuoteLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func h3diffArrayLiteral(ids []string) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, h3diffQuoteLiteral(id))
	}
	return "ARRAY[" + strings.Join(parts, ",") + "]::text[]"
}

func h3diffExceptCount(t *testing.T, ctx context.Context, q h3diffQuerier, outer string, ids []string, own string) int64 {
	t.Helper()
	rows, err := q.QueryContext(ctx, outer, ids, own)
	if err != nil {
		t.Fatalf("except count: %v\nquery:\n%s", err, outer)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		t.Fatalf("except count returned no row:\n%s", outer)
	}
	var n int64
	if err := rows.Scan(&n); err != nil {
		t.Fatalf("scan except count: %v", err)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate except count: %v", err)
	}
	return n
}

var (
	h3diffExecRe = regexp.MustCompile(`Execution Time:\s*([\d.]+)\s*ms`)
	h3diffHitRe  = regexp.MustCompile(`hit=(\d+)`)
	h3diffReadRe = regexp.MustCompile(`read=(\d+)`)
)

// h3diffExplain runs EXPLAIN (ANALYZE, BUFFERS) over a literal-parameter
// prepared EXECUTE and returns its execution time plus the buffer touches
// summed across plan nodes (per-node shared blocks are exclusive, so the sum
// is the statement total).
func h3diffExplain(t *testing.T, ctx context.Context, q h3diffQuerier, executeSQL string) (execMs float64, hit, read int64) {
	t.Helper()
	rows, err := q.QueryContext(ctx, `EXPLAIN (ANALYZE, BUFFERS, COSTS OFF, SUMMARY ON) `+executeSQL)
	stmt := executeSQL
	if err != nil {
		t.Fatalf("explain %s: %v", stmt, err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan explain %s: %v", stmt, err)
		}
		if m := h3diffExecRe.FindStringSubmatch(line); m != nil {
			v, err := strconv.ParseFloat(m[1], 64)
			if err != nil {
				t.Fatalf("parse execution time %q: %v", m[1], err)
			}
			execMs = v
		}
		for _, m := range h3diffHitRe.FindAllStringSubmatch(line, -1) {
			v, err := strconv.ParseInt(m[1], 10, 64)
			if err != nil {
				t.Fatalf("parse hit buffers %q: %v", m[1], err)
			}
			hit += v
		}
		for _, m := range h3diffReadRe.FindAllStringSubmatch(line, -1) {
			v, err := strconv.ParseInt(m[1], 10, 64)
			if err != nil {
				t.Fatalf("parse read buffers %q: %v", m[1], err)
			}
			read += v
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate explain %s: %v", stmt, err)
	}
	return execMs, hit, read
}

func h3diffPercentile(durs []time.Duration, p float64) time.Duration {
	cp := append([]time.Duration(nil), durs...)
	slices.Sort(cp)
	i := int(math.Ceil(p*float64(len(cp)))) - 1
	if i < 0 {
		i = 0
	}
	if i >= len(cp) {
		i = len(cp) - 1
	}
	return cp[i]
}

// TestHolderH3DiffSetsLive is the #10564 H set-identity proof: the shipped H3
// MATERIALIZED pair-union shape returns exactly the scopes the pre-H3 OR shape
// returns, per arm and combined, under force_custom_plan and
// force_generic_plan. Each arm is isolated through a dedicated id reachable
// through that arm only, so a dropped arm fails its set; the combined 40-id
// chunk additionally pins the tombstone, superseded-generation, own-scope and
// duplicate exclusions. Equivalence is proved both as ordered Go sets and as
// bidirectional SQL EXCEPT counts (0/0), with EXPLAIN (ANALYZE, BUFFERS)
// evidence per side, set and mode.
func TestHolderH3DiffSetsLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	db.SetMaxOpenConns(1)
	h3diffSeedFixture(t, ctx, db)
	holders := []string{h3diffHoldB, h3diffHoldC}
	sets := []struct {
		name string
		ids  []string
		want []string
	}{
		{"cve", []string{h3diffCVEArm}, holders},
		{"advisory", []string{h3diffAdvArm}, holders},
		{"members", []string{h3diffMbrArm}, holders},
		{"combined", h3diffCombinedIDs(), holders},
		{"unknown", h3diffUnknownIDs(), nil},
	}
	h3 := postgres.FindSupplyChainImpactHoldersQuery
	h0MinusH3 := `SELECT count(*) FROM ((` + holderH0BaselineQuery + `) EXCEPT (` + h3 + `)) d`
	h3MinusH0 := `SELECT count(*) FROM ((` + h3 + `) EXCEPT (` + holderH0BaselineQuery + `)) d`
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("pin session: %v", err)
	}
	defer func() { _ = conn.Close() }()
	for mi, mode := range []string{"force_custom_plan", "force_generic_plan"} {
		if _, err := conn.ExecContext(ctx, "SET plan_cache_mode = "+mode); err != nil {
			t.Fatalf("set %s: %v", mode, err)
		}
		for si, s := range sets {
			h0stmt := fmt.Sprintf("h3diff_h0_%d_%d", si, mi)
			h3stmt := fmt.Sprintf("h3diff_h3_%d_%d", si, mi)
			if _, err := conn.ExecContext(ctx, "PREPARE "+h0stmt+"(text[], text) AS "+holderH0BaselineQuery); err != nil {
				t.Fatalf("prepare %s: %v", h0stmt, err)
			}
			if _, err := conn.ExecContext(ctx, "PREPARE "+h3stmt+"(text[], text) AS "+h3); err != nil {
				t.Fatalf("prepare %s: %v", h3stmt, err)
			}
			params := "(" + h3diffArrayLiteral(s.ids) + ", " + h3diffQuoteLiteral(h3diffOwn) + ")"
			h0exec, h3exec := "EXECUTE "+h0stmt+params, "EXECUTE "+h3stmt+params
			h0got := h3diffScopeIDsArgs(t, ctx, conn, h0exec)
			h3got := h3diffScopeIDsArgs(t, ctx, conn, h3exec)
			if len(h0got) == 0 && s.want != nil {
				t.Fatalf("%s %s: baseline returned zero holders, the fixture is not exercising the read", s.name, mode)
			}
			if !slices.Equal(h0got, h3got) {
				t.Fatalf("%s %s: H3 set %v diverges from H0 set %v", s.name, mode, h3got, h0got)
			}
			if !slices.Equal(h0got, s.want) {
				t.Fatalf("%s %s: holders = %v, want %v", s.name, mode, h0got, s.want)
			}
			if n := h3diffExceptCount(t, ctx, conn, h0MinusH3, s.ids, h3diffOwn); n != 0 {
				t.Fatalf("%s %s: H0 EXCEPT H3 = %d, want 0", s.name, mode, n)
			}
			if n := h3diffExceptCount(t, ctx, conn, h3MinusH0, s.ids, h3diffOwn); n != 0 {
				t.Fatalf("%s %s: H3 EXCEPT H0 = %d, want 0", s.name, mode, n)
			}
			h0ms, h0hit, h0read := h3diffExplain(t, ctx, conn, h0exec)
			h3ms, h3hit, h3read := h3diffExplain(t, ctx, conn, h3exec)
			t.Logf("h3diff %s %s: H0 %.2fms hit=%d read=%d | H3 %.2fms hit=%d read=%d | scopes=%d exc=0/0",
				s.name, mode, h0ms, h0hit, h0read, h3ms, h3hit, h3read, len(h3got))
		}
	}
	if _, err := conn.ExecContext(ctx, "RESET plan_cache_mode"); err != nil {
		t.Fatalf("reset plan_cache_mode: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("release session: %v", err)
	}
	// The same sets through the production pgx path: FindHolderScopes must
	// agree with the baseline through the driver, not only through raw SQL.
	store := fanOutLiveStore(db)
	for _, s := range sets {
		got, err := store.FindHolderScopes(ctx, h3diffOwn, s.ids)
		if err != nil {
			t.Fatalf("FindHolderScopes(%s): %v", s.name, err)
		}
		if want := h3diffScopeIDs(t, ctx, db, holderH0BaselineQuery, s.ids, h3diffOwn); !slices.Equal(got, want) {
			t.Fatalf("FindHolderScopes(%s) = %v, baseline = %v", s.name, got, want)
		}
		if !slices.Equal(got, s.want) {
			t.Fatalf("FindHolderScopes(%s) = %v, want %v", s.name, got, s.want)
		}
	}
	if got, err := store.FindHolderScopes(ctx, h3diffOwn, nil); err != nil || got != nil {
		t.Fatalf("FindHolderScopes(nil) = %v, %v; want nil, nil", got, err)
	}
	// H-2 driver record: what plan_cache_mode production runs under, and how
	// pgx's per-connection statement cache divided repeated executions.
	for i := 0; i < 4; i++ {
		if _, err := store.FindHolderScopes(ctx, h3diffOwn, h3diffCombinedIDs()); err != nil {
			t.Fatalf("repeat FindHolderScopes: %v", err)
		}
	}
	var srv, pcm string
	if err := db.QueryRowContext(ctx, "SELECT version(), current_setting('plan_cache_mode')").Scan(&srv, &pcm); err != nil {
		t.Fatalf("read server settings: %v", err)
	}
	var entries, custom, generic int64
	if err := db.QueryRowContext(ctx, `
SELECT count(*), COALESCE(sum(custom_plans), 0), COALESCE(sum(generic_plans), 0)
FROM pg_prepared_statements WHERE statement LIKE '%FROM candidates%'`).Scan(&entries, &custom, &generic); err != nil {
		t.Fatalf("read prepared statements: %v", err)
	}
	t.Logf("h3diff driver: %s plan_cache_mode=%s prepared entries=%d custom=%d generic=%d", srv, pcm, entries, custom, generic)
}

// TestHolderH3StatementP95Live measures checklist H-1/H-2 at a 40-id
// fixture set (above the shipped 25-id chunk) on the disposable fixture:
// 24 alternating cold
// (DISCARD PLANS) and warm runs per statement, interleaved across H3 and H0
// so host drift cannot favor one side. Buffer evidence lives in
// TestHolderH3DiffSetsLive; this test owns the per-statement p50/p95. The 5 s
// bound is the issue's per-statement acceptance with wide headroom on this
// fixture size; production-scale headroom still rides the QA replay lane.
func TestHolderH3StatementP95Live(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	db.SetMaxOpenConns(1)
	h3diffSeedFixture(t, ctx, db)
	ids := h3diffCombinedIDs()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("pin session: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, "RESET plan_cache_mode"); err != nil {
		t.Fatalf("reset plan_cache_mode: %v", err)
	}
	timeRun := func(query string) (time.Duration, int) {
		t.Helper()
		start := time.Now()
		rows, err := conn.QueryContext(ctx, query, ids, h3diffOwn)
		if err != nil {
			t.Fatalf("holder read: %v", err)
		}
		n := 0
		for rows.Next() {
			var scope string
			if err := rows.Scan(&scope); err != nil {
				_ = rows.Close()
				t.Fatalf("scan holder scope: %v", err)
			}
			n++
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			t.Fatalf("iterate holder scopes: %v", err)
		}
		_ = rows.Close()
		return time.Since(start), n
	}
	const rounds = 24
	var h3, h0 []time.Duration
	for i := 0; i < rounds; i++ {
		if i%2 == 0 {
			if _, err := conn.ExecContext(ctx, "DISCARD PLANS"); err != nil {
				t.Fatalf("discard plans: %v", err)
			}
		}
		d3, n3 := timeRun(postgres.FindSupplyChainImpactHoldersQuery)
		d0, n0 := timeRun(holderH0BaselineQuery)
		if i == 0 && (n3 != 2 || n0 != 2) {
			t.Fatalf("first round scopes H3=%d H0=%d, want 2/2: the fixture is not exercising the read", n3, n0)
		}
		h3, h0 = append(h3, d3), append(h0, d0)
	}
	for _, tc := range []struct {
		name string
		durs []time.Duration
	}{{"H3", h3}, {"H0", h0}} {
		t.Logf("h3diff p95 %s n=%d p50=%s p95=%s max=%s", tc.name, len(tc.durs),
			h3diffPercentile(tc.durs, 0.5), h3diffPercentile(tc.durs, 0.95), slices.Max(tc.durs))
		if got := h3diffPercentile(tc.durs, 0.95); got >= 5*time.Second {
			t.Errorf("h3diff p95 %s = %s over %d alternating cold/warm runs, want < 5s", tc.name, got, len(tc.durs))
		}
	}
}
