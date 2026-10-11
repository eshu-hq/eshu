// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Guard for the version-row rule (#7126 protocol section 1): delete or reset
// a config_artifact_content_versions row only in the same statement that
// deletes the repository's config_artifact_repo_state row, which is the one
// documented retirement path. A version row created again while its state row
// survives counts up from 1, can reach the old state.content_version, and
// makes readers trust stale rows. The statements are enumerated from the Go
// source of the whole module and from every shipped migration, not from a
// list kept here.
//
// Go source is read one literal expression at a time (one string literal, or
// one + chain of literals), so two statements in one const block or one
// function are two statements. Known blind spots: SQL assembled from
// non-literal values, and a retirement statement whose state DELETE names
// another repository than its version DELETE (the guard sees that both deletes
// are in one statement, not that they match on repo_id).

const versionsTable = "config_artifact_content_versions"

// tableRef matches a table name with an optional schema and optional double
// quotes on either part.
func tableRef(name string) string {
	return `(?:"?[A-Za-z_][A-Za-z0-9_$]*"?\s*\.\s*)?"?` + name + `\b"?`
}

var (
	versionsRef = tableRef(versionsTable)
	stateRef    = tableRef("config_artifact_repo_state")

	// versionsMentionPattern finds statements that name the versions table.
	versionsMentionPattern = regexp.MustCompile(`(?i)\bconfig_artifact_content_versions\b`)
	// versionsDeletePattern is a DELETE of version rows.
	versionsDeletePattern = regexp.MustCompile(`(?i)\bDELETE\s+FROM\s+(?:ONLY\s+)?` + versionsRef)
	// versionsMergePattern is a MERGE into the versions table; its WHEN
	// clauses can delete or update.
	versionsMergePattern = regexp.MustCompile(`(?i)\bMERGE\s+INTO\s+(?:ONLY\s+)?` + versionsRef)
	// mergeDeletePattern is a MERGE action that deletes the matched row.
	mergeDeletePattern = regexp.MustCompile(`(?i)\bTHEN\s+DELETE\b`)
	// versionsTruncatePattern is a TRUNCATE whose table list names the
	// versions table.
	versionsTruncatePattern = regexp.MustCompile(`(?i)\bTRUNCATE\b[^;]*\bconfig_artifact_content_versions\b`)
	// versionsDropPattern drops the table, which deletes every row.
	versionsDropPattern = regexp.MustCompile(`(?i)\bDROP\s+TABLE\b[^;]*\bconfig_artifact_content_versions\b`)
	// versionsUpdatePattern is an UPDATE of version rows.
	versionsUpdatePattern = regexp.MustCompile(`(?i)\bUPDATE\s+(?:ONLY\s+)?` + versionsRef)
	// conflictUpdatePattern is an upsert into the versions table with a DO
	// UPDATE action.
	conflictUpdatePattern = regexp.MustCompile(`(?i)\bINSERT\s+INTO\s+` + versionsRef + `[^;]*?\bDO\s+UPDATE\s+SET\b`)
	// setPattern starts a SET list; setEndPattern ends it.
	setPattern    = regexp.MustCompile(`(?i)\bSET\b`)
	setEndPattern = regexp.MustCompile(`(?i)\b(?:WHERE|WHEN|RETURNING|FROM)\b`)
	// versionAssignPattern captures the right-hand side of a version
	// assignment in a SET list, up to the next comma or the end of the list.
	versionAssignPattern = regexp.MustCompile(`(?i)(?:^|[\s,(])"?version"?\s*=\s*([^,]*)`)
	// rowSetPattern is a row-constructor assignment, SET (a, b) = (...); its
	// column list is captured.
	rowSetPattern = regexp.MustCompile(`\(([^()]*)\)\s*=`)
	// versionColumnPattern is the version column in a column list.
	versionColumnPattern = regexp.MustCompile(`(?i)(?:^|[\s,])"?version"?(?:$|[\s,])`)
	// bumpPattern is the only allowed version assignment: one more than the
	// stored value.
	bumpPattern = regexp.MustCompile(`(?i)^(?:"?\w+"?\.)?"?version"?\s*\+\s*1$`)
	// stateDeletePattern is the retirement path's paired delete.
	stateDeletePattern = regexp.MustCompile(`(?i)\bDELETE\s+FROM\s+(?:ONLY\s+)?` + stateRef)
	// sqlCommentPattern strips SQL line and block comments.
	sqlCommentPattern = regexp.MustCompile(`(?s)--[^\n]*|/\*.*?\*/`)
	// statementWhitespace collapses whitespace in a statement.
	statementWhitespace = regexp.MustCompile(`\s+`)
)

// versionStatementViolations checks one SQL text (one Go literal expression,
// or a migration file) and returns a message per statement that deletes or
// resets version rows outside the retirement path, plus how many statements
// named the versions table.
func versionStatementViolations(origin, text string) (violations []string, mentions int) {
	text = sqlCommentPattern.ReplaceAllString(text, " ")
	for _, statement := range strings.Split(text, ";") {
		statement = strings.TrimSpace(statementWhitespace.ReplaceAllString(statement, " "))
		if !versionsMentionPattern.MatchString(statement) {
			continue
		}
		mentions++
		retires := stateDeletePattern.MatchString(statement)
		merges := versionsMergePattern.MatchString(statement)
		report := func(what string) {
			violations = append(violations, fmt.Sprintf("%s: %s %s outside the retirement path "+
				"(one statement that also deletes config_artifact_repo_state): %.160s", origin, what, versionsTable, statement))
		}
		switch {
		case versionsDeletePattern.MatchString(statement) && !retires:
			report("DELETE FROM")
		case merges && mergeDeletePattern.MatchString(statement) && !retires:
			report("MERGE ... THEN DELETE on")
		case versionsTruncatePattern.MatchString(statement):
			report("TRUNCATE of")
		case versionsDropPattern.MatchString(statement):
			report("DROP TABLE of")
		}
		if !merges && !versionsUpdatePattern.MatchString(statement) && !conflictUpdatePattern.MatchString(statement) {
			continue
		}
		for _, setLoc := range setPattern.FindAllStringIndex(statement, -1) {
			list := statement[setLoc[1]:]
			if end := setEndPattern.FindStringIndex(list); end != nil {
				list = list[:end[0]]
			}
			for _, assign := range versionAssignPattern.FindAllStringSubmatch(list, -1) {
				if rhs := strings.TrimSpace(assign[1]); !bumpPattern.MatchString(rhs) {
					report(fmt.Sprintf("version = %s reset of", rhs))
				}
			}
			for _, row := range rowSetPattern.FindAllStringSubmatch(list, -1) {
				if versionColumnPattern.MatchString(row[1]) {
					report(fmt.Sprintf("row assignment (%s) = ... of", row[1]))
				}
			}
		}
	}
	return violations, mentions
}

// goLiteralSQL returns the SQL texts of one Go file: one text per string
// literal expression, where a + chain of literals is one expression (its
// literal parts concatenated) and any non-literal operand is read on its own.
// Two statements in one const block or one function are two texts.
func goLiteralSQL(t *testing.T, filename, src string) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", filename, err)
	}
	var out []string
	var visit func(node ast.Node) bool
	visit = func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.ImportSpec:
			return false
		case *ast.BinaryExpr:
			if n.Op != token.ADD {
				return true
			}
			var parts []string
			var others []ast.Expr
			flattenConcat(n, &parts, &others)
			if len(parts) > 0 {
				out = append(out, strings.Join(parts, ""))
			}
			for _, other := range others {
				ast.Inspect(other, visit)
			}
			return false
		case *ast.BasicLit:
			if n.Kind == token.STRING {
				if value, err := strconv.Unquote(n.Value); err == nil {
					out = append(out, value)
				}
			}
		}
		return true
	}
	ast.Inspect(file, visit)
	return out
}

// flattenConcat collects the string literal operands of a + chain in order,
// and the operands that are not string literals.
func flattenConcat(expr ast.Expr, parts *[]string, others *[]ast.Expr) {
	switch e := expr.(type) {
	case *ast.ParenExpr:
		flattenConcat(e.X, parts, others)
	case *ast.BinaryExpr:
		if e.Op == token.ADD {
			flattenConcat(e.X, parts, others)
			flattenConcat(e.Y, parts, others)
			return
		}
		*others = append(*others, e)
	case *ast.BasicLit:
		if e.Kind == token.STRING {
			if value, err := strconv.Unquote(e.Value); err == nil {
				*parts = append(*parts, value)
				return
			}
		}
		*others = append(*others, e)
	default:
		*others = append(*others, e)
	}
}

// TestNoStatementDeletesVersionRowsOutsideRetirement holds every non-test Go
// file in the module and every shipped migration to the rule. It also fails
// when the scan sees no statement naming the versions table, so a broken
// pattern cannot pass by matching nothing.
func TestNoStatementDeletesVersionRowsOutsideRetirement(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..", "..", "..", "..")
	var violations []string
	goMentions, sqlMentions, goFiles := 0, 0, 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if name := entry.Name(); name == "vendor" || name == "testdata" || strings.HasPrefix(name, ".gocache") {
				return filepath.SkipDir
			}
			return nil
		}
		switch {
		case strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go"):
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			goFiles++
			if !strings.Contains(string(src), versionsTable) {
				return nil
			}
			for _, text := range goLiteralSQL(t, path, string(src)) {
				found, n := versionStatementViolations(path, text)
				violations = append(violations, found...)
				goMentions += n
			}
		case strings.HasSuffix(path, ".sql") && strings.Contains(filepath.ToSlash(path), "storage/postgres/migrations/"):
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			found, n := versionStatementViolations(path, string(src))
			violations = append(violations, found...)
			sqlMentions += n
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk module: %v", err)
	}
	if goFiles < 1000 {
		t.Fatalf("scanned %d Go files, want at least 1000: the walk root is wrong", goFiles)
	}
	if goMentions < 1 || sqlMentions < 1 {
		t.Fatalf("found %d Go and %d migration statements naming %s, want at least 1 of each: the scan is broken",
			goMentions, sqlMentions, versionsTable)
	}
	if len(violations) > 0 {
		t.Fatalf("statements that delete or reset version rows outside the retirement path:\n%s",
			strings.Join(violations, "\n"))
	}
}

// TestVersionGuardSeededViolations is the guard's RED/GREEN pair: each planted
// delete or reset is reported, the retirement statement and the trigger's bump
// are not, and a mention in a comment is not a statement.
func TestVersionGuardSeededViolations(t *testing.T) {
	t.Parallel()

	red := map[string]string{
		"delete":              "DELETE FROM config_artifact_content_versions WHERE repo_id = $1",
		"delete only":         "delete from only config_artifact_content_versions",
		"truncate":            "TRUNCATE config_artifact_paths, config_artifact_content_versions",
		"drop":                "DROP TABLE IF EXISTS config_artifact_content_versions",
		"update reset":        "UPDATE config_artifact_content_versions SET version = 0, bumped_at = now() WHERE repo_id = $1",
		"update set param":    "UPDATE config_artifact_content_versions AS v SET bumped_at = now(), version = $2 WHERE v.repo_id = $1",
		"upsert reset":        "INSERT INTO config_artifact_content_versions AS v (repo_id, version, bumped_xid, bumped_at) VALUES ($1, 0, pg_current_xact_id(), now()) ON CONFLICT (repo_id) DO UPDATE SET version = EXCLUDED.version",
		"delete beside state": "DELETE FROM config_artifact_repo_state WHERE repo_id = $1; DELETE FROM config_artifact_content_versions WHERE repo_id = $1",
		"quoted table":        `DELETE FROM "config_artifact_content_versions" WHERE repo_id = $1`,
		"schema table":        "DELETE FROM public.config_artifact_content_versions WHERE repo_id = $1",
		"quoted schema table": `DELETE FROM "public"."config_artifact_content_versions" WHERE repo_id = $1`,
		"schema update reset": "UPDATE public.config_artifact_content_versions SET version = 0 WHERE repo_id = $1",
		"merge delete":        "MERGE INTO config_artifact_content_versions AS v USING gone AS g ON v.repo_id = g.repo_id WHEN MATCHED THEN DELETE",
		"merge reset":         "MERGE INTO config_artifact_content_versions AS v USING src AS s ON v.repo_id = s.repo_id WHEN MATCHED THEN UPDATE SET version = 0",
		"row set reset":       "UPDATE config_artifact_content_versions SET (version, bumped_at) = (0, now()) WHERE repo_id = $1",
		"upsert row set":      "INSERT INTO config_artifact_content_versions AS v (repo_id, version, bumped_xid, bumped_at) VALUES ($1, 0, pg_current_xact_id(), now()) ON CONFLICT (repo_id) DO UPDATE SET (version, bumped_at) = (EXCLUDED.version, now())",
	}
	for name, statement := range red {
		if v, n := versionStatementViolations(name, statement); len(v) == 0 || n == 0 {
			t.Errorf("%s: violations=%v mentions=%d, want a violation for %q", name, v, n, statement)
		}
	}

	green := map[string]string{
		"select": "SELECT version FROM config_artifact_content_versions WHERE repo_id = $1",
		"trigger bump": `INSERT INTO config_artifact_content_versions AS v (repo_id, version, bumped_xid, bumped_at)
SELECT r.repo_id, 1, pg_current_xact_id(), now() FROM (SELECT DISTINCT repo_id FROM new_rows) AS r ORDER BY r.repo_id
ON CONFLICT (repo_id) DO UPDATE SET version = v.version + 1, bumped_xid = EXCLUDED.bumped_xid, bumped_at = EXCLUDED.bumped_at
WHERE v.bumped_xid IS DISTINCT FROM EXCLUDED.bumped_xid`,
		"seed":    "INSERT INTO config_artifact_content_versions (repo_id, version, bumped_xid, bumped_at) SELECT DISTINCT repo_id, 0, pg_current_xact_id(), now() FROM content_files ON CONFLICT DO NOTHING",
		"retire":  "WITH s AS (DELETE FROM config_artifact_repo_state WHERE repo_id = $1) DELETE FROM config_artifact_content_versions WHERE repo_id = $1",
		"comment": "SELECT 1 -- DELETE FROM config_artifact_content_versions\n",
		"merge bump": "MERGE INTO config_artifact_content_versions AS v USING src AS s ON v.repo_id = s.repo_id " +
			"WHEN MATCHED THEN UPDATE SET version = v.version + 1 " +
			"WHEN NOT MATCHED THEN INSERT (repo_id, version, bumped_xid, bumped_at) VALUES (s.repo_id, 1, pg_current_xact_id(), now())",
		"schema bump": "UPDATE public.config_artifact_content_versions AS v SET version = v.version + 1 WHERE v.repo_id = $1",
	}
	for name, statement := range green {
		if v, _ := versionStatementViolations(name, statement); len(v) != 0 {
			t.Errorf("%s: violations=%v, want none for %q", name, v, statement)
		}
	}

	// Go source is read one literal expression at a time: a statement split
	// across a + chain is one statement, but two statements in one const
	// block or one function stay two statements, so a state delete beside a
	// version delete is not the retirement path.
	goRed := map[string]string{
		"split chain": "package p\n\nconst q = `DELETE FROM ` +\n\t\"config_artifact_content_versions WHERE repo_id = $1\"\n",
		"const block": "package p\n\nconst (\n\tdeleteStateSQL = \"DELETE FROM config_artifact_repo_state WHERE repo_id = $1\"\n" +
			"\tdeleteVersionSQL = \"DELETE FROM config_artifact_content_versions WHERE repo_id = $1\"\n)\n",
		"two execs": "package p\n\nfunc retire(db execer, id string) {\n" +
			"\tdb.Exec(\"DELETE FROM config_artifact_repo_state WHERE repo_id = $1\", id)\n" +
			"\tdb.Exec(\"DELETE FROM config_artifact_content_versions WHERE repo_id = $1\", id)\n}\n",
	}
	for name, src := range goRed {
		if found := goSourceViolations(t, name+".go", src); len(found) != 1 {
			t.Errorf("%s: violations=%v, want 1", name, found)
		}
	}
	goGreen := map[string]string{
		"retire const": "package p\n\nconst q = `WITH s AS (DELETE FROM config_artifact_repo_state WHERE repo_id = $1) ` +\n" +
			"\t`DELETE FROM config_artifact_content_versions WHERE repo_id = $1`\n",
		"retire exec": "package p\n\nfunc retire(db execer, id string) {\n\tdb.Exec(`WITH s AS (DELETE FROM config_artifact_repo_state WHERE repo_id = $1)\n" +
			"DELETE FROM config_artifact_content_versions WHERE repo_id = $1`, id)\n}\n",
	}
	for name, src := range goGreen {
		if found := goSourceViolations(t, name+".go", src); len(found) != 0 {
			t.Errorf("%s: violations=%v, want none", name, found)
		}
	}
}

// goSourceViolations runs the guard over one Go file's SQL texts.
func goSourceViolations(t *testing.T, filename, src string) []string {
	t.Helper()
	var found []string
	for _, text := range goLiteralSQL(t, filename, src) {
		v, _ := versionStatementViolations(filename, text)
		found = append(found, v...)
	}
	return found
}
