// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// Production-writer differential and scale proof for the ruled #9805 chunked
// full, re-sourced round 5 (F3/F4). The writer is the only production path
// that creates findings, and since the #10880 write stop it writes supports
// only: no finding fact rows exist for what it wrote. These proofs seed
// through that writer, assert the supports-only precondition in the database,
// and compare the chunked full against the supports-swapped winners select —
// the oracle that can fail. The legacy fact_records twin reads nothing on
// this data, so a differential against it is vacuous; the tests assert that
// vacuity explicitly instead of leaning on it.

// fullSupportsExceptAll materializes the supports-swapped winners select and
// counts EXCEPT ALL in both directions against the stored winners, plus both
// row counts. Both differences must be zero for the paths to agree.
func fullSupportsExceptAll(t *testing.T, ctx context.Context, db *sql.DB, label string) (forward, reverse, stored, oracle int64) {
	t.Helper()
	// One materialization per call: a phase that asserts twice in a row
	// must not see the previous call's table (same session, same name).
	if _, err := db.ExecContext(ctx, `DROP TABLE IF EXISTS supports_now`); err != nil {
		t.Fatalf("%s drop stale supports winners: %v", label, err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TEMP TABLE supports_now AS `+postgres.WinnerSelectFromSupportsSQLForTest); err != nil {
		t.Fatalf("%s materialize supports winners: %v", label, err)
	}
	defer func() {
		if _, err := db.ExecContext(ctx, `DROP TABLE supports_now`); err != nil {
			t.Fatalf("%s drop supports_now: %v", label, err)
		}
	}()
	count := func(query string) int64 {
		t.Helper()
		var n int64
		if err := db.QueryRowContext(ctx, query).Scan(&n); err != nil {
			t.Fatalf("%s count: %v", label, err)
		}
		return n
	}
	cols := fullDiffTextList("supports_now")
	storedCols := fullDiffTextList("supply_chain_impact_canonical_winners")
	forward = count(`SELECT count(*) FROM ((SELECT ` + cols + ` FROM supports_now) EXCEPT ALL (SELECT ` + storedCols + ` FROM supply_chain_impact_canonical_winners)) AS d`)
	reverse = count(`SELECT count(*) FROM ((SELECT ` + storedCols + ` FROM supply_chain_impact_canonical_winners) EXCEPT ALL (SELECT ` + cols + ` FROM supports_now)) AS d`)
	stored = count(`SELECT count(*) FROM supply_chain_impact_canonical_winners`)
	oracle = count(`SELECT count(*) FROM supports_now`)
	t.Logf("%s differential: supports-minus-stored=%d stored-minus-supports=%d stored=%d supports=%d",
		label, forward, reverse, stored, oracle)
	return forward, reverse, stored, oracle
}

// fullSupportsFindingFactCount counts finding fact rows in the given scopes:
// the supports-only precondition the production writer leaves behind.
func fullSupportsFindingFactCount(t *testing.T, ctx context.Context, db *sql.DB, scopes ...string) int64 {
	t.Helper()
	placeholders := make([]string, len(scopes))
	args := make([]any, len(scopes)+1)
	args[0] = facts.ReducerSupplyChainImpactFindingFactKind
	for i, scope := range scopes {
		placeholders[i] = fmt.Sprintf("$%d", i+2)
		args[i+1] = scope
	}
	var n int64
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM fact_records WHERE fact_kind = $1 AND scope_id IN (`+
		strings.Join(placeholders, ", ")+`)`, args...).Scan(&n); err != nil {
		t.Fatalf("count finding facts: %v", err)
	}
	return n
}

// TestSupplyChainImpactWinnersFullChunkedMatchesSupportsWriterLive re-seeds
// one differential phase end to end through the production writer: the
// findings exist only in the supports table, the precondition the fix rests
// on. The full resweep must agree with the supports oracle row for row while
// the legacy fact oracle reads nothing.
func TestSupplyChainImpactWinnersFullChunkedMatchesSupportsWriterLive(t *testing.T) {
	h := newKeyedHarness(t, "#9805 chunked-full production-writer differential proof")
	scopeA, scopeB := "vuln-intel://9805-full-supports/a", "vuln-intel://9805-full-supports/b"
	h.seedScope(scopeA, scopeA+"/g1")
	h.seedScope(scopeB, scopeB+"/g1")
	// One canonical key in both scopes (the higher priority wins with
	// source_count 2), plus one key per scope.
	h.write(scopeA, scopeA+"/g1",
		keyedFinding("CVE-2026-9901", "pkg:npm/shared", 10),
		keyedFinding("CVE-2026-9902", "pkg:npm/only-a", 20))
	h.write(scopeB, scopeB+"/g1",
		keyedFinding("CVE-2026-9901", "pkg:npm/shared", 42),
		keyedFinding("CVE-2026-9903", "pkg:npm/only-b", 7))

	// The writer wrote supports only: no finding fact rows exist for these
	// scopes, and the supports hold all four findings.
	if n := fullSupportsFindingFactCount(t, h.ctx, h.db, scopeA, scopeB); n != 0 {
		t.Fatalf("finding fact rows in writer-seeded scopes = %d, want 0 (supports-only)", n)
	}
	var supports int
	if err := h.db.QueryRowContext(h.ctx, `SELECT count(*) FROM supply_chain_impact_finding_supports WHERE scope_id IN ($1, $2)`,
		scopeA, scopeB).Scan(&supports); err != nil || supports != 4 {
		t.Fatalf("supports in writer-seeded scopes = %d, %v; want 4", supports, err)
	}

	// Two identifiers per chunk: the four findings walk as bounded chunks.
	h.store.ChunkMaxIdentifiers = 2
	firstStart := time.Now()
	first, err := h.store.ResweepWinners(h.ctx)
	firstElapsed := time.Since(firstStart)
	if err != nil || first.Skipped {
		t.Fatalf("writer-seeded full resweep = %+v, %v", first, err)
	}
	t.Logf("writer-seeded full resweep: inserted=%d updated=%d deleted=%d unchanged=%d in %v",
		first.Inserted, first.Updated, first.Deleted, first.Unchanged, firstElapsed)

	fwd, rev, stored, oracle := fullSupportsExceptAll(t, h.ctx, h.db, "writer-seeded")
	if fwd != 0 || rev != 0 {
		t.Fatalf("writer-seeded chunked-full disagrees with the supports oracle: forward=%d reverse=%d", fwd, rev)
	}
	if stored != 3 || oracle != 3 {
		t.Fatalf("writer-seeded row counts: stored=%d supports=%d, want 3 (shared key deduped)", stored, oracle)
	}
	// The shared key's winner is the higher priority with both copies counted.
	var priority, sources int
	if err := h.db.QueryRowContext(h.ctx, `SELECT priority_score, source_count FROM supply_chain_impact_canonical_winners
WHERE package_id = 'pkg:npm/shared'`).Scan(&priority, &sources); err != nil {
		t.Fatalf("read shared winner: %v", err)
	}
	if priority != 42 || sources != 2 {
		t.Fatalf("shared winner = priority %d sources %d, want 42 and 2", priority, sources)
	}
	// The legacy fact oracle is vacuous on supports-only data: it reads
	// nothing, so agreement with it would prove nothing.
	var legacy int
	if err := h.db.QueryRowContext(h.ctx, `SELECT count(*) FROM (`+postgres.WinnerSelectSQLForTest+`) AS legacy`).Scan(&legacy); err != nil {
		t.Fatalf("legacy fact oracle count: %v", err)
	}
	if legacy != 0 {
		t.Fatalf("legacy fact oracle holds %d winners on writer-seeded data, want 0 (it cannot see supports)", legacy)
	}

	// A second resweep over the settled table writes nothing: the full is
	// stable on supports-only data.
	second, err := h.store.ResweepWinners(h.ctx)
	if err != nil || second.Skipped {
		t.Fatalf("settled full resweep = %+v, %v", second, err)
	}
	if second.Inserted != 0 || second.Updated != 0 || second.Deleted != 0 {
		t.Fatalf("settled full resweep wrote inserted=%d updated=%d deleted=%d, want a no-op",
			second.Inserted, second.Updated, second.Deleted)
	}
}

// fullScaleExplain runs EXPLAIN (ANALYZE, BUFFERS) over a bare SELECT and
// returns the plan lines plus the parsed execution time and buffer hits.
func fullScaleExplain(t *testing.T, ctx context.Context, db *sql.DB, name, selectSQL string) (plan []string, execution time.Duration, buffers string) {
	t.Helper()
	rows, err := db.QueryContext(ctx, "EXPLAIN (ANALYZE, BUFFERS) "+selectSQL)
	if err != nil {
		t.Fatalf("explain %s: %v", name, err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan %s plan: %v", name, err)
		}
		plan = append(plan, line)
		if v, ok := strings.CutPrefix(line, "Execution Time: "); ok {
			var ms float64
			if _, err := fmt.Sscanf(v, "%f ms", &ms); err == nil {
				execution = time.Duration(ms * float64(time.Millisecond))
			}
		}
		if strings.Contains(line, "Buffers:") {
			buffers = strings.TrimSpace(line)
		}
	}
	if err := rows.Close(); err != nil {
		t.Fatalf("close %s plan: %v", name, err)
	}
	t.Logf("%s plan:\n%s", name, strings.Join(plan, "\n"))
	return plan, execution, buffers
}

// fullScaleEnumSelect returns the bare SELECT the full enumeration pages: the
// CREATE wrapper cut at its stable boundary, like the stored-arm proof does.
func fullScaleEnumSelect(t *testing.T) string {
	t.Helper()
	const anchor = "ON COMMIT DROP AS\n"
	idx := strings.Index(postgres.WinnersFullIdentsSQLForTest, anchor)
	if idx < 0 {
		t.Fatal("full enumeration has no wrapped SELECT: the CREATE shape is stale")
	}
	sel := strings.TrimSpace(postgres.WinnersFullIdentsSQLForTest[idx+len(anchor):])
	if !strings.HasPrefix(sel, "SELECT package_id") {
		t.Fatalf("full enumeration SELECT is stale: %.60q", sel)
	}
	return sel
}

// TestSupplyChainImpactWinnersFullChunkedScaleLive measures the chunked full
// at a production-like corpus shape: 200 identifiers with one hot identifier
// carrying hundreds of copies (the QA shape quoted in the keyed probe
// comments), seeded fact-synced so the legacy single-statement select reads
// the same rows and the before/after comparison is on one corpus. It reports
// the legacy baseline, the enumeration plan (time, buffers, sort spill), the
// chunked walk total, and the per-chunk bracket, and requires the stored
// table to equal both oracles row for row.
func TestSupplyChainImpactWinnersFullChunkedScaleLive(t *testing.T) {
	_ = supplyChainImpactLiveDSN(t, "#9805 chunked-full scale proof")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	t.Cleanup(cancel)
	db := openAdvisorySuppressionIsolatedDB(t)
	store := postgres.NewSupplyChainImpactWinnersStore(postgres.SQLDB{DB: db})
	// 25 identifiers per chunk: ~191 identifiers walk as eight bounded chunks.
	store.ChunkMaxIdentifiers = 25

	const scopes, perScope = 20, 100
	hotCopies := 0
	loadStart := time.Now()
	type scaleSeed struct {
		id, scope, gen string
		payload        map[string]any
	}
	var seeds []scaleSeed
	for s := 0; s < scopes; s++ {
		scope := fmt.Sprintf("vuln-intel://9805-full-scale/s%02d", s)
		winnersLiveSeedScope(t, ctx, db, scope)
		for i := 0; i < perScope; i++ {
			g := s*perScope + i
			id := fmt.Sprintf("f-scale-%04d", g)
			// Every seventh finding shares the hot identifier (one hot
			// key with hundreds of copies); the rest spread over 190
			// keys, so the enumeration holds 191 identifiers.
			if g%7 == 0 {
				hotCopies++
				seeds = append(seeds, scaleSeed{
					id, scope, scope + "/active",
					winnersLiveFinding("CVE-2026-9950", "pkg:npm/hot", 50, nil),
				})
				continue
			}
			k := g % 190
			seeds = append(seeds, scaleSeed{
				id, scope, scope + "/active",
				winnersLiveFinding(fmt.Sprintf("CVE-2026-%04d", 9600+k), fmt.Sprintf("pkg:npm/p%02d", k%19), g%100, nil),
			})
		}
	}
	// One bulk plant plus one supports sync, not one sync per fact: the
	// sync rewrites every pair, so per-fact syncs are quadratic. The end
	// state is what per-fact seeding leaves: the writer's own upserts are
	// idempotent and nothing here tombstones.
	values := make([]string, 0, len(seeds))
	args := make([]any, 0, 5*len(seeds))
	for i, seed := range seeds {
		body, err := json.Marshal(seed.payload)
		if err != nil {
			t.Fatalf("marshal %s: %v", seed.id, err)
		}
		base := 5*i + 1
		values = append(values, fmt.Sprintf("($%d, $%d, $%d, $%d, $%d, '1.0.0', 'synthetic', $%d, now(), now(), false, $%d::jsonb)",
			base, base+1, base+2, base+3, base, base, base+4))
		args = append(args, seed.id, seed.scope, seed.gen, facts.ReducerSupplyChainImpactFindingFactKind, string(body))
	}
	winnersLivePlantFact(t, ctx, db, `INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload) VALUES `+strings.Join(values, ", "), args...)
	winnersLiveSyncSupports(t, ctx, db)
	loadElapsed := time.Since(loadStart)
	var findings, supportRows int64
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM fact_records WHERE fact_kind = $1`,
		facts.ReducerSupplyChainImpactFindingFactKind).Scan(&findings); err != nil {
		t.Fatalf("count seeded findings: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM supply_chain_impact_finding_supports`).Scan(&supportRows); err != nil {
		t.Fatalf("count seeded supports: %v", err)
	}
	t.Logf("scale corpus: findings=%d supports=%d hot_copies=%d (load in %v)", findings, supportRows, hotCopies, loadElapsed)

	var idents int64
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM (`+fullScaleEnumSelect(t)+`) AS e`).Scan(&idents); err != nil {
		t.Fatalf("count enumerated identifiers: %v", err)
	}
	chunks := (idents + 24) / 25
	t.Logf("scale enumeration: identifiers=%d chunk_size=25 chunks=%d", idents, chunks)

	// Before: the legacy single-statement select materialized on the same
	// corpus, timed, with its plan. This is the rebuild core the chunked
	// walk replaces.
	legacyStart := time.Now()
	if _, err := db.ExecContext(ctx, `CREATE TEMP TABLE legacy_scale AS `+postgres.WinnerSelectSQLForTest); err != nil {
		t.Fatalf("materialize legacy winners: %v", err)
	}
	legacyElapsed := time.Since(legacyStart)
	_, legacyExec, legacyBuffers := fullScaleExplain(t, ctx, db, "legacy single-statement select", postgres.WinnerSelectSQLForTest)

	// The enumeration plan: time, buffers, and the sort method (a spill
	// here is the unbounded-sort risk the chunk sizing answers).
	enumPlan, enumExec, enumBuffers := fullScaleExplain(t, ctx, db, "chunked-full enumeration", fullScaleEnumSelect(t))
	spill := "in-memory"
	for _, line := range enumPlan {
		if strings.Contains(line, "Sort Method:") {
			spill = strings.TrimSpace(line)
		}
	}
	// After: the chunked walk on the same state.
	walkStart := time.Now()
	walk, err := store.ResweepWinners(ctx)
	walkElapsed := time.Since(walkStart)
	if err != nil || walk.Skipped {
		t.Fatalf("scale full resweep = %+v, %v", walk, err)
	}
	t.Logf("scale walk: inserted=%d updated=%d deleted=%d unchanged=%d in %v",
		walk.Inserted, walk.Updated, walk.Deleted, walk.Unchanged, walkElapsed)

	fwd, rev := fullDiffExceptAll(t, ctx, db)
	t.Logf("scale legacy differential: legacy-minus-stored=%d stored-minus-legacy=%d", fwd, rev)
	if fwd != 0 || rev != 0 {
		t.Fatalf("scale chunked-full disagrees with the legacy select: forward=%d reverse=%d", fwd, rev)
	}
	sfwd, srev, stored, oracle := fullSupportsExceptAll(t, ctx, db, "scale")
	if sfwd != 0 || srev != 0 || stored != oracle {
		t.Fatalf("scale chunked-full disagrees with the supports oracle: forward=%d reverse=%d", sfwd, srev)
	}

	// The giant-chunk bracket: one chunk over every identifier costs at
	// least as much as the heaviest bounded chunk, so the mean and this
	// bound the worst chunk from both sides.
	store.ChunkMaxIdentifiers = 0
	giantStart := time.Now()
	giant, err := store.ResweepWinners(ctx)
	giantElapsed := time.Since(giantStart)
	if err != nil || giant.Skipped {
		t.Fatalf("giant-chunk full resweep = %+v, %v", giant, err)
	}
	if giant.Inserted != 0 || giant.Updated != 0 || giant.Deleted != 0 {
		t.Fatalf("giant-chunk resweep wrote inserted=%d updated=%d deleted=%d, want a no-op on settled state",
			giant.Inserted, giant.Updated, giant.Deleted)
	}

	mean := walkElapsed / time.Duration(max(chunks, 1))
	t.Logf("scale numbers: legacy_materialize=%v legacy_exec=%v legacy_buffers=[%s] enum_exec=%v enum_buffers=[%s] enum_sort=[%s]",
		legacyElapsed, legacyExec, legacyBuffers, enumExec, enumBuffers, spill)
	t.Logf("scale numbers: walk_total=%v chunks=%d mean_chunk=%v giant_single_chunk=%v winners=%d",
		walkElapsed, chunks, mean, giantElapsed, stored)
	if walkElapsed > 60*time.Second || giantElapsed > 60*time.Second {
		t.Errorf("scale walk took %v / %v: chunks must stay bounded under 60 s", walkElapsed, giantElapsed)
	}
}
