// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// contentFileGuardExcluded names the one column the conflict clause writes but
// deliberately leaves out of its guard: indexed_at moves on every write, so
// comparing it would stop the guard from ever skipping (#8915).
var contentFileGuardExcluded = []string{"indexed_at"}

// contentFileSeparateCompare is the column the guard compares outside the
// tuples, as its own `OR content IS DISTINCT FROM EXCLUDED.content` leg.
const contentFileSeparateCompare = "content"

// contentFileKeyColumns are the conflict target; they are inserted but never
// updated.
var contentFileKeyColumns = []string{"repo_id", "relative_path"}

var (
	setClausePattern    = regexp.MustCompile(`(?is)\bDO\s+UPDATE\s+SET\s+(.*?)\s+WHERE\s`)
	setItemPattern      = regexp.MustCompile(`(?i)^(\w+)\s*=\s*EXCLUDED\.(\w+)$`)
	guardTuplesPattern  = regexp.MustCompile(`(?is)\bWHERE\s*\((.*?)\)\s*IS\s+DISTINCT\s+FROM\s*\((.*?)\)\s*OR\s+content_files\.content\s+IS\s+DISTINCT\s+FROM\s+EXCLUDED\.content\b`)
	insertColumnPattern = regexp.MustCompile(`(?is)INSERT\s+INTO\s+content_files\s*\((.*?)\)`)
)

func splitColumns(list string) []string {
	var out []string
	for _, item := range strings.Split(list, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

// contentFileClauseDrift returns every way the conflict clause's SET list, its
// guard tuples, and every INSERT column list disagree. The SET columns minus the
// explicit exclusion and the separately compared body must equal both guard
// tuples in order, each tuple side
// must name its own table's column, the content body must be compared on its
// own, and the columns of each INSERT statement (the single-row query and the batch
// prefix) minus the key must equal the SET columns, so a column added to an
// insert without the update (or the guard) is caught.
func contentFileClauseDrift(clause string, insertStatements ...string) []string {
	var drift []string
	setMatch := setClausePattern.FindStringSubmatch(clause)
	if setMatch == nil {
		return []string{"no DO UPDATE SET ... WHERE clause found"}
	}
	var setColumns []string
	for _, item := range splitColumns(setMatch[1]) {
		parts := setItemPattern.FindStringSubmatch(item)
		if parts == nil || parts[1] != parts[2] {
			drift = append(drift, fmt.Sprintf("SET item %q is not `col = EXCLUDED.col`", item))
			continue
		}
		setColumns = append(setColumns, parts[1])
	}
	var wantGuard []string
	for _, column := range setColumns {
		if !slices.Contains(contentFileGuardExcluded, column) && column != contentFileSeparateCompare {
			wantGuard = append(wantGuard, column)
		}
	}
	guard := guardTuplesPattern.FindStringSubmatch(clause)
	if guard == nil {
		return append(drift, "guard `WHERE (tuple) IS DISTINCT FROM (tuple) OR content IS DISTINCT FROM EXCLUDED.content` not found")
	}
	for _, side := range []struct{ name, prefix, list string }{
		{"stored", "content_files.", guard[1]},
		{"incoming", "EXCLUDED.", guard[2]},
	} {
		var got []string
		for _, item := range splitColumns(side.list) {
			if !strings.HasPrefix(item, side.prefix) {
				drift = append(drift, fmt.Sprintf("%s guard item %q lacks the %s prefix", side.name, item, side.prefix))
			}
			got = append(got, strings.TrimPrefix(item, side.prefix))
		}
		if !slices.Equal(got, wantGuard) {
			drift = append(drift, fmt.Sprintf("%s guard tuple %v != SET columns minus %v and %s = %v", side.name, got, contentFileGuardExcluded, contentFileSeparateCompare, wantGuard))
		}
	}
	for i, statement := range insertStatements {
		insertMatch := insertColumnPattern.FindStringSubmatch(statement)
		if insertMatch == nil {
			drift = append(drift, fmt.Sprintf("insert statement %d: no INSERT INTO content_files column list found", i))
			continue
		}
		var wantSet []string
		for _, column := range splitColumns(insertMatch[1]) {
			if !slices.Contains(contentFileKeyColumns, column) {
				wantSet = append(wantSet, column)
			}
		}
		gotSet, wantSorted := slices.Clone(setColumns), slices.Clone(wantSet)
		slices.Sort(gotSet)
		slices.Sort(wantSorted)
		if !slices.Equal(gotSet, wantSorted) {
			drift = append(drift, fmt.Sprintf("SET columns %v != insert statement %d columns minus key %v", gotSet, i, wantSorted))
		}
	}
	return drift
}

// TestUpsertContentFileConflictClauseLockstep fails when the shipped clause's
// SET list, guard tuples, and insert columns drift apart: a column that is
// written but not compared would keep a stale value silently.
func TestUpsertContentFileConflictClauseLockstep(t *testing.T) {
	if drift := contentFileClauseDrift(upsertContentFileConflictClause, upsertContentFileQuery, upsertContentFileBatchPrefix); len(drift) > 0 {
		t.Fatalf("content_files conflict clause drifted:\n%s", strings.Join(drift, "\n"))
	}
	// Both statements must end in the shared clause, not a copy of it.
	if !strings.HasSuffix(upsertContentFileQuery, upsertContentFileConflictClause) ||
		upsertContentFileBatchSuffix != upsertContentFileConflictClause {
		t.Fatal("a content_files upsert statement does not use the shared conflict clause")
	}
}

// TestContentFileClauseDriftSeededViolations is the RED/GREEN pair for the
// lockstep check: a column added to one side only is reported, in either
// INSERT statement, and the clean clause is not.
func TestContentFileClauseDriftSeededViolations(t *testing.T) {
	t.Parallel()
	const insertBase = "INSERT INTO content_files (repo_id, relative_path, commit_sha, content, indexed_at) VALUES "
	const clean = `
ON CONFLICT (repo_id, relative_path) DO UPDATE
SET commit_sha = EXCLUDED.commit_sha, content = EXCLUDED.content, indexed_at = EXCLUDED.indexed_at
WHERE (content_files.commit_sha) IS DISTINCT FROM (EXCLUDED.commit_sha)
   OR content_files.content IS DISTINCT FROM EXCLUDED.content`
	withLanguage := strings.Replace(insertBase, "content,", "content, language,", 1)
	if drift := contentFileClauseDrift(clean, insertBase, insertBase); len(drift) != 0 {
		t.Fatalf("clean clause reported: %v", drift)
	}
	red := map[string]struct {
		clause string
		single string
		batch  string
	}{
		"column set but not guarded":           {strings.Replace(clean, "SET commit_sha", "SET language = EXCLUDED.language, commit_sha", 1), withLanguage, withLanguage},
		"guard has extra stored col":           {strings.Replace(clean, "(content_files.commit_sha)", "(content_files.commit_sha, content_files.language)", 1), insertBase, insertBase},
		"incoming side missing col":            {strings.Replace(clean, "(EXCLUDED.commit_sha)", "(EXCLUDED.content_hash)", 1), insertBase, insertBase},
		"batch insert column not updated":      {clean, insertBase, withLanguage},
		"single-row insert column not updated": {clean, withLanguage, insertBase},
		"single-row insert lacks a column":     {clean, strings.Replace(insertBase, "commit_sha, ", "", 1), insertBase},
		"content compare removed":              {strings.Replace(clean, "OR content_files.content IS DISTINCT FROM EXCLUDED.content", "", 1), insertBase, insertBase},
		"indexed_at guarded":                   {strings.Replace(strings.Replace(clean, "(content_files.commit_sha)", "(content_files.commit_sha, content_files.indexed_at)", 1), "(EXCLUDED.commit_sha)", "(EXCLUDED.commit_sha, EXCLUDED.indexed_at)", 1), insertBase, insertBase},
		"set item renamed":                     {strings.Replace(clean, "content = EXCLUDED.content", "content = EXCLUDED.commit_sha", 1), insertBase, insertBase},
	}
	for name, tc := range red {
		if len(contentFileClauseDrift(tc.clause, tc.single, tc.batch)) == 0 {
			t.Errorf("%s: not reported", name)
		}
	}
}
