// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"strings"
	"testing"
)

// TestScopedSearchWorkBoundGuardLive is the #7930 work-bound guard: the ten
// shipped cells (S1-S7 single-scope via searchFileContentPage, M1-M3
// multi-scope via searchFileContentInRepos) in both plan regimes (custom
// default PREPARE/EXECUTE and force_generic_plan), each under warm-cache
// EXPLAIN (ANALYZE, BUFFERS) caps of 400 shared-hit buffers with zero reads,
// 100 exact heap blocks, and removed rows within max(200, 10x(limit+offset)).
// Only nodes with Actual Loops > 0 count; never-executed branches are
// reported, not failed. There is no wall-time cap at this rung. Shipped cells
// over the caps fail here: that RED is the entry condition for the probes.
func TestScopedSearchWorkBoundGuardLive(t *testing.T) {
	ctx, db := openScopedWorkboundFixture(t)
	reader := NewContentReader(db)

	for _, cell := range scopedWorkboundCells() {
		// Warm the cache through the serving path before measuring it, so
		// the reads=0 bound tests steady state rather than first touch.
		warmScopedCell(t, ctx, reader, cell)
		query, args := shippedScopedQuery(cell)
		for _, regime := range []struct {
			name    string
			generic bool
		}{
			{name: "custom", generic: false},
			{name: "generic", generic: true},
		} {
			stats := explainScopedWork(t, ctx, db, query, args, regime.generic)
			checkScopedWorkBounds(t, cell.name+"/"+regime.name, regime.name, stats, cell.limit, cell.offset)
		}
	}
}

// warmScopedCell runs the cell once through the serving reader. It fails the
// test on error so a broken serving path cannot masquerade as a work-bound
// pass with zero rows.
func warmScopedCell(t *testing.T, ctx context.Context, reader *ContentReader, cell scopedCell) {
	t.Helper()
	var err error
	if cell.multi {
		_, err = reader.searchFileContentInRepos(ctx, cell.repoIDs, cell.pattern, cell.limit, cell.offset)
	} else {
		_, err = reader.searchFileContentPage(ctx, cell.repoIDs[0], cell.pattern, cell.limit, cell.offset)
	}
	if err != nil {
		t.Fatalf("warm cell %q: %v", cell.name, err)
	}
}

// TestScopedSearchShippedDerivationGuard pins, without a database, the stable
// boundaries the live differential derives from: both shipped WHERE fragments
// share one identical content predicate, the multi fragment differs only in
// its repository predicate, and the assembled statements keep the shipped
// SELECT, key ordering, and bound LIMIT/OFFSET shape.
func TestScopedSearchShippedDerivationGuard(t *testing.T) {
	t.Parallel()

	const contentPredicate = "content ILIKE '%' || $2 || '%'"
	single := fileContentSingleScopeWhere(1, 2)
	multi := fileContentMultiScopeWhere(1, 2)
	if !strings.Contains(single, contentPredicate) || !strings.Contains(multi, contentPredicate) {
		t.Fatalf("shipped WHERE fragments lost the shared content predicate:\nsingle: %s\nmulti:  %s", single, multi)
	}
	if single != "repo_id = $1 AND "+contentPredicate {
		t.Fatalf("single-scope WHERE drifted: %s", single)
	}
	if !strings.HasPrefix(multi, "repo_id = ANY(string_to_array($1, E'\\x1f')) AND ") {
		t.Fatalf("multi-scope WHERE lost its repository predicate: %s", multi)
	}
	for name, query := range map[string]string{
		"single": fileContentScopedQuery(single, 3, 4),
		"multi":  fileContentScopedQuery(multi, 3, 4),
	} {
		for _, part := range []string{
			"FROM content_files", "ORDER BY repo_id, relative_path", "LIMIT $3 OFFSET $4",
		} {
			if !strings.Contains(query, part) {
				t.Errorf("%s statement lost %q:\n%s", name, part, query)
			}
		}
	}
}

// TestScopedSearchCustomGenericEquivalenceLive is the #7930 equivalence
// oracle: for every guard cell, the custom-plan and generic-plan executions of
// the shipped statement return byte-identical ordered 8-field rows with bound
// prepared params, and the serving reader agrees with them. The expected rows
// always come from the database itself (never from hand lists), and DB
// ORDER BY is the ordering oracle. The edge matrix covers unicode, % and _
// patterns, offsets 0/10, limit+1 lookahead trim, zero limit, an exhausted
// offset, and empty/duplicate/multi grants.
func TestScopedSearchCustomGenericEquivalenceLive(t *testing.T) {
	ctx, db := openScopedWorkboundFixture(t)
	reader := NewContentReader(db)

	for _, cell := range scopedWorkboundCells() {
		query, args := shippedScopedQuery(cell)
		customRows := selectScopedRows(t, ctx, db, query, args, false)
		genericRows := selectScopedRows(t, ctx, db, query, args, true)
		if strings.Join(customRows, "\n") != strings.Join(genericRows, "\n") {
			t.Errorf("cell %q: custom (%d rows) != generic (%d rows)", cell.name, len(customRows), len(genericRows))
		}
		var readerRows []string
		var err error
		if cell.multi {
			var files []FileContent
			files, err = reader.searchFileContentInRepos(ctx, cell.repoIDs, cell.pattern, cell.limit, cell.offset)
			readerRows = fingerprintFileContents(files)
		} else {
			var files []FileContent
			files, err = reader.searchFileContentPage(ctx, cell.repoIDs[0], cell.pattern, cell.limit, cell.offset)
			readerRows = fingerprintFileContents(files)
		}
		if err != nil {
			t.Fatalf("cell %q: reader error = %v", cell.name, err)
		}
		if strings.Join(readerRows, "\n") != strings.Join(customRows, "\n") {
			t.Errorf("cell %q: reader (%d rows) != custom oracle (%d rows)", cell.name, len(readerRows), len(customRows))
		}
	}

	assertOracle := func(name, repoID string, repoIDs []string, pattern string, limit, offset int) []string {
		t.Helper()
		var cell scopedCell
		if repoIDs != nil {
			cell = scopedCell{name: name, multi: true, repoIDs: repoIDs, pattern: pattern, limit: limit, offset: offset}
		} else {
			cell = scopedCell{name: name, repoIDs: []string{repoID}, pattern: pattern, limit: limit, offset: offset}
		}
		query, args := shippedScopedQuery(cell)
		customRows := selectScopedRows(t, ctx, db, query, args, false)
		genericRows := selectScopedRows(t, ctx, db, query, args, true)
		if strings.Join(customRows, "\n") != strings.Join(genericRows, "\n") {
			t.Errorf("edge %q: custom (%d rows) != generic (%d rows)", name, len(customRows), len(genericRows))
		}
		return customRows
	}
	assertReader := func(name string, files []FileContent, err error, want []string) {
		t.Helper()
		if err != nil {
			t.Fatalf("edge %q: reader error = %v", name, err)
		}
		if strings.Join(fingerprintFileContents(files), "\n") != strings.Join(want, "\n") {
			t.Errorf("edge %q: reader (%d rows) != oracle (%d rows)", name, len(files), len(want))
		}
	}

	// Case-insensitive substring: upper-case pattern matches lower-case rows.
	lower := assertOracle("upper/lower oracle", scopedBigRepo, nil, scopedMediumToken, 10, 0)
	files, err := reader.searchFileContentPage(ctx, scopedBigRepo, strings.ToUpper(scopedMediumToken), 10, 0)
	assertReader("upper pattern", files, err, lower)

	// A literal percent in the pattern is an ILIKE wildcard in the shipped
	// semantics; both regimes and the reader must still agree byte for byte.
	percentWant := assertOracle("percent oracle", "scope-repo-00", nil, "100%", 10, 0)
	if len(percentWant) == 0 {
		t.Errorf("percent edge: oracle returned no rows, fixture lost its 100%%-bearing content")
	}
	files, err = reader.searchFileContentPage(ctx, "scope-repo-00", "100%", 10, 0)
	assertReader("percent pattern", files, err, percentWant)

	// Limit+1 lookahead trim: one extra oracle row trimmed to the page equals
	// the served page, for both single and multi shapes.
	lookSingle := assertOracle("lookahead oracle", scopedBigRepo, nil, scopedDenseToken, 11, 0)
	trimmed := lookSingle
	if len(trimmed) > 10 {
		trimmed = trimmed[:10]
	}
	files, err = reader.searchFileContentPage(ctx, scopedBigRepo, scopedDenseToken, 10, 0)
	assertReader("lookahead single trim", files, err, trimmed)

	lookMulti := assertOracle("lookahead multi oracle", "", []string{"scope-repo-00", "scope-repo-01", "scope-repo-02"}, scopedDenseToken, 11, 0)
	trimmedMulti := lookMulti
	if len(trimmedMulti) > 10 {
		trimmedMulti = trimmedMulti[:10]
	}
	mfiles, merr := reader.searchFileContentInRepos(ctx, []string{"scope-repo-00", "scope-repo-01", "scope-repo-02"}, scopedDenseToken, 10, 0)
	assertReader("lookahead multi trim", mfiles, merr, trimmedMulti)

	// Zero limit and an exhausted offset both serve empty pages exactly.
	zeroWant := assertOracle("zero limit oracle", scopedBigRepo, nil, scopedDenseToken, 0, 0)
	if len(zeroWant) != 0 {
		t.Errorf("zero limit oracle = %d rows, want 0", len(zeroWant))
	}
	files, err = reader.searchFileContentPage(ctx, scopedBigRepo, scopedDenseToken, 0, 0)
	assertReader("zero limit", files, err, zeroWant)

	exhaustedWant := assertOracle("exhausted offset oracle", scopedBigRepo, nil, scopedDenseToken, 10, 100000)
	if len(exhaustedWant) != 0 {
		t.Errorf("exhausted offset oracle = %d rows, want 0", len(exhaustedWant))
	}

	// Grants: an empty grant reads nothing, duplicate grants match the
	// deduplicated grant, and grant order never changes the key-ordered page.
	emptyWant := assertOracle("empty grant oracle", "", []string{}, scopedDenseToken, 10, 0)
	if len(emptyWant) != 0 {
		t.Errorf("empty grant oracle = %d rows, want 0", len(emptyWant))
	}
	mfiles, merr = reader.searchFileContentInRepos(ctx, []string{}, scopedDenseToken, 10, 0)
	assertReader("empty grant", mfiles, merr, emptyWant)

	deduped := assertOracle("deduped grant oracle", "", []string{"scope-repo-02", "scope-repo-03"}, scopedDenseToken, 10, 10)
	mfiles, merr = reader.searchFileContentInRepos(ctx, []string{"scope-repo-02", "scope-repo-02", "scope-repo-03", "scope-repo-03"}, scopedDenseToken, 10, 10)
	assertReader("duplicate grant", mfiles, merr, deduped)

	reordered := assertOracle("reordered grant oracle", "", []string{"scope-repo-01", "scope-repo-00", "scope-repo-02"}, scopedDenseToken, 10, 0)
	mfiles, merr = reader.searchFileContentInRepos(ctx, []string{"scope-repo-00", "scope-repo-01", "scope-repo-02"}, scopedDenseToken, 10, 0)
	assertReader("grant order", mfiles, merr, reordered)
}
