// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Guard for #9320: every SQL statement that moves a fact work item back to
// pending, or reopens a completed shared projection intent, must also write
// reopened_at, so the #9292 backlog age counts from the reopen. The writers
// are found in the Go source of the whole module, so a writer added later is
// held to the rule without anyone editing this test.

// pendingWriterPattern matches a SET list that assigns status = 'pending'
// unconditionally: an UPDATE/ON CONFLICT SET, or a composed SET-list constant
// that starts with the assignment. CASE-style writers (THEN 'pending') stamp
// through their own CASE and are covered by their own tests.
var pendingWriterPattern = regexp.MustCompile(`(?i)(\bSET\s+status\s*=\s*'pending'|^\s*status\s*=\s*'pending'\s*,)`)

// sharedIntentReopenPattern matches an UPDATE that reopens a completed shared
// projection intent.
var sharedIntentReopenPattern = regexp.MustCompile(`(?is)UPDATE\s+shared_projection_intents\b.*\bSET\s+completed_at\s*=\s*NULL`)

// otherQueueTables name tables whose own status column is not a fact work
// item; their pending writers are out of scope.
var otherQueueTables = regexp.MustCompile(`(?i)\bworkflow_work_items\b`)

// reopenStampViolations returns "file:line" for every string literal in files
// that reopens a row without naming reopened_at. files maps a display name to
// Go source.
func reopenStampViolations(t *testing.T, files map[string]string) []string {
	t.Helper()
	fset := token.NewFileSet()
	var violations []string
	for name, src := range files {
		file, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			lit, ok := node.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			text, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			reopens := (pendingWriterPattern.MatchString(text) && !otherQueueTables.MatchString(text)) ||
				sharedIntentReopenPattern.MatchString(text)
			if reopens && !strings.Contains(text, "reopened_at") {
				violations = append(violations, fset.Position(lit.Pos()).String())
			}
			return true
		})
	}
	sort.Strings(violations)
	return violations
}

// moduleProductionSources returns every non-test Go file under the module's
// internal tree, keyed by its path relative to the module root.
func moduleProductionSources(t *testing.T) map[string]string {
	t.Helper()
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate this test file")
	}
	moduleRoot := filepath.Clean(filepath.Join(filepath.Dir(here), "..", "..", ".."))
	files := map[string]string{}
	err := filepath.WalkDir(filepath.Join(moduleRoot, "internal"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(moduleRoot, path)
		if err != nil {
			return err
		}
		files[rel] = string(src)
		return nil
	})
	if err != nil {
		t.Fatalf("walk module sources: %v", err)
	}
	return files
}

// TestEveryTerminalReopenWriterStampsReopenedAt holds every production writer
// to the #9320 rule.
func TestEveryTerminalReopenWriterStampsReopenedAt(t *testing.T) {
	t.Parallel()
	if violations := reopenStampViolations(t, moduleProductionSources(t)); len(violations) > 0 {
		t.Fatalf("writers move rows back to pending or reopen intents without stamping reopened_at (#9320):\n  %s",
			strings.Join(violations, "\n  "))
	}
}

// TestReopenStampGuardBitesOnPlantedWriters proves the guard is not satisfied
// by an empty scan: planted writers fail it, their stamped twins pass, and a
// workflow-queue writer stays out of scope.
func TestReopenStampGuardBitesOnPlantedWriters(t *testing.T) {
	t.Parallel()
	planted := map[string]string{
		"planted.go": "package planted\n" +
			"const replay = `UPDATE fact_work_items SET status = 'pending', updated_at = $1 WHERE status = 'dead_letter'`\n" +
			"const setList = `status = 'pending',\n    updated_at = $1`\n" +
			"const intents = `UPDATE shared_projection_intents\nSET completed_at = NULL\nWHERE intent_id = ANY($1)`\n",
	}
	if got := reopenStampViolations(t, planted); len(got) != 3 {
		t.Fatalf("guard flagged %d planted writers, want 3: %v", len(got), got)
	}
	clean := map[string]string{
		"clean.go": "package clean\n" +
			"const replay = `UPDATE fact_work_items SET status = 'pending', reopened_at = $1 WHERE status = 'dead_letter'`\n" +
			"const setList = `status = 'pending',\n    reopened_at = $1`\n" +
			"const intents = `UPDATE shared_projection_intents\nSET completed_at = NULL, reopened_at = $2\nWHERE intent_id = ANY($1)`\n" +
			"const workflow = `UPDATE workflow_work_items AS item\nSET status = 'pending'\nWHERE item.status = 'claimed'`\n",
	}
	if got := reopenStampViolations(t, clean); len(got) != 0 {
		t.Fatalf("guard flagged stamped or out-of-scope writers: %v", got)
	}
}
