// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"go/scanner"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// Guard for #8283: the deployable_unit_correlation input fingerprint is the
// hex sha256 of the sorted resolved_id set the fused corpus-fenced read
// returns (backfill.ResolvedInputDigestSQL over backfill.ResolvedInputRowsSQL
// in go/internal/storage/postgres/backfill/reopen_fingerprint_sql.go). The id
// set is a sound change signal only while resolved_relationships is
// append-only: the one writer is the ON CONFLICT DO NOTHING insert
// (insertResolvedSQL in relationship_schema.go, executed by UpsertResolved in
// relationship_store.go), rows are never updated, and nothing removes a row
// short of dropping a whole generation's table. A statement that updates or
// deletes a row can change content, or take a row away and add another, with
// the id set unchanged, and the item would then be skipped on a stale result.
//
// The sources are enumerated from the module, so a later writer is held to the
// rule without anyone editing this test.
//
// Limits: this is a text scan of SQL literals. A table name assembled at
// runtime (fmt.Sprintf, string concatenation) or a statement built from
// fragments that never spell "resolved_relationships" next to the verb passes it.
// Go comments are removed with the Go scanner, so a "//" inside a literal does
// not hide SQL after it; SQL files strip "--" comments by line, so a "--" inside
// a SQL string literal would hide the rest of that line. SQL /*...*/ block
// comments are stripped from both Go and SQL sources, since a hint between
// the verb and the table sits inside the literal, out of reach of the Go
// scanner. The write
// path has one SQL constant (insertResolvedSQL), so review any new code that
// builds SQL for this table dynamically. A deliberate change of that rule must
// change the fingerprint contract first and then this guard.
//
// Post-#8299 identity note: resolved_id is a pure function of the
// unique-index identity (generation_id, coalesced source and target ids,
// relationship_type) with deliberately no ordinal input
// (ResolvedRelationshipID in go/internal/relationships/models.go), so an
// ordinal shift cannot collide two identities. Rows stored by the pre-#8299
// ordinal digest keep their old ids and are never rewritten —
// insertResolvedSQL is ON CONFLICT DO NOTHING — which reinforces append-only
// rather than weakening it.

// resolvedTable is the table spelled the ways Postgres accepts it: bare,
// schema-qualified (with optional whitespace around the dot, as in
// public . resolved_relationships), and with double-quoted identifiers on
// either part (resolved_relationships, public.resolved_relationships,
// "resolved_relationships", public."resolved_relationships",
// "public"."resolved_relationships"). Every pattern below builds on it.
const resolvedTable = `(?:"?\w+"?\s*\.\s*)?"?resolved_relationships"?\b`

var (
	// Every pattern is bounded by [^;`] so a match stops at the end of its own
	// statement (";") or its own Go raw-string literal (a backtick) and cannot
	// run into a neighboring literal that happens to mention the table.
	resolvedRowMutationPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?is)\bUPDATE\s+(?:ONLY\s+)?` + resolvedTable),
		// Postgres makes FROM optional: DELETE tbl and DELETE ONLY tbl remove
		// rows exactly like DELETE FROM tbl.
		regexp.MustCompile(`(?is)\bDELETE\s+(?:FROM\s+)?(?:ONLY\s+)?` + resolvedTable),
		regexp.MustCompile("(?is)\\bINSERT\\s+INTO\\s+" + resolvedTable + "[^;`]*?\\bDO\\s+UPDATE\\b"),
		regexp.MustCompile(`(?is)\bMERGE\s+INTO\s+(?:ONLY\s+)?` + resolvedTable),
		regexp.MustCompile("(?is)\\bTRUNCATE\\b[^;`]*?" + resolvedTable),
		regexp.MustCompile("(?is)\\bDROP\\s+TABLE\\b[^;`]*?" + resolvedTable),
		regexp.MustCompile("(?is)\\bCREATE\\s+(?:OR\\s+REPLACE\\s+)?(?:CONSTRAINT\\s+)?TRIGGER\\b[^;`]*?\\bON\\s+" + resolvedTable),
		regexp.MustCompile("(?is)\\bALTER\\s+TABLE\\s+(?:IF\\s+EXISTS\\s+)?(?:ONLY\\s+)?" + resolvedTable + "[^;`]*?\\b(?:REFERENCES|FOREIGN\\s+KEY)\\b"),
		regexp.MustCompile("(?is)\\bCREATE\\s+TABLE\\s+(?:IF\\s+NOT\\s+EXISTS\\s+)?" + resolvedTable + "[^;`]*?\\bREFERENCES\\b"),
		regexp.MustCompile("(?is)\\bCREATE\\s+RULE\\b[^;`]*?\\bON\\s+" + resolvedTable),
	}
	sqlLineComment  = regexp.MustCompile(`(?m)--[^\n]*`)
	sqlBlockComment = regexp.MustCompile(`(?s)/\*.*?\*/`)
)

// stripGoComments blanks every Go comment with the Go scanner, so a "//" inside
// a string or raw-string literal (a URL, say) does not start a comment and hide
// SQL after it on the same line. Newlines are kept so offsets stay readable.
func stripGoComments(src string) string {
	b := []byte(src)
	fset := token.NewFileSet()
	file := fset.AddFile("", fset.Base(), len(b))
	var scan scanner.Scanner
	scan.Init(file, b, nil, scanner.ScanComments)
	for {
		pos, tok, lit := scan.Scan()
		if tok == token.EOF {
			break
		}
		if tok != token.COMMENT {
			continue
		}
		start := file.Offset(pos)
		for i := start; i < start+len(lit) && i < len(b); i++ {
			if b[i] != '\n' {
				b[i] = ' '
			}
		}
	}
	return string(b)
}

// stripScanComments removes Go or SQL comments (per file kind) plus SQL
// block comments, so prose cannot trip or hide a finding. Block comments are
// stripped even inside Go literals because a SQL hint (UPDATE /*hint*/ t)
// sits inside the literal, out of reach of the Go scanner.
func stripScanComments(name, src string) string {
	if strings.HasSuffix(name, ".sql") {
		src = sqlLineComment.ReplaceAllString(src, "")
	} else {
		src = stripGoComments(src)
	}
	return sqlBlockComment.ReplaceAllString(src, " ")
}

// scanResolvedRowMutations reports every place a source set can change or
// remove a resolved_relationships row. files maps a display name to the source
// text; names ending in .sql are scanned as SQL, others as Go. Comments are
// stripped first so prose cannot trip or hide a finding.
func scanResolvedRowMutations(files map[string]string) []string {
	var violations []string
	for name, src := range files {
		src = stripScanComments(name, src)
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
			if mentionsResolvedRelationships(string(src)) {
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

// mentionsResolvedRelationships reports whether src names the table in any
// letter case. Postgres folds unquoted identifiers, so the enumeration filter
// must be case-insensitive to match the (?is) scan patterns: an unquoted
// UPDATE Resolved_Relationships names the same table.
func mentionsResolvedRelationships(src string) bool {
	return strings.Contains(strings.ToLower(src), "resolved_relationships")
}

// TestResolvedRelationshipsEnumerationFoldsCase is the proof that a
// mixed-case spelling still enters the scan set (F-2): the patterns are
// case-insensitive, so the enumeration filter must be too.
func TestResolvedRelationshipsEnumerationFoldsCase(t *testing.T) {
	t.Parallel()

	if !mentionsResolvedRelationships("const q = `UPDATE Resolved_Relationships SET confidence = 1`") {
		t.Fatal("mixed-case table name was not enumerated for the scan")
	}
	if mentionsResolvedRelationships("const q = `UPDATE relationship_generations SET status = 1`") {
		t.Fatal("unrelated source was enumerated for the scan")
	}
}

// TestResolvedRelationshipsStayAppendOnly fails when any non-test Go source or
// migration can update, delete, truncate, or cascade-remove a
// resolved_relationships row. It also fails if the scan finds no source that
// writes the table, because an empty scan would pass for the wrong reason.
func TestResolvedRelationshipsStayAppendOnly(t *testing.T) {
	t.Parallel()

	files := resolvedRelationshipsSources(t)
	sawInsert := false
	for name, src := range files {
		if regexp.MustCompile(`(?is)INSERT\s+INTO\s+resolved_relationships\b`).MatchString(stripScanComments(name, src)) {
			sawInsert = true
		}
	}
	if !sawInsert {
		t.Fatalf("no source inserts into resolved_relationships among %d scanned files; the scan is broken, not the code", len(files))
	}
	if violations := scanResolvedRowMutations(files); len(violations) > 0 {
		t.Fatalf("resolved_relationships is no longer append-only; the deployable_unit_correlation input fingerprint "+
			"(hex sha256 of the sorted resolved_id set, #8283) would miss the change. Update the fingerprint contract "+
			"(backfill.ResolvedInputDigestSQL/backfill.ResolvedInputRowsSQL) before adding this:\n  %s", strings.Join(violations, "\n  "))
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
		"update.go":               "package x\nconst q = `UPDATE resolved_relationships SET confidence = 1 WHERE generation_id = $1`\n",
		"delete.go":               "package x\nconst q = `DELETE FROM resolved_relationships WHERE generation_id = $1`\n",
		"truncate.sql":            "TRUNCATE TABLE resolved_relationships;\n",
		"drop.sql":                "DROP TABLE IF EXISTS resolved_relationships;\n",
		"trigger.sql":             "CREATE TRIGGER t AFTER INSERT ON resolved_relationships FOR EACH ROW EXECUTE FUNCTION f();\n",
		"fk_alter.sql":            "ALTER TABLE resolved_relationships ADD CONSTRAINT fk FOREIGN KEY (generation_id) REFERENCES relationship_generations(generation_id) ON DELETE CASCADE;\n",
		"upsert_update.go":        "package x\nconst q = `INSERT INTO resolved_relationships (resolved_id, confidence) VALUES ($1, $2) ON CONFLICT (resolved_id) DO UPDATE SET confidence = EXCLUDED.confidence`\n",
		"upsert_update.sql":       "INSERT INTO public.resolved_relationships (resolved_id) SELECT 1 ON CONFLICT (resolved_id) DO UPDATE SET confidence = 1;\n",
		"merge.go":                "package x\nconst q = `MERGE INTO resolved_relationships AS t USING src ON t.resolved_id = src.id WHEN MATCHED THEN UPDATE SET confidence = src.c`\n",
		"quoted_update.go":        "package x\nconst q = `UPDATE \"resolved_relationships\" SET confidence = 1`\n",
		"quoted_delete.sql":       "DELETE FROM public.\"resolved_relationships\" WHERE generation_id = 'x';\n",
		"quoted_schema.sql":       "DELETE FROM \"public\".\"resolved_relationships\";\n",
		"quoted_upsert.go":        "package x\nconst q = `INSERT INTO \"resolved_relationships\" (resolved_id) VALUES ($1) ON CONFLICT (resolved_id) DO UPDATE SET confidence = 1`\n",
		"after_url.go":            "package x\nvar a, b = \"http://example.test/x\", \"UPDATE resolved_relationships SET confidence = 1\"\n",
		"fk_create.sql":           "CREATE TABLE resolved_relationships (resolved_id TEXT PRIMARY KEY, generation_id TEXT REFERENCES relationship_generations(generation_id) ON DELETE CASCADE);\n",
		"delete_no_from.go":       "package x\nconst q = `DELETE resolved_relationships WHERE generation_id = $1`\n",
		"delete_only_no_from.sql": "DELETE ONLY resolved_relationships WHERE generation_id = 'x';\n",
		"blockcmt_go.go":          "package x\nconst q = `UPDATE /*hint*/ resolved_relationships SET confidence = 1`\n",
		"blockcmt.sql":            "UPDATE /*hint*/ resolved_relationships SET confidence = 1;\n",
		"spaced_schema.go":        "package x\nconst q = `DELETE FROM public . resolved_relationships WHERE generation_id = $1`\n",
		"mixed_case_update.go":    "package x\nconst q = `UPDATE Resolved_Relationships SET confidence = 1`\n",
	} {
		got := scanResolvedRowMutations(map[string]string{name: planted})
		if len(got) == 0 {
			t.Errorf("planted %s was not flagged", name)
		}
	}
}
