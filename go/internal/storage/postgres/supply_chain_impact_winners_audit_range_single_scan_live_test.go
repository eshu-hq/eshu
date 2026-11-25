// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"encoding/json"
	"regexp"
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

// explainRangeFirstText runs EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) over the
// first-range statement at budget and returns the raw plan text. It is the
// one seam every first-range plan proof reads through, so a plan-shape
// assertion covers the rendered statement itself instead of a hand-written
// proxy.
func explainRangeFirstText(t *testing.T, h *keyedHarness, stmt string, budget int) string {
	t.Helper()
	rows, err := h.db.QueryContext(h.ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+stmt, budget)
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

// planNodeMatches reports whether the plan tree rooted at node holds a node
// of the given type on the given relation (an empty relation matches any).
func planNodeMatches(node map[string]any, nodeType, relation string) bool {
	if node["Node Type"] == nodeType && (relation == "" || node["Relation Name"] == relation) {
		return true
	}
	kids, _ := node["Plans"].([]any)
	for _, kid := range kids {
		if child, ok := kid.(map[string]any); ok && planNodeMatches(child, nodeType, relation) {
			return true
		}
	}
	return false
}

// planIndexScansOn reports whether the plan tree scans relation through the
// named index: an Index Scan node carrying that Index Name on the relation.
func planIndexScansOn(node map[string]any, index, relation string) bool {
	if node["Node Type"] == "Index Scan" && node["Relation Name"] == relation && node["Index Name"] == index {
		return true
	}
	kids, _ := node["Plans"].([]any)
	for _, kid := range kids {
		if child, ok := kid.(map[string]any); ok && planIndexScansOn(child, index, relation) {
			return true
		}
	}
	return false
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
// plan rides an index scan on the new index instead of sequentially scanning
// the winners table.
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
	auditRangeSeed(t, h, auditRangeFixtureIdentifiers)
	h.exec(`ANALYZE supply_chain_impact_canonical_winners`)
	// The EXPLAINed text IS the rendered single-scan first-range statement at
	// a mid-size budget, read through the explainRangeFirst seam, so the pin
	// covers the range's own stored-side access. A hand-written
	// scalar-predicate proxy could pass while the rendered statement
	// sequentially scans the winners table.
	renderedText := explainRangeFirstText(t, h, postgres.WinnersAuditSingleScanFirstRangeSQLForTest, 200)
	rendered := decodeExplainPlan(t, renderedText)
	t.Logf("rendered first-range plan:\n%s", renderedText)
	if !planIndexScansOn(rendered, indexName, "supply_chain_impact_canonical_winners") {
		t.Errorf("rendered first range does not index-scan %s:\n%s", indexName, renderedText)
	}
	if planNodeMatches(rendered, "Seq Scan", "supply_chain_impact_canonical_winners") {
		t.Errorf("rendered first range sequentially scans supply_chain_impact_canonical_winners:\n%s", renderedText)
	}
}
