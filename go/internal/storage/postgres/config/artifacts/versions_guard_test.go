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

const versionsTable = "config_artifact_content_versions"

var (
	// versionsMentionPattern finds statements that name the versions table.
	versionsMentionPattern = regexp.MustCompile(`(?i)\bconfig_artifact_content_versions\b`)
	// versionsDeletePattern is a DELETE of version rows.
	versionsDeletePattern = regexp.MustCompile(`(?i)\bDELETE\s+FROM\s+(?:ONLY\s+)?config_artifact_content_versions\b`)
	// versionsTruncatePattern is a TRUNCATE whose table list names the
	// versions table.
	versionsTruncatePattern = regexp.MustCompile(`(?i)\bTRUNCATE\b[^;]*\bconfig_artifact_content_versions\b`)
	// versionsDropPattern drops the table, which deletes every row.
	versionsDropPattern = regexp.MustCompile(`(?i)\bDROP\s+TABLE\b[^;]*\bconfig_artifact_content_versions\b`)
	// versionsUpdatePattern starts an UPDATE of version rows; the SET list
	// follows.
	versionsUpdatePattern = regexp.MustCompile(`(?i)\bUPDATE\s+(?:ONLY\s+)?config_artifact_content_versions\b`)
	// conflictUpdatePattern starts the DO UPDATE SET list of an upsert into
	// the versions table.
	conflictUpdatePattern = regexp.MustCompile(`(?i)\bINSERT\s+INTO\s+config_artifact_content_versions\b[^;]*?\bDO\s+UPDATE\s+SET\b`)
	// versionAssignPattern captures the right-hand side of a version
	// assignment up to the next comma, WHERE, or end of statement.
	versionAssignPattern = regexp.MustCompile(`(?i)(?:^|[\s,(])version\s*=\s*([^,;]*?)(?:,|\bWHERE\b|;|$)`)
	// bumpPattern is the only allowed version assignment: one more than the
	// stored value.
	bumpPattern = regexp.MustCompile(`(?i)^(?:\w+\.)?version\s*\+\s*1$`)
	// stateDeletePattern is the retirement path's paired delete.
	stateDeletePattern = regexp.MustCompile(`(?i)\bDELETE\s+FROM\s+(?:ONLY\s+)?config_artifact_repo_state\b`)
	// sqlCommentPattern strips SQL line and block comments.
	sqlCommentPattern = regexp.MustCompile(`(?s)--[^\n]*|/\*.*?\*/`)
	// statementWhitespace collapses whitespace in a statement.
	statementWhitespace = regexp.MustCompile(`\s+`)
)

// versionStatementViolations checks one SQL text (a Go declaration's string
// literals, or a migration file) and returns a message per statement that
// deletes or resets version rows outside the retirement path, plus how many
// statements named the versions table.
func versionStatementViolations(origin, text string) (violations []string, mentions int) {
	text = sqlCommentPattern.ReplaceAllString(text, " ")
	for _, statement := range strings.Split(text, ";") {
		statement = strings.TrimSpace(statementWhitespace.ReplaceAllString(statement, " "))
		if !versionsMentionPattern.MatchString(statement) {
			continue
		}
		mentions++
		retires := stateDeletePattern.MatchString(statement)
		report := func(what string) {
			violations = append(violations, fmt.Sprintf("%s: %s %s outside the retirement path "+
				"(one statement that also deletes config_artifact_repo_state): %.160s", origin, what, versionsTable, statement))
		}
		switch {
		case versionsDeletePattern.MatchString(statement) && !retires:
			report("DELETE FROM")
		case versionsTruncatePattern.MatchString(statement):
			report("TRUNCATE of")
		case versionsDropPattern.MatchString(statement):
			report("DROP TABLE of")
		}
		for _, start := range []*regexp.Regexp{versionsUpdatePattern, conflictUpdatePattern} {
			for _, loc := range start.FindAllStringIndex(statement, -1) {
				for _, assign := range versionAssignPattern.FindAllStringSubmatch(statement[loc[1]:], -1) {
					if rhs := strings.TrimSpace(assign[1]); !bumpPattern.MatchString(rhs) {
						report(fmt.Sprintf("version = %s reset of", rhs))
					}
				}
			}
		}
	}
	return violations, mentions
}

// goDeclarationSQL returns, per top-level declaration of one Go file, the
// concatenation of its string literals: the SQL the declaration can send.
func goDeclarationSQL(t *testing.T, filename, src string) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", filename, err)
	}
	var out []string
	for _, decl := range file.Decls {
		if gen, ok := decl.(*ast.GenDecl); ok && gen.Tok == token.IMPORT {
			continue
		}
		var parts []string
		ast.Inspect(decl, func(node ast.Node) bool {
			lit, ok := node.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			if value, err := strconv.Unquote(lit.Value); err == nil {
				parts = append(parts, value)
			}
			return true
		})
		out = append(out, strings.Join(parts, " "))
	}
	return out
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
			for _, text := range goDeclarationSQL(t, path, string(src)) {
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
	}
	for name, statement := range green {
		if v, _ := versionStatementViolations(name, statement); len(v) != 0 {
			t.Errorf("%s: violations=%v, want none for %q", name, v, statement)
		}
	}

	// A Go declaration is read through its string literals, including a
	// statement split across concatenated pieces.
	planted := "package p\n\nconst q = `DELETE FROM ` +\n\t\"config_artifact_content_versions WHERE repo_id = $1\"\n"
	var found []string
	for _, text := range goDeclarationSQL(t, "planted.go", planted) {
		v, _ := versionStatementViolations("planted.go", text)
		found = append(found, v...)
	}
	if len(found) != 1 {
		t.Fatalf("planted Go delete: violations=%v, want 1", found)
	}
}
