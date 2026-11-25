// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// narrowDigestOfWinners hashes the stored winner set the way the M6
// differential requires: over canonical key and content only, so row-version
// identity (xmin) and stamp times cannot false-fail a shape whose truth is
// identical.
func narrowDigestOfWinners(t *testing.T, rows map[string]winnersLiveRow) string {
	t.Helper()
	keys := make([]string, 0, len(rows))
	for key := range rows {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, key := range keys {
		fmt.Fprintf(h, "%s\n%s\n", key, rows[key].content)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// narrowWALBytes returns the WAL byte delta between two pg_current_wal_lsn
// readings.
func narrowWALBytes(t *testing.T, h *keyedHarness, before, after string) int64 {
	t.Helper()
	var n int64
	if err := h.db.QueryRowContext(h.ctx, `SELECT pg_wal_lsn_diff($1, $2)`, after, before).Scan(&n); err != nil {
		t.Fatalf("wal diff: %v", err)
	}
	return n
}

func narrowCurrentLSN(t *testing.T, h *keyedHarness) string {
	t.Helper()
	var lsn string
	if err := h.db.QueryRowContext(h.ctx, `SELECT pg_current_wal_lsn()`).Scan(&lsn); err != nil {
		t.Fatalf("current lsn: %v", err)
	}
	return lsn
}

// narrowExplain runs EXPLAIN (ANALYZE, BUFFERS) on query and returns the plan
// text plus the shared-buffer hits and reads.
func narrowExplain(t *testing.T, h *keyedHarness, query string) (plan string, hits, reads int64) {
	t.Helper()
	rows, err := h.db.QueryContext(h.ctx, `EXPLAIN (ANALYZE, BUFFERS) `+query)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			_ = rows.Close()
			t.Fatalf("scan plan: %v", err)
		}
		lines = append(lines, line)
		trimmed := strings.TrimSpace(line)
		if i := strings.Index(trimmed, "shared hit="); i >= 0 {
			var hb int64
			if _, err := fmt.Sscanf(trimmed[i:], "shared hit=%d", &hb); err == nil {
				hits += hb
			}
		}
		if i := strings.Index(trimmed, "read="); i >= 0 {
			var rb int64
			if _, err := fmt.Sscanf(trimmed[i:], "read=%d", &rb); err == nil {
				reads += rb
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read plan: %v", err)
	}
	return strings.Join(lines, "\n"), hits, reads
}

// narrowSeedCorpus writes the corpus through the production impact writer, so
// every fact row is QA-shaped: the production encoder, the production
// columns, and the full production index set the disposable bootstrap
// carries (including the 291 keyed probe index).
func narrowSeedCorpus(t *testing.T, h *keyedHarness, corpus narrowProofCorpus, scopePrefix string) int {
	t.Helper()
	total := 0
	for s := 0; s < corpus.scopes; s++ {
		scope := fmt.Sprintf("%s/s%04d", scopePrefix, s)
		h.seedScope(scope, scope+"/g1")
		findings := make([]supplychaincore.SupplyChainImpactFinding, 0, corpus.findingsPerScope)
		for i := 0; i < corpus.findingsPerScope; i++ {
			g := s*corpus.findingsPerScope + i
			k := g % ((corpus.scopes * corpus.findingsPerScope * 5) / 6)
			finding := keyedFinding(
				fmt.Sprintf("CVE-2026-%07d", k),
				fmt.Sprintf("pkg:npm/narrow-p%d", k/10),
				(g*7)%100,
			)
			finding.RepositoryID = fmt.Sprintf("repo-narrow-%d", k/100)
			findings = append(findings, finding)
		}
		h.write(scope, scope+"/g1", findings...)
		total += len(findings)
	}
	return total
}

// narrowWriteScope writes one scope of findings and returns the fact count.
func narrowWriteScope(t *testing.T, h *keyedHarness, scope string, n int, pkgBase string) int {
	t.Helper()
	h.seedScope(scope, scope+"/g1")
	findings := make([]supplychaincore.SupplyChainImpactFinding, 0, n)
	for i := 0; i < n; i++ {
		finding := keyedFinding(fmt.Sprintf("CVE-2026-%07d", 9000000+n*1000+i), fmt.Sprintf("pkg:npm/%s-%d", pkgBase, i), i)
		finding.RepositoryID = fmt.Sprintf("repo-narrow-%s", pkgBase)
		findings = append(findings, finding)
	}
	if _, err := h.writer.WriteSupplyChainImpactFindings(h.ctx, supplychaincore.SupplyChainImpactWrite{
		IntentID: "intent-11142-narrow", ScopeID: scope, GenerationID: scope + "/g1",
		SourceSystem: "vulnerability_intelligence", Cause: "11142 narrow write bar",
		Findings: findings, FencingToken: keyedFenceTokens.Add(1),
	}); err != nil {
		t.Fatalf("write %s: %v", scope, err)
	}
	return len(findings)
}

// narrowVehicleDigest runs the shipped winner body over vehicle and digests
// the result. The vehicle is the shape's candidate set in fact shape; the
// body is byte-identical to the shipped select's tail.
func narrowVehicleDigest(t *testing.T, h *keyedHarness, vehicle string) string {
	t.Helper()
	rows, err := h.db.QueryContext(h.ctx,
		`SELECT canonical_key, (to_jsonb(w) - 'materialized_at')::text FROM (`+
			postgres.NarrowWinnersSelectSQL(vehicle)+`) AS w`)
	if err != nil {
		t.Fatalf("vehicle %s winner select: %v", vehicle, err)
	}
	defer func() { _ = rows.Close() }()
	got := map[string]winnersLiveRow{}
	for rows.Next() {
		var key, content string
		if err := rows.Scan(&key, &content); err != nil {
			_ = rows.Close()
			t.Fatalf("vehicle %s scan winner: %v", vehicle, err)
		}
		got[key] = winnersLiveRow{content: content}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("vehicle %s read winners: %v", vehicle, err)
	}
	return narrowDigestOfWinners(t, got)
}

// narrowStatioBlocks reads this database's block touches (buffer hits plus
// reads) from the statistics views. pg_stat_database carries the live
// per-database counters; the legacy pg_statio_* table views report zeros
// for relation I/O on this stack (PostgreSQL 18), so they cannot back the
// bar. Relation stats reach the shared counters on flush, so the read is
// preceded by pg_stat_force_next_flush. The sum is cache-state
// independent: a warm cache moves touches from reads to hits without
// changing the total, so the bar below compares work, not cache luck.
func narrowStatioBlocks(t *testing.T, h *keyedHarness) int64 {
	t.Helper()
	if _, err := h.db.ExecContext(h.ctx, `SELECT pg_stat_force_next_flush()`); err != nil {
		t.Fatalf("statio flush: %v", err)
	}
	var n int64
	if err := h.db.QueryRowContext(h.ctx, `
SELECT blks_read + blks_hit FROM pg_stat_database WHERE datname = current_database()`).Scan(&n); err != nil {
		t.Fatalf("statio blocks: %v", err)
	}
	return n
}

// narrowStatioProbe executes queries in one transaction and returns the
// block touches they caused, measured as the statistics-view delta around
// the execution. The statements run for their real side effects (the
// shipped arm builds its narrow probe then its temp active-facts table,
// mirroring runKeyedOrFull); temp-table writes are not block touches, and
// the single transaction keeps the ON COMMIT DROP tables alive across the
// statements the way the tick does.
func narrowStatioProbe(t *testing.T, h *keyedHarness, queries ...string) int64 {
	t.Helper()
	before := narrowStatioBlocks(t, h)
	tx, err := h.db.BeginTx(h.ctx, nil)
	if err != nil {
		t.Fatalf("begin statio probe: %v", err)
	}
	for _, query := range queries {
		if _, err := tx.ExecContext(h.ctx, query); err != nil {
			_ = tx.Rollback()
			t.Fatalf("statio probe exec: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit statio probe: %v", err)
	}
	return narrowStatioBlocks(t, h) - before
}

// narrowWriteBarPasses reports whether a narrow shape's first-pass block
// touches satisfy the Box 1 bar against the shipped probe's touches: the
// shape must read at most one quarter of the shipped blocks.
func narrowWriteBarPasses(shippedBlocks, shapeBlocks int64) bool {
	return shippedBlocks >= 4*shapeBlocks
}

// narrowDeadlocksPass reports whether the observed deadlock count satisfies
// the Box 1 zero-deadlock bar for concurrent disjoint-scope writers.
func narrowDeadlocksPass(deadlocks int) bool {
	return deadlocks == 0
}

// narrowDrain runs bounded ticks until the backlog drains and returns every
// tick result. The chunk bound keeps every tick short; the loop is the
// maintainer's re-tick while Bounded.
func narrowDrain(t *testing.T, h *keyedHarness, bound int) []supplychaincore.SupplyChainImpactWinnersResweep {
	t.Helper()
	var results []supplychaincore.SupplyChainImpactWinnersResweep
	for h.pendingDepth() > 0 {
		if len(results) >= bound {
			t.Fatalf("backlog not drained after %d bounded ticks (pending %d)", len(results), h.pendingDepth())
		}
		result, err := h.store.ResweepWinnersKeyed(h.ctx)
		if err != nil {
			t.Fatalf("drain tick %d: %v", len(results), err)
		}
		if result.Skipped {
			t.Fatalf("drain tick %d skipped with %d pending", len(results), h.pendingDepth())
		}
		results = append(results, result)
	}
	return results
}

// TestSupplyChainImpactWriterNarrowShapesWriteBarLive builds the three M6
// narrow-store write shapes side by side on QA-shaped payloads with the
// production index set and reports the write bar per shape: WAL bytes per
// fact, CPU per fact, first-pass block touches from the statistics views
// (each shape at most one quarter of the shipped probe), and, at 8 and 16
// concurrent writers, facts written per hour, WAL rate, and the deadlock
// count, which must stay 0. It then runs the full keyed differential:
// every shape's winner digest must equal the shipped digest over the same
// snapshot.
func TestSupplyChainImpactWriterNarrowShapesWriteBarLive(t *testing.T) {
	corpus := narrowProofCorpusFor(t)
	h := newKeyedHarness(t, "#11142 M6 narrow write shapes")
	if corpus.label != "smoke" {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Hour)
		t.Cleanup(cancel)
		h.ctx = ctx
	}
	seeded := narrowSeedCorpus(t, h, corpus, "vuln-intel://11142-narrow")
	t.Logf("seeded %d QA-shaped facts in %d scopes", seeded, corpus.scopes)

	// Shape (c) marginal: identical fact-write passes with and without the
	// trailing-expression index. The fact write itself is sunk cost; the
	// shape question is the index maintenance it adds.
	const marginalFacts = 500
	w0scope := "vuln-intel://11142-narrow-m0/s0000"
	l0 := narrowCurrentLSN(t, h)
	s0 := time.Now()
	narrowWriteScope(t, h, w0scope, marginalFacts, "m0")
	w0wall := time.Since(s0)
	w0 := narrowWALBytes(t, h, l0, narrowCurrentLSN(t, h))
	h.exec(postgres.NarrowShapeDDL(postgres.NarrowShapeIndexExpression))
	w1scope := "vuln-intel://11142-narrow-m1/s0000"
	l1 := narrowCurrentLSN(t, h)
	s1 := time.Now()
	narrowWriteScope(t, h, w1scope, marginalFacts, "m1")
	w1wall := time.Since(s1)
	w1 := narrowWALBytes(t, h, l1, narrowCurrentLSN(t, h))

	// Table shapes over the whole live set, so every shape and the shipped
	// path below read the same snapshot.
	liveFacts := seeded + 2*marginalFacts
	for _, shape := range []postgres.NarrowWriteShape{postgres.NarrowShapePrototype, postgres.NarrowShapeDeduped} {
		h.exec(postgres.NarrowShapeDDL(shape))
	}

	walPerFact := map[postgres.NarrowWriteShape]float64{}
	cpuPerFact := map[postgres.NarrowWriteShape]float64{}
	for _, shape := range []postgres.NarrowWriteShape{postgres.NarrowShapePrototype, postgres.NarrowShapeDeduped} {
		populate := postgres.NarrowShapePopulateSQL(shape)
		if populate == "" {
			t.Fatalf("shape %s has no populate", shape)
		}
		before := narrowCurrentLSN(t, h)
		start := time.Now()
		h.exec(populate)
		elapsed := time.Since(start)
		wal := narrowWALBytes(t, h, before, narrowCurrentLSN(t, h))
		walPerFact[shape] = float64(wal) / float64(liveFacts)
		cpuPerFact[shape] = float64(elapsed.Nanoseconds()) / float64(liveFacts) / 1e6
		t.Logf("shape %s populate: WAL %d bytes over %d facts = %.0f bytes/fact, wall %v = %.4f ms/fact",
			shape, wal, liveFacts, walPerFact[shape], elapsed.Round(time.Millisecond), cpuPerFact[shape])
	}
	if walPerFact[postgres.NarrowShapePrototype] <= walPerFact[postgres.NarrowShapeDeduped] {
		t.Errorf("prototype WAL/fact %.0f <= deduped %.0f: the covering payload must cost more than the deduped row",
			walPerFact[postgres.NarrowShapePrototype], walPerFact[postgres.NarrowShapeDeduped])
	}

	marginalWAL := (float64(w1) - float64(w0)) / float64(marginalFacts)
	marginalCPU := (float64(w1wall.Nanoseconds()) - float64(w0wall.Nanoseconds())) / float64(marginalFacts) / 1e6
	walPerFact[postgres.NarrowShapeIndexExpression] = marginalWAL
	cpuPerFact[postgres.NarrowShapeIndexExpression] = marginalCPU
	t.Logf("shape index-expression marginal: WAL %d vs %d bytes over %d facts = %.0f bytes/fact, wall %v vs %v = %.4f ms/fact",
		w1, w0, marginalFacts, marginalWAL, w1wall.Round(time.Millisecond), w0wall.Round(time.Millisecond), marginalCPU)
	if marginalWAL >= walPerFact[postgres.NarrowShapePrototype] {
		t.Errorf("index-expression marginal WAL/fact %.0f >= prototype %.0f: index maintenance must cost less than a second heap write",
			marginalWAL, walPerFact[postgres.NarrowShapePrototype])
	}

	// Steady-state visibility: a freshly loaded fixture has no visibility
	// map, which forces heap visits and denies every Index Only Scan. QA
	// carries a set map via autovacuum; VACUUM ANALYZE represents that
	// steady state before the read proof.
	h.exec(`VACUUM (ANALYZE) fact_records`)
	h.exec(`VACUUM (ANALYZE) sci_winners_narrow_prototype`)
	h.exec(`VACUUM (ANALYZE) sci_winners_narrow_deduped`)

	// First-pass read proof per shape over the tick's per-identifier probe: 50
	// sampled identifiers, the same set for every arm. The shipped arm runs
	// the shipped active-facts build (derived, not hand-copied) over its own
	// identifier temp table.
	h.exec(`CREATE TEMP TABLE narrow_proof_idents AS
SELECT DISTINCT package_id, repository_id, subject_digest, c0, advisory_id
FROM sci_winners_narrow_prototype LIMIT 50`)
	h.exec(`ANALYZE narrow_proof_idents`)
	h.exec(`CREATE TEMP TABLE ` + postgres.WinnersKeyedIdentsTableForTest + ` AS
SELECT *, now() AS age_captured_at, 0 AS age_seq FROM narrow_proof_idents`)
	h.exec(`ANALYZE ` + postgres.WinnersKeyedIdentsTableForTest)
	// The shipped tick persists the identifier probe to the narrow temp
	// table before the active-facts build reads it; the probe measures all
	// three shipped statements in one transaction, mirroring runKeyedOrFull
	// (post-#11400 the build errors on the missing relation otherwise, and
	// ON COMMIT DROP tables do not survive autocommit execs).
	shippedBlocks := narrowStatioProbe(t, h,
		postgres.WinnersKeyedNarrowBuildSQLForTest,
		postgres.WinnersKeyedNarrowAnalyzeSQLForTest,
		postgres.WinnersKeyedActiveFactsSQLForTest)
	// The bar counts first-pass block touches (buffer hits plus reads) from
	// the statistics views around a real execution of each probe, per shape:
	// each shape must touch at most one quarter of the shipped probe's
	// blocks. EXPLAIN below serves the plan-shape assertions only (which
	// index serves each probe); its buffer lines are plan context, not the
	// bar, and the shipped arm is measured on the executed build, never on
	// an EXPLAIN of the CREATE TABLE AS.
	t.Logf("shipped per-identifier probe: statio block touches=%d", shippedBlocks)
	if shippedBlocks == 0 {
		t.Fatalf("shipped probe recorded 0 statio block touches: the statistics-view measurement is blind")
	}
	h.exec(`DROP TABLE IF EXISTS ` + postgres.WinnersKeyedActiveFactsTableForTest)
	candidates := map[postgres.NarrowWriteShape]int{}
	for _, shape := range postgres.NarrowWriteShapes() {
		firstPass := postgres.NarrowShapeFirstPassSQL(shape, "narrow_proof_idents")
		plan, _, _ := narrowExplain(t, h, firstPass)
		if shape == postgres.NarrowShapeIndexExpression &&
			!strings.Contains(plan, "Index Scan using fact_records_sci_winners_rank_expr_idx") {
			t.Errorf("shape %s first pass does not probe the trailing-expression index:\n%s", shape, plan)
		}
		if (shape == postgres.NarrowShapePrototype || shape == postgres.NarrowShapeDeduped) &&
			!strings.Contains(plan, "covering_idx") {
			t.Errorf("shape %s first pass does not use its covering index:\n%s", shape, plan)
		}
		before := narrowStatioBlocks(t, h)
		var count int
		if err := h.db.QueryRowContext(h.ctx,
			`SELECT count(*) FROM (`+firstPass+`) AS fp`).Scan(&count); err != nil {
			t.Fatalf("shape %s candidate count: %v", shape, err)
		}
		shapeBlocks := narrowStatioBlocks(t, h) - before
		t.Logf("shape %s first pass: statio block touches=%d (shipped %d)", shape, shapeBlocks, shippedBlocks)
		if shapeBlocks == 0 {
			t.Errorf("shape %s recorded 0 statio block touches: the statistics-view measurement is blind", shape)
		}
		if !narrowWriteBarPasses(shippedBlocks, shapeBlocks) {
			t.Errorf("shape %s first pass block touches %d > shipped %d / 4: the narrow first pass must read at least 4x fewer blocks than the shipped probe",
				shape, shapeBlocks, shippedBlocks)
		}
		candidates[shape] = count
		t.Logf("shape %s first pass candidates: %d", shape, count)
	}
	for _, shape := range postgres.NarrowWriteShapes()[1:] {
		if candidates[shape] != candidates[postgres.NarrowWriteShapes()[0]] {
			t.Errorf("shape %s candidates %d != shape %s candidates %d: first passes disagree on the candidate set",
				shape, candidates[shape], postgres.NarrowWriteShapes()[0], candidates[postgres.NarrowWriteShapes()[0]])
		}
	}
	// The shipped temp tables would collide with the drain ticks' own, so
	// drop them before the differential.
	h.exec(`DROP TABLE IF EXISTS ` + postgres.WinnersKeyedActiveFactsTableForTest)
	h.exec(`DROP TABLE IF EXISTS ` + postgres.WinnersKeyedIdentsTableForTest)

	// Full keyed differential over the same snapshot.
	drainResults := narrowDrain(t, h, h.pendingDepth()/150+8)
	t.Logf("drained %d bounded ticks", len(drainResults))
	shippedDigest := narrowDigestOfWinners(t, winnersLiveRead(t, h.ctx, h.db))
	t.Logf("shipped winner digest: %s", shippedDigest)

	vehicles := map[postgres.NarrowWriteShape]string{
		postgres.NarrowShapePrototype:       "sci_winners_narrow_prototype",
		postgres.NarrowShapeDeduped:         "active_facts_narrow_deduped",
		postgres.NarrowShapeIndexExpression: "active_facts_narrow_expr",
	}
	h.exec(`CREATE TEMP TABLE active_facts_narrow_deduped AS
SELECT fact.* FROM fact_records AS fact
WHERE fact.fact_id IN (SELECT fact_id FROM sci_winners_narrow_deduped)`)
	h.exec(`CREATE TEMP TABLE narrow_proof_all_idents AS
SELECT DISTINCT package_id, repository_id, subject_digest, c0, advisory_id
FROM sci_winners_narrow_prototype`)
	h.exec(`CREATE TEMP TABLE active_facts_narrow_expr AS
SELECT fact.* FROM fact_records AS fact
WHERE fact.fact_id IN (SELECT fact_id FROM (` +
		postgres.NarrowShapeFirstPassSQL(postgres.NarrowShapeIndexExpression, "narrow_proof_all_idents") + `) AS fp)`)
	for _, shape := range postgres.NarrowWriteShapes() {
		digest := narrowVehicleDigest(t, h, vehicles[shape])
		if digest != shippedDigest {
			t.Errorf("shape %s winner digest %s != shipped %s", shape, digest, shippedDigest)
		} else {
			t.Logf("shape %s winner digest matches shipped %s", shape, digest)
		}
	}

	// Write bar at 8 and 16 concurrent writers on disjoint scopes.
	for _, writers := range narrowProofWriters() {
		var wg sync.WaitGroup
		errs := make([]error, writers)
		start := time.Now()
		lsnBefore := narrowCurrentLSN(t, h)
		var facts atomic.Int64
		for w := 0; w < writers; w++ {
			wg.Add(1)
			go func(w int) {
				defer wg.Done()
				scope := fmt.Sprintf("vuln-intel://11142-narrow-w%d/s%04d", writers, w)
				if err := func() error {
					n := narrowWriteScope(t, h, scope, 50, fmt.Sprintf("w%d", writers*100+w))
					facts.Add(int64(n))
					return nil
				}(); err != nil {
					errs[w] = err
				}
			}(w)
		}
		wg.Wait()
		elapsed := time.Since(start)
		for w, err := range errs {
			if err != nil {
				t.Fatalf("%d writers: writer %d: %v", writers, w, err)
			}
		}
		n := facts.Load()
		wal := narrowWALBytes(t, h, lsnBefore, narrowCurrentLSN(t, h))
		var deadlocks int
		if err := h.db.QueryRowContext(h.ctx,
			`SELECT deadlocks FROM pg_stat_database WHERE datname = current_database()`).Scan(&deadlocks); err != nil {
			t.Fatalf("deadlocks: %v", err)
		}
		hours := elapsed.Hours()
		secs := elapsed.Seconds()
		if secs == 0 {
			secs = 1
		}
		t.Logf("%d writers: %d facts in %v = %.0f facts/hr, WAL %d bytes = %.2f MB/s, deadlocks=%d",
			writers, n, elapsed.Round(time.Millisecond), float64(n)/hours, wal, float64(wal)/secs/1e6, deadlocks)
		if !narrowDeadlocksPass(deadlocks) {
			t.Errorf("%d writers: pg_stat_database.deadlocks = %d, want 0 (concurrent disjoint-scope writers must not deadlock the tick)", writers, deadlocks)
		}
		drainStart := time.Now()
		drainResults := narrowDrain(t, h, h.pendingDepth()/150+8)
		drainWall := time.Since(drainStart)
		t.Logf("%d writers: drained in %d bounded ticks in %v", writers, len(drainResults), drainWall.Round(time.Millisecond))
		h.assertParity(fmt.Sprintf("%d writers", writers))
	}
}

// TestSupplyChainImpactWinnersNarrowNearMissSingleFlipLive is the Box 1/2
// RED test for #11142: one contested canonical key, exactly one ranking
// input flipped. The control phase rewrites a scope with byte-identical
// findings and requires the winner digest to stay byte-identical (the
// differential is not volatile). The flip phase raises the loser's
// priority_score above the winner's and requires the winner digest to move
// while the winner count stays unchanged (the differential is sensitive to
// a one-input near miss). The shape phase replays the M6 vehicle
// differential on the flipped snapshot: every narrow shape's winner digest
// must equal the shipped digest (the shapes preserve the flipped truth).
func TestSupplyChainImpactWinnersNarrowNearMissSingleFlipLive(t *testing.T) {
	h := newKeyedHarness(t, "#11142 Box 1/2 narrow near-miss single flip")

	const (
		scopeA = "vuln-intel://11142-nearmiss/a"
		scopeB = "vuln-intel://11142-nearmiss/b"
		genA   = scopeA + "/g1"
		genB   = scopeB + "/g1"
	)
	h.seedScope(scopeA, genA)
	h.seedScope(scopeB, genB)

	// The contested copies share every canonical-key field (the key carries
	// no scope), so both scopes feed one canonical key with source_count 2.
	// The control key never moves.
	contested := func(priority int) supplychaincore.SupplyChainImpactFinding {
		f := keyedFinding("CVE-2026-11000", "pkg:npm/nearmiss", priority)
		f.RepositoryID = "repo-nearmiss"
		return f
	}
	controlFinding := func() supplychaincore.SupplyChainImpactFinding {
		f := keyedFinding("CVE-2026-11001", "pkg:npm/nearmiss-control", 7)
		f.RepositoryID = "repo-nearmiss"
		return f
	}
	// writeA rewrites scope A carrying the contested copy at priority plus
	// the untouched control finding, so a rewrite never drops a key.
	writeA := func(priority int) {
		t.Helper()
		if _, err := h.writer.WriteSupplyChainImpactFindings(h.ctx, supplychaincore.SupplyChainImpactWrite{
			IntentID: "intent-11142-nearmiss", ScopeID: scopeA, GenerationID: genA,
			SourceSystem: "vulnerability_intelligence", Cause: "11142 near-miss single flip",
			Findings:     []supplychaincore.SupplyChainImpactFinding{contested(priority), controlFinding()},
			FencingToken: keyedFenceTokens.Add(1),
		}); err != nil {
			t.Fatalf("write scope A at priority %d: %v", priority, err)
		}
	}

	writeA(10)
	if _, err := h.writer.WriteSupplyChainImpactFindings(h.ctx, supplychaincore.SupplyChainImpactWrite{
		IntentID: "intent-11142-nearmiss", ScopeID: scopeB, GenerationID: genB,
		SourceSystem: "vulnerability_intelligence", Cause: "11142 near-miss single flip",
		Findings:     []supplychaincore.SupplyChainImpactFinding{contested(50)},
		FencingToken: keyedFenceTokens.Add(1),
	}); err != nil {
		t.Fatalf("write scope B: %v", err)
	}
	narrowDrain(t, h, 32)
	h.assertParity("near-miss baseline")
	before := narrowDigestOfWinners(t, winnersLiveRead(t, h.ctx, h.db))
	beforeCount := h.count(`SELECT count(*) FROM supply_chain_impact_canonical_winners`)
	if beforeCount != 2 {
		t.Fatalf("baseline winner count = %d, want 2 (one contested key plus one control key)", beforeCount)
	}
	t.Logf("baseline digest %s over %d winners (A=10 loses to B=50)", before, beforeCount)

	// Control: a byte-identical rewrite of scope A must not move the digest.
	writeA(10)
	narrowDrain(t, h, 32)
	h.assertParity("near-miss control rewrite")
	controlDigest := narrowDigestOfWinners(t, winnersLiveRead(t, h.ctx, h.db))
	if controlDigest != before {
		t.Fatalf("control rewrite moved the digest: %s -> %s (a no-op rewrite must be digest-stable)", before, controlDigest)
	}
	t.Logf("control rewrite digest stable: %s", controlDigest)

	// Flip: exactly one ranking input moves — A's copy outranks B's.
	writeA(90)
	narrowDrain(t, h, 32)
	h.assertParity("near-miss flip")
	after := narrowDigestOfWinners(t, winnersLiveRead(t, h.ctx, h.db))
	afterCount := h.count(`SELECT count(*) FROM supply_chain_impact_canonical_winners`)
	if after == before {
		t.Fatalf("flip left the digest at %s: raising the loser's priority_score above the winner's must move the winner", after)
	}
	if afterCount != beforeCount {
		t.Fatalf("flip moved the winner count %d -> %d: one re-ranked key must keep the same winner rows", beforeCount, afterCount)
	}
	t.Logf("flip moved the digest %s -> %s with count unchanged at %d", before, after, afterCount)

	// Shape phase: every narrow shape preserves the flipped truth.
	for _, shape := range []postgres.NarrowWriteShape{postgres.NarrowShapePrototype, postgres.NarrowShapeDeduped} {
		h.exec(postgres.NarrowShapeDDL(shape))
		h.exec(postgres.NarrowShapePopulateSQL(shape))
	}
	h.exec(postgres.NarrowShapeDDL(postgres.NarrowShapeIndexExpression))
	h.exec(`CREATE TEMP TABLE active_facts_narrow_deduped AS
SELECT fact.* FROM fact_records AS fact
WHERE fact.fact_id IN (SELECT fact_id FROM sci_winners_narrow_deduped)`)
	h.exec(`CREATE TEMP TABLE narrow_proof_all_idents AS
SELECT DISTINCT package_id, repository_id, subject_digest, c0, advisory_id
FROM sci_winners_narrow_prototype`)
	h.exec(`CREATE TEMP TABLE active_facts_narrow_expr AS
SELECT fact.* FROM fact_records AS fact
WHERE fact.fact_id IN (SELECT fact_id FROM (` +
		postgres.NarrowShapeFirstPassSQL(postgres.NarrowShapeIndexExpression, "narrow_proof_all_idents") + `) AS fp)`)
	vehicles := map[postgres.NarrowWriteShape]string{
		postgres.NarrowShapePrototype:       "sci_winners_narrow_prototype",
		postgres.NarrowShapeDeduped:         "active_facts_narrow_deduped",
		postgres.NarrowShapeIndexExpression: "active_facts_narrow_expr",
	}
	for _, shape := range postgres.NarrowWriteShapes() {
		if digest := narrowVehicleDigest(t, h, vehicles[shape]); digest != after {
			t.Errorf("shape %s vehicle digest %s != flipped shipped digest %s", shape, digest, after)
		} else {
			t.Logf("shape %s vehicle digest matches flipped shipped %s", shape, digest)
		}
	}
}
