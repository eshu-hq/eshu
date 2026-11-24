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
// fact, CPU per fact, first-pass buffers, and, at 8 and 16 concurrent
// writers, facts written per hour, WAL rate, and the deadlock profile. It
// then runs the full keyed differential: every shape's winner digest must
// equal the shipped digest over the same snapshot.
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
	shippedPlan, shippedHits, shippedReads := narrowExplain(t, h, postgres.WinnersKeyedActiveFactsSQLForTest)
	t.Logf("shipped per-identifier probe: buffers hit=%d read=%d", shippedHits, shippedReads)
	_ = shippedPlan
	candidates := map[postgres.NarrowWriteShape]int{}
	for _, shape := range postgres.NarrowWriteShapes() {
		firstPass := postgres.NarrowShapeFirstPassSQL(shape, "narrow_proof_idents")
		plan, hits, reads := narrowExplain(t, h, firstPass)
		t.Logf("shape %s first pass: buffers hit=%d read=%d", shape, hits, reads)
		if shape == postgres.NarrowShapeIndexExpression &&
			!strings.Contains(plan, "Index Scan using fact_records_sci_winners_rank_expr_idx") {
			t.Errorf("shape %s first pass does not probe the trailing-expression index:\n%s", shape, plan)
		}
		if (shape == postgres.NarrowShapePrototype || shape == postgres.NarrowShapeDeduped) &&
			!strings.Contains(plan, "covering_idx") {
			t.Errorf("shape %s first pass does not use its covering index:\n%s", shape, plan)
		}
		if hits+reads >= shippedHits+shippedReads {
			t.Errorf("shape %s first pass buffers %d >= shipped %d", shape, hits+reads, shippedHits+shippedReads)
		}
		var count int
		if err := h.db.QueryRowContext(h.ctx,
			`SELECT count(*) FROM (`+firstPass+`) AS fp`).Scan(&count); err != nil {
			t.Fatalf("shape %s candidate count: %v", shape, err)
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
		drainStart := time.Now()
		drainResults := narrowDrain(t, h, h.pendingDepth()/150+8)
		drainWall := time.Since(drainStart)
		t.Logf("%d writers: drained in %d bounded ticks in %v", writers, len(drainResults), drainWall.Round(time.Millisecond))
		h.assertParity(fmt.Sprintf("%d writers", writers))
	}
}
