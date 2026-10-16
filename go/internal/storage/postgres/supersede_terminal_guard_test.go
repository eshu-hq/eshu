// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Guard for #7427 c1: superseded is a terminal scope_generations status. The
// search index pre-delete deletes a superseded generation's rows in committed
// batches before the prune, so a statement that moved a row back out of
// superseded (to pending, active or failed) would revive a generation whose
// search index is already gone. Every scope_generations writer in the module's
// Go source is enumerated, not listed here, so a new writer is held to the
// rule without editing this test.

var (
	// sgUpdatePattern starts a scope_generations UPDATE and captures its alias.
	sgUpdatePattern = regexp.MustCompile(`(?i)\bUPDATE\s+scope_generations\b(?:\s+AS\s+(\w+))?\s+SET\b`)
	// sgUpsertPattern starts the DO UPDATE SET list of a scope_generations upsert.
	sgUpsertPattern = regexp.MustCompile(`(?is)\bINSERT\s+INTO\s+scope_generations\b.*?\bON\s+CONFLICT\b.*?\bDO\s+UPDATE\s+SET\b`)
	// sgWherePattern ends a SET list.
	sgWherePattern = regexp.MustCompile(`(?i)\bWHERE\b`)
	// sgWhereEndPattern ends a WHERE clause inside a CTE or before RETURNING.
	sgWhereEndPattern = regexp.MustCompile("(?i)\\bRETURNING\\b|\\n\\s*\\)|`")
	// sgWhenThenPattern is a CASE condition; only its THEN/ELSE results are
	// assigned.
	sgWhenThenPattern = regexp.MustCompile(`(?is)\bWHEN\b.*?\bTHEN\b`)
	// sgCommentPattern strips SQL comments from a statement.
	sgCommentPattern = regexp.MustCompile(`(?s)--[^\n]*|/\*.*?\*/`)
	// sgTokenPattern splits an assigned expression into words and literals.
	sgTokenPattern = regexp.MustCompile(`'[^']*'|[\w.]+`)
)

// sgSetItems splits a SET list into assignments at top-level commas.
func sgSetItems(setList string) []string {
	var items []string
	depth, quoted, start := 0, false, 0
	for i, r := range setList {
		switch {
		case r == '\'':
			quoted = !quoted
		case quoted:
		case r == '(':
			depth++
		case r == ')':
			depth--
		case r == ',' && depth == 0:
			items = append(items, setList[start:i])
			start = i + 1
		}
	}
	return append(items, setList[start:])
}

// sgNeverLeavesSuperseded reports whether an assigned status expression can
// only yield 'superseded' or keep the row's own status.
func sgNeverLeavesSuperseded(expr, alias string) bool {
	expr = sgWhenThenPattern.ReplaceAllString(expr, " ")
	for _, token := range sgTokenPattern.FindAllString(expr, -1) {
		switch strings.ToLower(token) {
		case "case", "else", "end", "'superseded'", "status", "scope_generations.status":
			continue
		}
		if alias != "" && strings.EqualFold(token, alias+".status") {
			continue
		}
		return false
	}
	return true
}

// sgWherePinsNonSuperseded reports whether a WHERE clause restricts the row's
// status to values that exclude superseded.
func sgWherePinsNonSuperseded(where string) bool {
	if regexp.MustCompile(`(?i)\bstatus\s*(<>|!=)\s*'superseded'`).MatchString(where) {
		return true
	}
	for _, m := range regexp.MustCompile(`(?i)\bstatus\s*=\s*'(\w+)'`).FindAllStringSubmatch(where, -1) {
		if !strings.EqualFold(m[1], "superseded") {
			return true
		}
	}
	for _, m := range regexp.MustCompile(`(?i)\bstatus\s+IN\s*\(([^)]*)\)`).FindAllStringSubmatch(where, -1) {
		if !strings.Contains(strings.ToLower(m[1]), "'superseded'") {
			return true
		}
	}
	return false
}

// supersededTerminalViolations parses one Go file and returns a message for
// every scope_generations status assignment that could move a superseded row,
// plus how many status writers it saw.
func supersededTerminalViolations(t *testing.T, filename, src string) (violations []string, writers int) {
	t.Helper()
	if !strings.Contains(src, "scope_generations") {
		return nil, 0
	}
	file, err := parser.ParseFile(token.NewFileSet(), filename, src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", filename, err)
	}
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		text := strings.Trim(lit.Value, "`\"")
		text = sgCommentPattern.ReplaceAllString(text, " ")
		type statement struct{ alias, rest string }
		var found []statement
		for _, loc := range sgUpdatePattern.FindAllStringSubmatchIndex(text, -1) {
			alias := ""
			if loc[2] >= 0 {
				alias = text[loc[2]:loc[3]]
			}
			found = append(found, statement{alias, text[loc[1]:]})
		}
		for _, loc := range sgUpsertPattern.FindAllStringIndex(text, -1) {
			found = append(found, statement{"", text[loc[1]:]})
		}
		for _, st := range found {
			setList, where := st.rest, ""
			if loc := sgWherePattern.FindStringIndex(st.rest); loc != nil {
				setList, where = st.rest[:loc[0]], st.rest[loc[1]:]
				if end := sgWhereEndPattern.FindStringIndex(where); end != nil {
					where = where[:end[0]]
				}
			}
			for _, item := range sgSetItems(setList) {
				column, expr, ok := strings.Cut(item, "=")
				if !ok || !strings.EqualFold(strings.TrimSpace(column), "status") {
					continue
				}
				writers++
				if sgNeverLeavesSuperseded(expr, st.alias) || sgWherePinsNonSuperseded(where) {
					continue
				}
				violations = append(violations, fmt.Sprintf(
					"%s: scope_generations status = %s can move a superseded row (#7427 c1: pin status away from 'superseded' in its WHERE)",
					filename, strings.TrimSpace(expr)))
			}
		}
		return true
	})
	return violations, writers
}

// TestScopeGenerationStatusNeverLeavesSuperseded holds every production
// scope_generations status writer to the terminal-superseded rule. It also
// fails if the scan finds fewer writers than existed when it landed, so a
// broken pattern cannot pass by matching nothing.
func TestScopeGenerationStatusNeverLeavesSuperseded(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	var violations []string
	writers := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if name := entry.Name(); name == "vendor" || name == "testdata" || name == ".gocache" || name == "testfixtures" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		found, count := supersededTerminalViolations(t, path, string(src))
		violations = append(violations, found...)
		writers += count
		return nil
	})
	if err != nil {
		t.Fatalf("walk module: %v", err)
	}
	if writers < 9 {
		t.Fatalf("found %d scope_generations status writers, want at least 9: the scan is broken", writers)
	}
	if len(violations) > 0 {
		t.Fatalf("scope_generations writers that can leave superseded:\n%s", strings.Join(violations, "\n"))
	}
}

// TestScopeGenerationStatusGuardSeededViolation is the guard's RED/GREEN pair.
func TestScopeGenerationStatusGuardSeededViolation(t *testing.T) {
	for name, tc := range map[string]struct {
		src  string
		want int
	}{
		"revive without pin":   {"package p\nconst q = `UPDATE scope_generations SET status = 'active' WHERE generation_id = $1`\n", 1},
		"revive pinned":        {"package p\nconst q = `UPDATE scope_generations SET status = 'active' WHERE generation_id = $1 AND status = 'pending'`\n", 0},
		"pin includes it":      {"package p\nconst q = `UPDATE scope_generations AS g SET status = 'failed' WHERE g.status IN ('active', 'superseded')`\n", 1},
		"supersede":            {"package p\nconst q = `UPDATE scope_generations SET status = 'superseded' WHERE generation_id = $1`\n", 0},
		"case keeps own":       {"package p\nconst q = `UPDATE scope_generations AS g SET status = CASE WHEN g.status = 'pending' THEN 'superseded' ELSE g.status END WHERE g.generation_id = $1`\n", 0},
		"case revives":         {"package p\nconst q = `UPDATE scope_generations AS g SET status = CASE WHEN g.status = 'superseded' THEN 'pending' ELSE g.status END WHERE g.generation_id = $1`\n", 1},
		"upsert without pin":   {"package p\nconst q = `INSERT INTO scope_generations (generation_id, status) VALUES ($1, $2) ON CONFLICT (generation_id) DO UPDATE SET status = EXCLUDED.status`\n", 1},
		"upsert pinned":        {"package p\nconst q = `INSERT INTO scope_generations (generation_id, status) VALUES ($1, $2) ON CONFLICT (generation_id) DO UPDATE SET status = EXCLUDED.status WHERE scope_generations.status = 'pending'`\n", 0},
		"comment is no pin":    {"package p\nconst q = `UPDATE scope_generations SET status = 'active' WHERE generation_id = $1 -- AND status = 'pending'\n`\n", 1},
		"other column ignored": {"package p\nconst q = `UPDATE scope_generations SET payload = '{}' WHERE generation_id = $1`\n", 0},
	} {
		if got, _ := supersededTerminalViolations(t, name+".go", tc.src); len(got) != tc.want {
			t.Errorf("%s: violations = %v, want %d", name, got, tc.want)
		}
	}
}
