// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// The #10972 single-scan proofs: the new renderer keeps the shipped range
// contract (RED 1 equivalence over the fixture corpus) within the 1.25x
// parity bound the cost test pins (RED 2 sub-half bound void per arbiter),
// and the stored side rides the new canonical identifier index whose
// expressions match the code byte for byte (RED 3). All three fail before
// the renderer exists: they reference the single-scan statement pair the
// new file renders.

// singleScanRangeSnapshot is one range read for old-versus-new comparison.
type singleScanRangeSnapshot struct {
	raw      int64
	facts    int64
	last     bool
	oversize bool
	pipe     bool
	hi       *string
	set      map[string]bool
}

func snapshotHi(ident *postgres.WinnersAuditRangeIdent) *string {
	if ident == nil {
		return nil
	}
	s := strings.Join([]string{ident.PackageID, ident.RepositoryID, ident.SubjectDigest, ident.C0, ident.AdvisoryID}, "\x1f")
	return &s
}

// sweepSnapshotAll walks one sweep at budget with the given statement pair,
// returning every range in order.
func sweepSnapshotAll(t *testing.T, ctx context.Context, reader postgres.SQLDB, budget int, first, next string) []singleScanRangeSnapshot {
	t.Helper()
	var out []singleScanRangeSnapshot
	var lo *postgres.WinnersAuditRangeIdent
	for {
		rng, err := postgres.ReadWinnersAuditRangeWithSQLForTest(ctx, reader, lo, budget, first, next)
		if err != nil {
			t.Fatalf("budget %d range %d: %v", budget, len(out), err)
		}
		set := map[string]bool{}
		for _, c := range rng.Candidates {
			set[auditIdentKey(c)] = true
		}
		out = append(out, singleScanRangeSnapshot{
			raw: rng.RawRows, facts: rng.Facts, last: rng.Last,
			oversize: rng.Oversize, pipe: rng.Pipe, hi: snapshotHi(rng.Hi), set: set,
		})
		if rng.Last {
			return out
		}
		if len(out) > 100000 {
			t.Fatalf("budget %d: sweep did not end", budget)
		}
		lo = rng.Hi
	}
}

func requireSnapshotsEqual(t *testing.T, budget int, want, got []singleScanRangeSnapshot) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("budget %d: shipped %d ranges, single-scan %d", budget, len(want), len(got))
	}
	for i := range want {
		w, g := want[i], got[i]
		if w.raw != g.raw || w.facts != g.facts || w.last != g.last || w.oversize != g.oversize || w.pipe != g.pipe {
			t.Errorf("budget %d range %d: shipped %+v, single-scan %+v", budget, i, w, g)
		}
		if (w.hi == nil) != (g.hi == nil) || (w.hi != nil && *w.hi != *g.hi) {
			t.Errorf("budget %d range %d: shipped hi %v, single-scan hi %v", budget, i, w.hi, g.hi)
		}
		for k := range w.set {
			if !g.set[k] {
				t.Errorf("budget %d range %d: single-scan misses candidate %q", budget, i, k)
			}
		}
		for k := range g.set {
			if !w.set[k] {
				t.Errorf("budget %d range %d: single-scan adds candidate %q", budget, i, k)
			}
		}
	}
}

// TestSupplyChainImpactWinnersResweepSingleScanUnionEqualsShippedLive is the #10972
// RED 1 differential: the new single-scan pair walks the same ranges as the
// shipped pair (identical bounds, facts, flags, and candidate sets per
// range) and their union equals the shipped single-statement candidate set,
// at every budget including the oversize and whole-corpus shapes.
func TestSupplyChainImpactWinnersResweepSingleScanUnionEqualsShippedLive(t *testing.T) {
	h := newKeyedHarness(t, "#10972 single-scan differential")
	liveFacts := auditRangeSeed(t, h, auditRangeFixtureIdentifiers)
	auditRangePerturb(t, h)
	single := auditSingleStatementCandidates(t, h.ctx, h.db)

	reader := postgres.SQLDB{DB: h.db}
	for _, budget := range []int{1, 7, 50, 200, 1000, 100000} {
		shipped := sweepSnapshotAll(t, h.ctx, reader, budget, postgres.WinnersAuditFirstRangeSQLForTest, postgres.WinnersAuditNextRangeSQLForTest)
		fresh := sweepSnapshotAll(t, h.ctx, reader, budget, postgres.WinnersAuditSingleScanFirstRangeSQLForTest, postgres.WinnersAuditSingleScanNextRangeSQLForTest)
		requireSnapshotsEqual(t, budget, shipped, fresh)
		union := map[string]int{}
		var facts int64
		var oversize int
		for _, s := range fresh {
			facts += s.facts
			if s.oversize {
				oversize++
			}
			if s.pipe {
				t.Errorf("budget %d: a range reported '|' in an identifier", budget)
			}
			for k := range s.set {
				union[k]++
			}
		}
		if missing := auditSetDiff(single, union); len(missing) > 0 {
			t.Errorf("budget %d: %d candidates missing from the single-scan union: %v", budget, len(missing), missing)
		}
		for k, n := range union {
			if !single[k] {
				t.Errorf("budget %d: extra single-scan candidate %q", budget, k)
			}
			if n > 1 {
				t.Errorf("budget %d: candidate %q appeared in %d ranges", budget, k, n)
			}
		}
		if facts != int64(liveFacts) {
			t.Errorf("budget %d: single-scan recomputed %d facts, want each of the %d live facts once", budget, facts, liveFacts)
		}
		if budget <= 50 && oversize == 0 {
			t.Errorf("budget %d: no range ran the %d-fact identifier alone", budget, auditRangeOversizeFacts)
		}
		t.Logf("budget %d: %d ranges, %d oversize, %d candidates (single %d), %d facts",
			budget, len(fresh), oversize, len(union), len(single), facts)
	}
}

// TestSupplyChainImpactWinnersResweepSingleScanNullTailKnifeEdgeLive pins the one
// reachable divergence: a first range whose budget lands exactly on the
// corpus end pulls the NULL-identifier operator row into range_raw, where it
// sorts first under ORDER BY DESC. The shipped pair then takes the oversize
// arm with NULL bounds and returns no upper bound (the reader errors); the
// single-scan pair completes the sweep instead, with the same candidate
// union as the single statement. Under a clean NULL probe (which gates every
// sweep) range_raw carries no NULL identifiers and both pairs agree, as the
// differential above proves.
func TestSupplyChainImpactWinnersResweepSingleScanNullTailKnifeEdgeLive(t *testing.T) {
	h := newKeyedHarness(t, "#10972 single-scan NULL-tail knife edge")
	auditRangeSeed(t, h, auditRangeFixtureIdentifiers)
	auditRangePerturb(t, h)
	single := auditSingleStatementCandidates(t, h.ctx, h.db)
	total := h.count(`SELECT count(*) FROM fact_records WHERE fact_kind = $1 AND is_tombstone = FALSE`,
		facts.ReducerSupplyChainImpactFindingFactKind)
	if total <= 0 {
		t.Fatalf("fixture corpus is empty")
	}

	reader := postgres.SQLDB{DB: h.db}
	_, err := postgres.ReadWinnersAuditRangeWithSQLForTest(h.ctx, reader, nil, total,
		postgres.WinnersAuditFirstRangeSQLForTest, postgres.WinnersAuditNextRangeSQLForTest)
	if err == nil || !strings.Contains(err.Error(), "no upper bound") {
		t.Fatalf("shipped pair at budget == corpus (%d) = %v, want the no-upper-bound error", total, err)
	}
	fresh := sweepSnapshotAll(t, h.ctx, reader, total,
		postgres.WinnersAuditSingleScanFirstRangeSQLForTest, postgres.WinnersAuditSingleScanNextRangeSQLForTest)
	union := map[string]int{}
	for _, s := range fresh {
		for k := range s.set {
			union[k]++
		}
	}
	if missing := auditSetDiff(single, union); len(missing) > 0 {
		t.Errorf("knife edge: %d candidates missing from the single-scan union: %v", len(missing), missing)
	}
	for k, n := range union {
		if !single[k] {
			t.Errorf("knife edge: extra single-scan candidate %q", k)
		}
		if n > 1 {
			t.Errorf("knife edge: candidate %q appeared in %d ranges", k, n)
		}
	}
}

// explainBuffers is the whole-statement buffer total of an EXPLAIN
// (ANALYZE, BUFFERS, FORMAT JSON) plan: the root node's shared and
// temporary blocks.
type explainBuffers struct {
	shared int64
	temp   int64
}

func planBuffers(t *testing.T, raw json.RawMessage) explainBuffers {
	t.Helper()
	var plans []struct {
		Plan struct {
			SharedHitBlocks   int64 `json:"Shared Hit Blocks"`
			SharedReadBlocks  int64 `json:"Shared Read Blocks"`
			TempReadBlocks    int64 `json:"Temp Read Blocks"`
			TempWrittenBlocks int64 `json:"Temp Written Blocks"`
		} `json:"Plan"`
	}
	if err := json.Unmarshal(raw, &plans); err != nil {
		t.Fatalf("decode explain json: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("explain json has %d plans, want 1", len(plans))
	}
	p := plans[0].Plan
	return explainBuffers{
		shared: p.SharedHitBlocks + p.SharedReadBlocks,
		temp:   p.TempReadBlocks + p.TempWrittenBlocks,
	}
}

// explainRangeFirstTextGUC runs EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) over
// the first-range statement at budget and returns the raw plan text, with
// one session GUC applied to the statement only (empty applies none). It is
// the one seam every first-range plan proof reads through, so a plan-shape
// assertion covers the rendered statement itself instead of a hand-written
// proxy. The GUC rides SET LOCAL in an explicit transaction on a pinned
// connection, so pool reuse can neither leak it into other statements nor
// drop it from this one.
func explainRangeFirstTextGUC(t *testing.T, h *keyedHarness, stmt string, budget int, guc string) string {
	t.Helper()
	conn, err := h.db.Conn(h.ctx)
	if err != nil {
		t.Fatalf("explain range conn: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(h.ctx, "BEGIN"); err != nil {
		t.Fatalf("explain range begin: %v", err)
	}
	defer func() { _, _ = conn.ExecContext(context.Background(), "ROLLBACK") }()
	if guc != "" {
		if _, err := conn.ExecContext(h.ctx, "SET LOCAL "+guc); err != nil {
			t.Fatalf("explain range GUC %q: %v", guc, err)
		}
	}
	rows, err := conn.QueryContext(h.ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+stmt, budget)
	if err != nil {
		t.Fatalf("explain range: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var text string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan explain: %v", err)
		}
		text += line
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("explain range: %v", err)
	}
	return text
}

// explainRangeFirstText runs EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) over the
// first-range statement at budget and returns the raw plan text. It is the
// one seam every first-range plan proof reads through, so a plan-shape
// assertion covers the rendered statement itself instead of a hand-written
// proxy.
func explainRangeFirstText(t *testing.T, h *keyedHarness, stmt string, budget int) string {
	t.Helper()
	return explainRangeFirstTextGUC(t, h, stmt, budget, "")
}

// explainRangeFirst runs EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) over the
// first-range statement at budget and returns the root-node touched buffers.
// The root node is the whole-statement total; summing every node would
// double-count, because EXPLAIN reports each parent's buffers cumulatively
// with its children.
func explainRangeFirst(t *testing.T, h *keyedHarness, stmt string, budget int) explainBuffers {
	t.Helper()
	return planBuffers(t, json.RawMessage(explainRangeFirstText(t, h, stmt, budget)))
}

// decodeExplainPlan decodes the single plan tree of one EXPLAIN
// (ANALYZE, BUFFERS, FORMAT JSON) text from the explainRangeFirst seam.
func decodeExplainPlan(t *testing.T, text string) map[string]any {
	t.Helper()
	var plans []struct {
		Plan map[string]any `json:"Plan"`
	}
	if err := json.Unmarshal([]byte(text), &plans); err != nil {
		t.Fatalf("decode explain plan: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("explain json has %d plans, want 1", len(plans))
	}
	return plans[0].Plan
}

// planSubtreeHas reports whether the plan tree rooted at node holds a node
// satisfying pred.
func planSubtreeHas(node map[string]any, pred func(map[string]any) bool) bool {
	if pred(node) {
		return true
	}
	kids, _ := node["Plans"].([]any)
	for _, kid := range kids {
		if child, ok := kid.(map[string]any); ok && planSubtreeHas(child, pred) {
			return true
		}
	}
	return false
}

// isWinnersBtreeScan reports an index path on the named index over the
// canonical winners: a plain Index Scan on the relation, or the Bitmap
// Index Scan under a Bitmap Heap Scan (which carries no Relation Name).
func isWinnersBtreeScan(index string) func(map[string]any) bool {
	return func(node map[string]any) bool {
		if node["Index Name"] != index {
			return false
		}
		return node["Node Type"] == "Index Scan" && node["Relation Name"] == "supply_chain_impact_canonical_winners" ||
			node["Node Type"] == "Bitmap Index Scan"
	}
}

// isWinnersSeqScan reports a sequential scan of the canonical winners table.
func isWinnersSeqScan(node map[string]any) bool {
	return node["Node Type"] == "Seq Scan" && node["Relation Name"] == "supply_chain_impact_canonical_winners"
}

// storedRangeArms finds the stored_range UNION ALL Append in the rendered
// first-range plan: the one two-child Append with one arm riding the named
// btree index on the winners table and the other sequentially scanning it.
// It returns the btree arm first, then the sequential arm.
func storedRangeArms(t *testing.T, plan map[string]any, index string) (btree, seq map[string]any) {
	t.Helper()
	btreeScan := isWinnersBtreeScan(index)
	var found []struct{ btree, seq map[string]any }
	var walk func(node map[string]any)
	walk = func(node map[string]any) {
		if node["Node Type"] == "Append" {
			kids, _ := node["Plans"].([]any)
			if len(kids) == 2 {
				a, aok := kids[0].(map[string]any)
				b, bok := kids[1].(map[string]any)
				if aok && bok {
					aIdx, bIdx := planSubtreeHas(a, btreeScan), planSubtreeHas(b, btreeScan)
					aSeq, bSeq := planSubtreeHas(a, isWinnersSeqScan), planSubtreeHas(b, isWinnersSeqScan)
					if (aIdx || bIdx) && (aSeq || bSeq) {
						if aIdx {
							found = append(found, struct {
								btree, seq map[string]any
							}{a, b})
						} else {
							found = append(found, struct {
								btree, seq map[string]any
							}{b, a})
						}
					}
				}
			}
		}
		kids, _ := node["Plans"].([]any)
		for _, kid := range kids {
			if child, ok := kid.(map[string]any); ok {
				walk(child)
			}
		}
	}
	walk(plan)
	if len(found) != 1 {
		t.Fatalf("rendered plan holds %d stored_range arm pairs, want exactly 1", len(found))
	}
	return found[0].btree, found[0].seq
}

// TestSupplyChainImpactWinnersResweepSingleScanCostLive is the #10972 RED 2 cost
// proof: on a padded fixture the single-scan statement costs no more than
// the shipped one within a parity bound, so the primary-key re-attachment
// cannot silently regress the range read.
//
// Measured verdict (local Postgres 18, padded 1500-identifier fixture,
// budgets 2000/10000, three stable reps): the handoff's <1/2 premise does
// NOT hold — totals land at parity (single-scan +0.2%..+1.4% shared, time
// +5% stable). The structural reason: heap fetches pull the full tuple
// whatever the select list projects, so the narrow scan saves no heap
// buffers, while the re-attachment adds about one PK probe per kept row.
// The narrower passes only pay past sort/tuplestore spill, which this
// fixture (like the default-budget sweep) does not reach; the winner-body
// recompute dominates both statements equally. The <1/2 bound belongs to a
// QA-scale spill regime unproven here and needs an arbiter ruling before
// any claim built on it ships. This test pins the proven relationship
// (parity with headroom) and logs the full evidence.
func TestSupplyChainImpactWinnersResweepSingleScanCostLive(t *testing.T) {
	h := newKeyedHarness(t, "#10972 single-scan cost")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	t.Cleanup(cancel)
	h.ctx = ctx
	auditRangeSeed(t, h, 1500)
	h.exec(`UPDATE fact_records SET payload = payload || jsonb_build_object('pad',
  (SELECT string_agg(md5(fact_id || i::text), '') FROM generate_series(1, 64) AS i))
WHERE fact_kind = 'reducer_supply_chain_impact_finding'`)
	h.exec(`VACUUM ANALYZE fact_records`)
	auditRangePerturb(t, h)

	for _, budget := range []int{2000, 10000} {
		old := explainRangeFirst(t, h, postgres.WinnersAuditFirstRangeSQLForTest, budget)
		fresh := explainRangeFirst(t, h, postgres.WinnersAuditSingleScanFirstRangeSQLForTest, budget)
		oldTotal, freshTotal := old.shared+old.temp, fresh.shared+fresh.temp
		t.Logf("budget %d: shipped shared=%d temp=%d total=%d; single-scan shared=%d temp=%d total=%d",
			budget, old.shared, old.temp, oldTotal, fresh.shared, fresh.temp, freshTotal)
		if oldTotal == 0 || freshTotal == 0 {
			t.Fatalf("budget %d: empty buffer totals (shipped %d, single-scan %d): the plans did not execute", budget, oldTotal, freshTotal)
		}
		if float64(freshTotal) > 1.25*float64(oldTotal) {
			t.Errorf("budget %d: single-scan buffers %d exceed 1.25x the shipped %d", budget, freshTotal, oldTotal)
		}
	}
}

// canonicalIdentIndexDef finds the canonical identifier index migration in
// the embedded bootstrap definitions by its stable name suffix, so the pin
// never hand-copies the migration number.
func canonicalIdentIndexDef(t *testing.T) (name, sql string) {
	t.Helper()
	var found []string
	for _, def := range postgres.BootstrapDefinitions() {
		if strings.HasSuffix(def.Path, "_supply_chain_impact_winners_canonical_ident_idx.sql") {
			found = append(found, def.Path)
			name, sql = def.Path, def.SQL
		}
	}
	if len(found) != 1 {
		t.Fatalf("canonical ident index migrations = %v, want exactly one", found)
	}
	return name, sql
}

// splitTopLevelCommas splits s at commas outside any parentheses.
func splitTopLevelCommas(s string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

// TestSupplyChainImpactWinnersResweepCanonicalIdentIndexLive is the #10972 RED 3
// proof: migration 356's index expressions are byte-identical to the stored
// identifier expressions (after stripping the `w.` qualifier and the
// index-syntax parentheses), and the rendered first-range statement's own
// plan rides the new index on the stored side. Under #11441 verdict-B the
// stored_range predicate is a complementary UNION ALL, so the pin is
// per-arm: the bounded arm (NOT is_last with the pure row-comparison) takes
// the btree path, and the only sequential scan of the winners table is the
// unbounded arm (bare is_last).
func TestSupplyChainImpactWinnersResweepCanonicalIdentIndexLive(t *testing.T) {
	_, migrationSQL := canonicalIdentIndexDef(t)
	created := regexp.MustCompile(`(?m)CREATE INDEX CONCURRENTLY IF NOT EXISTS (\S+)`).FindStringSubmatch(migrationSQL)
	if created == nil {
		t.Fatalf("canonical ident migration creates no concurrent index:\n%s", migrationSQL)
	}
	indexName := created[1]
	open := strings.Index(migrationSQL, "supply_chain_impact_canonical_winners (")
	if open < 0 {
		t.Fatalf("canonical ident migration has no column list:\n%s", migrationSQL)
	}
	list := migrationSQL[strings.Index(migrationSQL[open:], "(")+open+1:]
	depth := 1
	end := -1
	for i := 0; i < len(list); i++ {
		switch list[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				end = i
			}
		}
		if end >= 0 {
			break
		}
	}
	if end < 0 {
		t.Fatalf("canonical ident migration column list never closes:\n%s", migrationSQL)
	}
	cols := splitTopLevelCommas(list[:end])
	if len(cols) != 5 {
		t.Fatalf("canonical ident index has %d columns, want 5", len(cols))
	}
	for i, expr := range postgres.WinnersAuditRangeStoredIdentExprsForTest {
		want := strings.ReplaceAll(expr, "w.", "")
		got := strings.TrimSpace(cols[i])
		got = strings.TrimSuffix(strings.TrimPrefix(got, "("), ")")
		if got != want {
			t.Errorf("index expression %d = %s, want stored identifier bytes %s", i, got, want)
		}
	}

	h := newKeyedHarness(t, "#10972 canonical ident index")
	live := auditRangeSeed(t, h, auditRangeFixtureIdentifiers)
	const budget = 200
	if live < budget {
		t.Fatalf("fixture holds %d live facts, want at least the budget %d so the first range is not last", live, budget)
	}
	h.exec(`ANALYZE supply_chain_impact_canonical_winners`)
	// The EXPLAINed text IS the rendered single-scan first-range statement at
	// a mid-size budget, read through the explainRangeFirst seam, so the pin
	// covers the range's own stored-side access. A hand-written
	// scalar-predicate proxy could pass while the rendered statement
	// sequentially scans the winners table.
	//
	// The plan runs with enable_seqscan forced off for the statement only.
	// The bound subselects are plan-time parameters, so the row-comparison
	// estimates a fixed default fraction of the table (a third on the
	// fixtures probed) and the natural bitmap-versus-seq choice is a costing
	// coin flip on any padded fixture. Forcing proves the verdict-B theory
	// itself — the bounded arm's predicate is index-compatible —
	// deterministically, instead of pinning one flip of that coin. The
	// folded pre-UNION ALL statement is the control: under the same forcing
	// it still sequentially scans, proving the old OR over the is_last
	// InitPlan is what blocked the btree path.
	renderedText := explainRangeFirstTextGUC(t, h, postgres.WinnersAuditSingleScanFirstRangeSQLForTest,
		budget, "enable_seqscan = 'off'")
	rendered := decodeExplainPlan(t, renderedText)
	foldedText := explainRangeFirstTextGUC(t, h,
		storedRangePreUnionSQL(t, postgres.WinnersAuditSingleScanFirstRangeSQLForTest),
		budget, "enable_seqscan = 'off'")
	if planSubtreeHas(decodeExplainPlan(t, foldedText), isWinnersBtreeScan(indexName)) {
		t.Errorf("folded pre-UNION ALL statement rides %s under forcing: the control does not discriminate", indexName)
	}
	t.Logf("rendered first-range plan:\n%s", renderedText)
	// #11441 verdict-B: the stored_range predicate is a complementary
	// UNION ALL, so the bounded arm (NOT is_last with the pure
	// row-comparison) rides the 356 btree path while only the unbounded
	// arm (bare is_last) sequentially scans the winners table.
	btree, seq := storedRangeArms(t, rendered, indexName)
	if planSubtreeHas(btree, isWinnersSeqScan) {
		t.Errorf("bounded stored_range arm sequentially scans supply_chain_impact_canonical_winners:\n%s", renderedText)
	}
	if planSubtreeHas(seq, isWinnersBtreeScan(indexName)) {
		t.Errorf("unbounded stored_range arm touches %s:\n%s", indexName, renderedText)
	}
	rawBtree, _ := json.Marshal(btree)
	rawSeq, _ := json.Marshal(seq)
	if !strings.Contains(string(rawBtree), "NOT (InitPlan") {
		t.Errorf("bounded stored_range arm is not gated on NOT is_last:\n%s", renderedText)
	}
	if !strings.Contains(string(rawSeq), "(InitPlan") || strings.Contains(string(rawSeq), "NOT (InitPlan") {
		t.Errorf("unbounded stored_range arm is not gated on bare is_last:\n%s", renderedText)
	}
	var seqScans, btreeScans int
	var count func(node map[string]any)
	count = func(node map[string]any) {
		if isWinnersSeqScan(node) {
			seqScans++
		}
		if isWinnersBtreeScan(indexName)(node) {
			btreeScans++
		}
		kids, _ := node["Plans"].([]any)
		for _, kid := range kids {
			if child, ok := kid.(map[string]any); ok {
				count(child)
			}
		}
	}
	count(rendered)
	if seqScans != 1 {
		t.Errorf("rendered plan sequentially scans supply_chain_impact_canonical_winners %d times, want exactly 1 (the unbounded arm)", seqScans)
	}
	if btreeScans != 1 {
		t.Errorf("rendered plan rides %s %d times, want exactly 1 (the bounded arm)", indexName, btreeScans)
	}
}

// storedRangeUnionSpine is the UNION ALL arm split unique to the rewritten
// stored_range CTE: the unbounded arm (bare is_last) over the winners table
// followed by the bounded arm (NOT is_last with the pure row-comparison).
const storedRangeUnionSpine = `
    UNION ALL
    SELECT w.*
    FROM supply_chain_impact_canonical_winners AS w
    WHERE `

// storedRangePreUnionSQL rebuilds the pre-#11441 stored_range predicate from
// a rewritten single-scan range statement: the complementary UNION ALL folds
// back into `WHERE <lower>((SELECT is_last ...) OR <row> <= <bound>)`. The
// reconstruction is positional and self-validating — it fails unless the
// fresh statement holds exactly one stored_range spine, both arms share the
// same lower bound, and the fold removes exactly one UNION ALL — so the
// equivalence proof below compares against the true old text, not a
// hand-written proxy.
func storedRangePreUnionSQL(t *testing.T, fresh string) string {
	t.Helper()
	if strings.Count(fresh, storedRangeUnionSpine) != 1 {
		t.Fatalf("single-scan statement holds %d stored_range UNION ALL spines, want 1",
			strings.Count(fresh, storedRangeUnionSpine))
	}
	head, tail, _ := strings.Cut(fresh, storedRangeUnionSpine)
	const armGate = "(SELECT is_last FROM range_bounds)"
	if !strings.HasSuffix(head, armGate) {
		t.Fatalf("unbounded stored_range arm does not end at the is_last gate")
	}
	lower := head[strings.LastIndex(head, "WHERE ")+len("WHERE ") : len(head)-len(armGate)]
	const boundedGate = "NOT (SELECT is_last FROM range_bounds)\n      AND "
	if !strings.HasPrefix(tail, lower+boundedGate) {
		t.Fatalf("bounded stored_range arm does not share the %q lower bound", lower)
	}
	rest := tail[len(lower)+len(boundedGate):]
	old := head[:strings.LastIndex(head, "WHERE ")] + "WHERE " + lower +
		"((SELECT is_last FROM range_bounds) OR " + rest
	const storedClose = "\n),\nrange_diff AS ("
	if strings.Count(old, storedClose) != 1 {
		t.Fatalf("folded statement holds %d stored_range closes, want 1", strings.Count(old, storedClose))
	}
	old = strings.Replace(old, storedClose, ")\n),\nrange_diff AS (", 1)
	if strings.Contains(old, "NOT (SELECT is_last") {
		t.Fatalf("folded statement keeps a NOT is_last arm")
	}
	if got, want := strings.Count(old, "UNION ALL"), strings.Count(fresh, "UNION ALL")-1; got != want {
		t.Fatalf("folded statement holds %d UNION ALLs, want one fewer than the fresh %d", got, want+1)
	}
	return old
}

// rangeCellText renders one result cell deterministically, keeping the
// driver type visible so a representation change reads as a mismatch.
func rangeCellText(v any) string {
	switch c := v.(type) {
	case nil:
		return "\x00NULL"
	case []byte:
		return "B:" + string(c)
	case time.Time:
		return "T:" + c.UTC().Format(time.RFC3339Nano)
	default:
		return fmt.Sprintf("V:%v", c)
	}
}

// collectRangeRows runs one range statement and returns its full row
// multiset as sorted deterministic lines: byte-identical output compares
// equal, and any added, dropped, or altered row (or arm duplication) fails
// the comparison.
func collectRangeRows(t *testing.T, h *keyedHarness, stmt string, args ...any) []string {
	t.Helper()
	rows, err := h.db.QueryContext(h.ctx, stmt, args...)
	if err != nil {
		t.Fatalf("range read: %v", err)
	}
	defer func() { _ = rows.Close() }()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatalf("range columns: %v", err)
	}
	var out []string
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatalf("range scan: %v", err)
		}
		cells := make([]string, len(cols))
		for i, v := range vals {
			cells[i] = rangeCellText(v)
		}
		out = append(out, strings.Join(cells, "\x1f"))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("range rows: %v", err)
	}
	sort.Strings(out)
	return out
}

// requireStoredRangeEquivalent runs the rewritten statement, its pre-UNION
// ALL fold, and its UNION (deduped-arm) variant over identical arguments and
// demands byte-identical multisets. The old-versus-new leg proves the
// rewrite preserves every row; the UNION ALL-versus-UNION leg proves the
// arms are disjoint (each arm reads winners rows once through the primary
// key, so a UNION/UNION ALL divergence is arm overlap, and any mismatch in
// either leg fails the proof).
func requireStoredRangeEquivalent(t *testing.T, h *keyedHarness, first, next string, lo *postgres.WinnersAuditRangeIdent, budget int) {
	t.Helper()
	stmt, args := first, []any{budget}
	if lo != nil {
		stmt = next
		args = append(args, lo.PackageID, lo.RepositoryID, lo.SubjectDigest, lo.C0, lo.AdvisoryID)
	}
	old := storedRangePreUnionSQL(t, stmt)
	dedup := strings.Replace(stmt, storedRangeUnionSpine, `
    UNION
    SELECT w.*
    FROM supply_chain_impact_canonical_winners AS w
    WHERE `, 1)
	want := collectRangeRows(t, h, old, args...)
	got := collectRangeRows(t, h, stmt, args...)
	if strings.Join(want, "\n") != strings.Join(got, "\n") {
		t.Errorf("budget %d lo=%+v: folded statement returns %d rows, rewritten %d rows", budget, lo, len(want), len(got))
	}
	same := collectRangeRows(t, h, dedup, args...)
	if strings.Join(got, "\n") != strings.Join(same, "\n") {
		t.Errorf("budget %d lo=%+v: UNION ALL returns %d rows, UNION %d rows: the stored_range arms overlap", budget, lo, len(got), len(same))
	}
	t.Logf("budget %d lo=%+v: %d rows byte-identical across folded, UNION ALL, and UNION", budget, lo, len(got))
}

// TestSupplyChainImpactWinnersStoredRangeUnionAllEquivalenceLive is the
// #11441 verdict-B gate (3): the rewritten stored_range predicate returns
// byte-identical row multisets to the old OR predicate on a last range
// (P=true), a selective mid-sweep range and an unselective mid-sweep range
// (P=false), an empty winners table, and a NULL-bound range, for both the
// first- and next-range renderings, with disjoint arms throughout.
func TestSupplyChainImpactWinnersStoredRangeUnionAllEquivalenceLive(t *testing.T) {
	first, next := postgres.WinnersAuditSingleScanFirstRangeSQLForTest, postgres.WinnersAuditSingleScanNextRangeSQLForTest

	t.Run("selective", func(t *testing.T) {
		h := newKeyedHarness(t, "#11441 stored_range selective")
		auditRangeSeed(t, h, auditRangeFixtureIdentifiers)
		auditRangePerturb(t, h)
		requireStoredRangeEquivalent(t, h, first, next, nil, 7)
	})

	t.Run("unselective", func(t *testing.T) {
		h := newKeyedHarness(t, "#11441 stored_range unselective")
		auditRangeSeed(t, h, auditRangeFixtureIdentifiers)
		auditRangePerturb(t, h)
		// One minimal stored row: the mid-sweep bound of live facts sits
		// above the empty key under every collation, so the bounded arm is
		// non-selective while is_last stays false.
		h.exec(`DELETE FROM supply_chain_impact_canonical_winners`)
		h.exec(`INSERT INTO supply_chain_impact_canonical_winners
  (canonical_key, winner_fact_id, finding_id, package_id, repository_id, subject_digest, cve_id, advisory_id, materialized_at)
VALUES ('unselective|one', 'gone:one', 'finding:one', '', '', '', '', '', now())`)
		requireStoredRangeEquivalent(t, h, first, next, nil, 7)
	})

	t.Run("last", func(t *testing.T) {
		h := newKeyedHarness(t, "#11441 stored_range last")
		auditRangeSeed(t, h, auditRangeFixtureIdentifiers)
		auditRangePerturb(t, h)
		requireStoredRangeEquivalent(t, h, first, next, nil, 100000)
	})

	t.Run("empty winners", func(t *testing.T) {
		h := newKeyedHarness(t, "#11441 stored_range empty winners")
		auditRangeSeed(t, h, auditRangeFixtureIdentifiers)
		auditRangePerturb(t, h)
		h.exec(`DELETE FROM supply_chain_impact_canonical_winners`)
		requireStoredRangeEquivalent(t, h, first, next, nil, 7)
	})

	t.Run("null bound", func(t *testing.T) {
		h := newKeyedHarness(t, "#11441 stored_range null bound")
		auditRangeSeed(t, h, auditRangeFixtureIdentifiers)
		auditRangePerturb(t, h)
		// No finding facts: range_raw is empty, so is_last is true and every
		// hi_* bound is NULL while the winners table stays populated.
		h.exec(`DELETE FROM fact_records WHERE fact_kind = $1`, facts.ReducerSupplyChainImpactFindingFactKind)
		requireStoredRangeEquivalent(t, h, first, next, nil, 7)
	})

	t.Run("next range", func(t *testing.T) {
		h := newKeyedHarness(t, "#11441 stored_range next range")
		auditRangeSeed(t, h, auditRangeFixtureIdentifiers)
		auditRangePerturb(t, h)
		reader := postgres.SQLDB{DB: h.db}
		rng, err := postgres.ReadWinnersAuditRangeWithSQLForTest(h.ctx, reader, nil, 7, first, next)
		if err != nil {
			t.Fatalf("open first range: %v", err)
		}
		if rng.Last || rng.Hi == nil {
			t.Fatalf("first range at budget 7 already ends the sweep: %+v", rng)
		}
		requireStoredRangeEquivalent(t, h, first, next, rng.Hi, 7)
	})

	t.Run("sweep", func(t *testing.T) {
		h := newKeyedHarness(t, "#11441 stored_range sweep")
		auditRangeSeed(t, h, auditRangeFixtureIdentifiers)
		auditRangePerturb(t, h)
		reader := postgres.SQLDB{DB: h.db}
		oldFirst, oldNext := storedRangePreUnionSQL(t, first), storedRangePreUnionSQL(t, next)
		for _, budget := range []int{1, 7, 200} {
			want := sweepSnapshotAll(t, h.ctx, reader, budget, oldFirst, oldNext)
			got := sweepSnapshotAll(t, h.ctx, reader, budget, first, next)
			requireSnapshotsEqual(t, budget, want, got)
		}
	})
}
