// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// Guard for #8283: the deployable_unit_correlation input fingerprint counts the
// resolved rows of each relationship generation (backfill.InputFingerprintExprSQL).
// That count catches rows the cross-repo resolver adds under an already-active
// generation before its activation re-stamps activated_at (resolution inserts
// and activation are separate statements, cross_repo_resolution.go steps 4 and
// 6). It is a sound change signal only while resolved_relationships is
// append-only: the one writer is the ON CONFLICT DO NOTHING insert, rows are
// never updated, and nothing can remove a row but dropping a whole generation's
// table. A statement that updates or deletes a row can change content, or take
// a row away and add another, with the count unchanged, and the item would then
// be skipped on a stale result.
//
// The sources are enumerated from the module, so a later writer is held to the
// rule without anyone editing this test. A deliberate change of that rule must
// change the fingerprint contract first and then this guard.

var (
	resolvedRowMutationPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?is)\bUPDATE\s+(?:ONLY\s+)?(?:\w+\.)?resolved_relationships\b`),
		regexp.MustCompile(`(?is)\bDELETE\s+FROM\s+(?:ONLY\s+)?(?:\w+\.)?resolved_relationships\b`),
		regexp.MustCompile(`(?is)\bTRUNCATE\b[^;]*?\bresolved_relationships\b`),
		regexp.MustCompile(`(?is)\bDROP\s+TABLE\b[^;]*?\bresolved_relationships\b`),
		regexp.MustCompile(`(?is)\bCREATE\s+(?:OR\s+REPLACE\s+)?(?:CONSTRAINT\s+)?TRIGGER\b[^;]*?\bON\s+(?:\w+\.)?resolved_relationships\b`),
		regexp.MustCompile(`(?is)\bALTER\s+TABLE\s+(?:IF\s+EXISTS\s+)?(?:ONLY\s+)?(?:\w+\.)?resolved_relationships\b[^;]*?\b(?:REFERENCES|FOREIGN\s+KEY)\b`),
		regexp.MustCompile(`(?is)\bCREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?(?:\w+\.)?resolved_relationships\b[^;]*?\bREFERENCES\b`),
		regexp.MustCompile(`(?is)\bCREATE\s+RULE\b[^;]*?\bON\s+(?:\w+\.)?resolved_relationships\b`),
	}
	goLineComment  = regexp.MustCompile(`(?m)//[^\n]*`)
	sqlLineComment = regexp.MustCompile(`(?m)--[^\n]*`)
)

// scanResolvedRowMutations reports every place a source set can change or
// remove a resolved_relationships row. files maps a display name to the source
// text; names ending in .sql are scanned as SQL, others as Go. Comments are
// stripped first so prose cannot trip or hide a finding.
func scanResolvedRowMutations(files map[string]string) []string {
	var violations []string
	for name, src := range files {
		if strings.HasSuffix(name, ".sql") {
			src = sqlLineComment.ReplaceAllString(src, "")
		} else {
			src = goLineComment.ReplaceAllString(src, "")
		}
		for _, pattern := range resolvedRowMutationPatterns {
			if loc := pattern.FindStringIndex(src); loc != nil {
				excerpt := strings.Join(strings.Fields(src[loc[0]:minInt(loc[1]+40, len(src))]), " ")
				violations = append(violations, name+": "+excerpt)
			}
		}
	}
	sort.Strings(violations)
	return violations
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// resolvedRelationshipsSources reads every non-test Go file under go/ and every
// .sql file under the migrations and schema trees that mentions the table.
func resolvedRelationshipsSources(t *testing.T) map[string]string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	goRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
	roots := []string{goRoot, filepath.Join(goRoot, "..", "schema")}
	files := map[string]string{}
	for _, root := range roots {
		if _, err := os.Stat(root); err != nil {
			continue
		}
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if name := entry.Name(); name == "vendor" || name == "testdata" || strings.HasPrefix(name, ".") {
					return filepath.SkipDir
				}
				return nil
			}
			isGo := strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go")
			if !isGo && !strings.HasSuffix(path, ".sql") {
				return nil
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if strings.Contains(string(src), "resolved_relationships") {
				files[path] = string(src)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	return files
}

// TestResolvedRelationshipsStayAppendOnly fails when any non-test Go source or
// migration can update, delete, truncate, or cascade-remove a
// resolved_relationships row. It also fails if the scan finds no source that
// writes the table, because an empty scan would pass for the wrong reason.
func TestResolvedRelationshipsStayAppendOnly(t *testing.T) {
	t.Parallel()

	files := resolvedRelationshipsSources(t)
	sawInsert := false
	for _, src := range files {
		if regexp.MustCompile(`(?is)INSERT\s+INTO\s+resolved_relationships\b`).MatchString(src) {
			sawInsert = true
		}
	}
	if !sawInsert {
		t.Fatalf("no source inserts into resolved_relationships among %d scanned files; the scan is broken, not the code", len(files))
	}
	if violations := scanResolvedRowMutations(files); len(violations) > 0 {
		t.Fatalf("resolved_relationships is no longer append-only; the deployable_unit_correlation input fingerprint "+
			"(row counts per generation, #8283) would miss the change. Update the fingerprint contract "+
			"(backfill.InputFingerprintExprSQL) before adding this:\n  %s", strings.Join(violations, "\n  "))
	}
}

// TestResolvedRelationshipsAppendOnlyGuardBitesOnPlantedWriters is the seeded
// RED/GREEN pair for the guard: every planted mutation fails it, and the clean
// insert passes.
func TestResolvedRelationshipsAppendOnlyGuardBitesOnPlantedWriters(t *testing.T) {
	t.Parallel()

	clean := map[string]string{
		"insert.go": "package x\nconst q = `INSERT INTO resolved_relationships (resolved_id) VALUES ($1) ON CONFLICT DO NOTHING`\n" +
			"// DELETE FROM resolved_relationships is documented here and must not count\n",
		"010.sql": "-- UPDATE resolved_relationships in a comment\nCREATE TABLE IF NOT EXISTS resolved_relationships (resolved_id TEXT PRIMARY KEY, generation_id TEXT NOT NULL);\n",
	}
	if got := scanResolvedRowMutations(clean); len(got) != 0 {
		t.Fatalf("clean sources flagged: %v", got)
	}
	for name, planted := range map[string]string{
		"update.go":     "package x\nconst q = `UPDATE resolved_relationships SET confidence = 1 WHERE generation_id = $1`\n",
		"delete.go":     "package x\nconst q = `DELETE FROM resolved_relationships WHERE generation_id = $1`\n",
		"truncate.sql":  "TRUNCATE TABLE resolved_relationships;\n",
		"drop.sql":      "DROP TABLE IF EXISTS resolved_relationships;\n",
		"trigger.sql":   "CREATE TRIGGER t AFTER INSERT ON resolved_relationships FOR EACH ROW EXECUTE FUNCTION f();\n",
		"fk_alter.sql":  "ALTER TABLE resolved_relationships ADD CONSTRAINT fk FOREIGN KEY (generation_id) REFERENCES relationship_generations(generation_id) ON DELETE CASCADE;\n",
		"fk_create.sql": "CREATE TABLE resolved_relationships (resolved_id TEXT PRIMARY KEY, generation_id TEXT REFERENCES relationship_generations(generation_id) ON DELETE CASCADE);\n",
	} {
		got := scanResolvedRowMutations(map[string]string{name: planted})
		if len(got) == 0 {
			t.Errorf("planted %s was not flagged", name)
		}
	}
}
