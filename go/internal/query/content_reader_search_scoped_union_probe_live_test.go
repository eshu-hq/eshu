// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
)

// This file holds the two #7930 AUTHORIZED probes. Probe (A) replaces the
// multi-scope ANY statement with a per-repo UNION ALL of single-scope legs
// for the red multi cells. Probe (B) investigates, for still-red rare/empty
// single-scope cells, whether the generic plan already finds a BitmapAnd the
// custom plan misses. Neither probe changes serving shapes: both run
// measurement-only against the disposable fixture, and the kill rule
// (regression vs shipped work-bounds, healthy-path regression, or
// equivalence failure) rejects a probe before it could be adopted.

// dedupeScopedRepos drops duplicate grant entries preserving first-seen
// order. UNION ALL over a duplicated grant would return that repository's
// rows twice, while the shipped ANY matches them once, so the probe must
// dedupe to stay equivalent.
func dedupeScopedRepos(repos []string) []string {
	seen := make(map[string]struct{}, len(repos))
	out := make([]string, 0, len(repos))
	for _, repo := range repos {
		if _, ok := seen[repo]; ok {
			continue
		}
		seen[repo] = struct{}{}
		out = append(out, repo)
	}
	return out
}

// unionAllProbeQuery derives the probe-(A) statement from the shipped
// builders: one unbounded single-scope leg per deduplicated repository (the
// leg body is a byte-prefix truncation of fileContentScopedQuery output at
// the stable ORDER BY boundary, with per-leg repo bind positions sharing one
// pattern bind), joined by UNION ALL under one outer key-order
// LIMIT/OFFSET. It returns the statement and the bound args for one pattern,
// one repo list, one window. No shipped SQL is copied: legs come from the
// builders, and only the UNION ALL joints plus the outer ORDER BY/LIMIT are
// probe-only assembly.
func unionAllProbeQuery(repos []string, patternArg, firstRepoArg, limitArg, offsetArg int) (string, int) {
	deduped := dedupeScopedRepos(repos)
	const orderBoundary = "\n\t\tORDER BY repo_id, relative_path"
	legs := make([]string, 0, len(deduped))
	repoArg := firstRepoArg
	for range deduped {
		full := fileContentScopedQuery(fileContentSingleScopeWhere(repoArg, patternArg), limitArg, offsetArg)
		body, found := strings.CutSuffix(strings.TrimSpace(full), orderBoundary+"\n\t\tLIMIT $"+fmt.Sprint(limitArg)+" OFFSET $"+fmt.Sprint(offsetArg))
		if !found {
			// Fall back to a hard failure downstream: a leg that is not a
			// truncation of the shipped text must never run silently.
			legs = append(legs, "(/* NOT DERIVED FROM SHIPPED TEXT */)")
			continue
		}
		legs = append(legs, "("+strings.TrimSpace(body)+")")
		repoArg++
	}
	outer := fmt.Sprintf(orderBoundary+"\n\t\tLIMIT $%d OFFSET $%d\n\t", limitArg, offsetArg)
	return strings.Join(legs, "\n\t\tUNION ALL ") + outer, repoArg
}

// unionAllProbeArgs binds one probe execution: the shared pattern, one repo
// id per deduplicated leg, then the window.
func unionAllProbeArgs(repos []string, pattern string, limit, offset int) []any {
	deduped := dedupeScopedRepos(repos)
	args := make([]any, 0, len(deduped)+3)
	args = append(args, pattern)
	for _, repo := range deduped {
		args = append(args, repo)
	}
	return append(args, int64(limit), int64(offset))
}

// TestUnionAllProbeDerivationGuard pins, without a database, that the probe
// legs carry the shipped predicate once per repository and nothing else
// hand-written: no string_to_array (the ANY mechanism under test is absent),
// exactly one shared content predicate per leg, and the outer key-order
// window. It also pins grant dedupe.
func TestUnionAllProbeDerivationGuard(t *testing.T) {
	t.Parallel()

	repos := []string{scopedSmallRepo(9), scopedSmallRepo(9), scopedSmallRepo(10)}
	if got := dedupeScopedRepos(repos); len(got) != 2 || got[0] != scopedSmallRepo(9) || got[1] != scopedSmallRepo(10) {
		t.Fatalf("dedupeScopedRepos(%v) = %v, want first-seen order without duplicates", repos, got)
	}
	// Bind layout: $1 pattern, $2/$3 repos, $4 limit, $5 offset.
	query, nextArg := unionAllProbeQuery(repos, 1, 2, 4, 5)
	if nextArg != 4 {
		t.Fatalf("unionAllProbeQuery consumed through $%d, want $3", nextArg-1)
	}
	if strings.Contains(query, "string_to_array") {
		t.Fatalf("probe carries the ANY mechanism under test:\n%s", query)
	}
	if got := strings.Count(query, "content ILIKE '%' || $1 || '%'"); got != 2 {
		t.Fatalf("probe holds %d shared content predicates, want 2 (one per leg):\n%s", got, query)
	}
	if got := strings.Count(query, "UNION ALL"); got != 1 {
		t.Fatalf("probe holds %d UNION ALL joints, want 1:\n%s", got, query)
	}
	if !strings.Contains(query, "ORDER BY repo_id, relative_path") || !strings.Contains(query, "LIMIT $4 OFFSET $5") {
		t.Fatalf("probe lost the outer key-order window:\n%s", query)
	}
	if strings.Contains(query, "NOT DERIVED FROM SHIPPED TEXT") {
		t.Fatalf("probe leg is not a truncation of the shipped builders:\n%s", query)
	}
	for _, leg := range []string{"repo_id = $2 AND", "repo_id = $3 AND"} {
		if !strings.Contains(query, leg) {
			t.Errorf("probe lost per-leg predicate %q:\n%s", leg, query)
		}
	}
}

// probeMultiCells returns the guard's multi-scope cells: the only shapes
// probe (A) may replace.
func probeMultiCells() []scopedCell {
	var out []scopedCell
	for _, cell := range scopedWorkboundCells() {
		if cell.multi {
			out = append(out, cell)
		}
	}
	return out
}

// TestUnionAllProbeWorkAndEquivalenceLive measures probe (A) against the
// shipped multi cells in both regimes: same work caps, custom/generic/reader
// row equivalence, and the kill rule. Equivalence failure or a regression on
// a shipped-green cell fails the test (probe KILLED); a probe that is red
// exactly where shipped is red is a NO-GAIN verdict, recorded but not failed.
func TestUnionAllProbeWorkAndEquivalenceLive(t *testing.T) {
	ctx, db := openScopedWorkboundFixture(t)
	reader := NewContentReader(db)

	for _, cell := range probeMultiCells() {
		// Bind layout mirrors the derivation guard: $1 pattern, $2.. repos,
		// then limit and offset.
		limitArg := len(dedupeScopedRepos(cell.repoIDs)) + 2
		offsetArg := limitArg + 1
		probeQuery, _ := unionAllProbeQuery(cell.repoIDs, 1, 2, limitArg, offsetArg)
		probeArgs := unionAllProbeArgs(cell.repoIDs, cell.pattern, cell.limit, cell.offset)
		shippedQuery, shippedArgs := shippedScopedQuery(cell)

		for _, regime := range []struct {
			name    string
			generic bool
		}{
			{name: "custom", generic: false},
			{name: "generic", generic: true},
		} {
			shippedStats := explainScopedWork(t, ctx, db, shippedQuery, shippedArgs, regime.generic)
			probeStats := explainScopedWork(t, ctx, db, probeQuery, probeArgs, regime.generic)
			t.Logf("probeA cell=%q regime=%s shipped(hit=%d heap=%d removed=%d) probe(hit=%d heap=%d removed=%d)",
				cell.name, regime.name,
				shippedStats.sharedHit, shippedStats.heapExact, shippedStats.removed,
				probeStats.sharedHit, probeStats.heapExact, probeStats.removed)

			shippedViolated := workViolated(shippedStats, cell.limit, cell.offset)
			probeViolated := reportScopedWorkBounds(t, "probeA "+cell.name+"/"+regime.name, regime.name, probeStats, cell.limit, cell.offset)
			if !shippedViolated && probeViolated {
				t.Errorf("KILL probeA cell=%q regime=%s: shipped is within bounds but the probe regresses", cell.name, regime.name)
			}
			if shippedViolated && !probeViolated {
				t.Logf("probeA cell=%q regime=%s: GREEN where shipped is RED (adoption candidate for a later rung; no serving change here)", cell.name, regime.name)
			}
			if shippedViolated && probeViolated {
				t.Logf("probeA cell=%q regime=%s: NO-GAIN (red where shipped is red)", cell.name, regime.name)
			}

			shippedRows := selectScopedRows(t, ctx, db, shippedQuery, shippedArgs, regime.generic)
			probeRows := selectScopedRows(t, ctx, db, probeQuery, probeArgs, regime.generic)
			if strings.Join(shippedRows, "\n") != strings.Join(probeRows, "\n") {
				t.Errorf("KILL probeA cell=%q regime=%s: probe (%d rows) != shipped (%d rows)",
					cell.name, regime.name, len(probeRows), len(shippedRows))
			}
		}

		// Reader agreement: the serving multi path (which always plans under
		// the default custom regime) must equal the custom-regime probe rows.
		probeRows := selectScopedRows(t, ctx, db, probeQuery, probeArgs, false)
		files, err := reader.searchFileContentInRepos(ctx, cell.repoIDs, cell.pattern, cell.limit, cell.offset)
		if err != nil {
			t.Fatalf("probeA cell=%q: reader error = %v", cell.name, err)
		}
		if strings.Join(fingerprintFileContents(files), "\n") != strings.Join(probeRows, "\n") {
			t.Errorf("KILL probeA cell=%q: reader (%d rows) != probe (%d rows)", cell.name, len(files), len(probeRows))
		}
	}
}

// planHasNode reports whether any executed node has the given type,
// optionally restricted to a substring of its index name.
func planHasNode(stats scopedWorkStats, nodeType, indexSubstr string) bool {
	for _, shape := range stats.shapes {
		parts := strings.SplitN(shape, "/", 2)
		if len(parts) != 2 || parts[0] != nodeType {
			continue
		}
		if indexSubstr == "" || strings.Contains(parts[1], indexSubstr) {
			return true
		}
	}
	return false
}

// TestRareEmptySingleScopeBitmapInvestigationLive is probe (B): for still-red
// rare/empty single-scope cells it compares the custom plan against the
// generic plan and asks whether the generic plan already finds a BitmapAnd
// the custom plan misses. Investigation only: whatever the answer, no serving
// shape changes here (steering is REDIRECTED). A cell whose generic plan
// holds no BitmapAnd has nothing to steer toward.
func TestRareEmptySingleScopeBitmapInvestigationLive(t *testing.T) {
	ctx, db := openScopedWorkboundFixture(t)

	// probeBAuthorized restricts probe (B) to its ruling scope: still-red
	// rare/empty single-scope terms. A still-red cell outside that scope
	// (e.g. a medium term) is reported as residual with no authorized probe.
	probeBAuthorized := func(cell scopedCell) bool {
		return !cell.multi && (strings.Contains(cell.name, "rare") || strings.Contains(cell.name, "empty"))
	}
	stillRed := []scopedCell{}
	for _, cell := range scopedWorkboundCells() {
		query, args := shippedScopedQuery(cell)
		custom := explainScopedWork(t, ctx, db, query, args, false)
		if !workViolated(custom, cell.limit, cell.offset) {
			continue
		}
		if !probeBAuthorized(cell) {
			t.Logf("probeB: cell=%q is still red but outside probe (B) scope (rare/empty single-scope only); residual for a later rung", cell.name)
			continue
		}
		stillRed = append(stillRed, cell)
	}
	if len(stillRed) == 0 {
		t.Log("probeB: no still-red rare/empty single-scope cells; investigation not needed")
		return
	}
	for _, cell := range stillRed {
		query, args := shippedScopedQuery(cell)
		custom := explainScopedWork(t, ctx, db, query, args, false)
		generic := explainScopedWork(t, ctx, db, query, args, true)
		customAnd := planHasNode(custom, "BitmapAnd", "")
		genericAnd := planHasNode(generic, "BitmapAnd", "")
		t.Logf("probeB cell=%q custom shapes=%s | generic shapes=%s | BitmapAnd custom=%v generic=%v",
			cell.name, strings.Join(custom.shapes, " "), strings.Join(generic.shapes, " "), customAnd, genericAnd)
		if genericAnd && !customAnd {
			t.Logf("probeB cell=%q: STEER CANDIDATE (generic finds a BitmapAnd custom misses; steering REDIRECTED, no change here)", cell.name)
		} else {
			t.Logf("probeB cell=%q: nothing to steer toward (generic holds no BitmapAnd custom misses)", cell.name)
		}
		assertProbeBReadOnly(t, ctx, db)
	}
}

// assertProbeBReadOnly guards the investigation's read-only contract: probe
// (B) runs EXPLAIN only and must leave the corpus row count untouched.
func assertProbeBReadOnly(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM content_files`).Scan(&count); err != nil {
		t.Fatalf("probeB corpus count: %v", err)
	}
	const want = scopedBigRows + scopedSmallRepoCount*scopedSmallRows
	if count != want {
		t.Fatalf("probeB corpus count = %d, want %d (fixture mutated)", count, want)
	}
}
