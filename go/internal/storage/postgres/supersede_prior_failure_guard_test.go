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
	"strings"
	"testing"
)

// Guard for #7320: every statement that sets a fact_work_items row to
// superseded must fold the row's prior failure into failure_details through
// priorFailureDetailsSQL. The writers are enumerated from the Go source of the
// whole module, not from a list kept here, so a sixth writer added tomorrow is
// held to the same rule without anyone editing this test.

var (
	// updateWorkItemsPattern starts one fact_work_items UPDATE statement.
	updateWorkItemsPattern = regexp.MustCompile(`(?i)UPDATE\s+fact_work_items\b`)
	// wherePattern ends the SET list of a statement.
	wherePattern = regexp.MustCompile(`(?i)\bWHERE\b`)
	// setSupersededPattern is a work row assigned the superseded status. The
	// leading word boundary keeps *_authorized_status assignments out.
	setSupersededPattern = regexp.MustCompile(`(?i)\bstatus\s*=\s*'superseded'`)
)

// supersedeWriterViolations parses one Go source file and returns a message for
// every top-level declaration holding a fact_work_items UPDATE that assigns
// status = 'superseded' in its SET list without calling priorFailureDetailsSQL
// in that same SET list, plus how many writers it saw.
func supersedeWriterViolations(t *testing.T, filename, src string) (violations []string, writers int) {
	t.Helper()
	if !strings.Contains(src, "fact_work_items") || !strings.Contains(src, "'superseded'") {
		return nil, 0
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", filename, err)
	}
	for _, decl := range file.Decls {
		text := src[fset.Position(decl.Pos()).Offset:fset.Position(decl.End()).Offset]
		if gen, ok := decl.(*ast.GenDecl); ok && gen.Tok == token.IMPORT {
			continue
		}
		for _, loc := range updateWorkItemsPattern.FindAllStringIndex(text, -1) {
			setList := text[loc[0]:]
			if end := wherePattern.FindStringIndex(setList); end != nil {
				setList = setList[:end[0]]
			}
			if !setSupersededPattern.MatchString(setList) {
				continue
			}
			writers++
			if !strings.Contains(setList, "priorFailureDetailsSQL(") {
				violations = append(violations, filename+": a fact_work_items UPDATE sets status = 'superseded' "+
					"without priorFailureDetailsSQL in its SET list (#7320)")
			}
		}
	}
	return violations, writers
}

// TestSupersedeStatementsFoldPriorFailure holds the whole module to the rule.
// It also fails if the scan finds fewer writers than the five known when this
// guard landed, so a broken pattern cannot pass by matching nothing.
func TestSupersedeStatementsFoldPriorFailure(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	var violations []string
	writers := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if name := entry.Name(); name == "vendor" || name == "testdata" || name == ".gocache" {
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
		found, count := supersedeWriterViolations(t, path, string(src))
		violations = append(violations, found...)
		writers += count
		return nil
	})
	if err != nil {
		t.Fatalf("walk module: %v", err)
	}
	if writers < 5 {
		t.Fatalf("found %d fact_work_items supersede writers in source, want at least 5: the scan is broken", writers)
	}
	if len(violations) > 0 {
		t.Fatalf("supersede writers without the prior-failure fold:\n%s", strings.Join(violations, "\n"))
	}
}

// TestSupersedeWriterGuardSeededViolation is the guard's RED/GREEN pair: a
// planted writer without the fold is reported, the same writer with it is not,
// and stripping the fold from a real writer's source is reported.
func TestSupersedeWriterGuardSeededViolation(t *testing.T) {
	planted := "package p\n\nconst q = `\nUPDATE fact_work_items AS w\nSET status = 'superseded',\n    failure_details = jsonb_build_object('k', 1)\nWHERE w.status = 'pending'\n`\n"
	if v, n := supersedeWriterViolations(t, "planted.go", planted); n != 1 || len(v) != 1 {
		t.Fatalf("planted writer: violations=%v writers=%d, want 1 violation over 1 writer", v, n)
	}
	folded := strings.Replace(planted, "jsonb_build_object('k', 1)\n", "jsonb_build_object('k', 1) || ` + priorFailureDetailsSQL(\"w\") + `\n", 1)
	if v, n := supersedeWriterViolations(t, "folded.go", folded); n != 1 || len(v) != 0 {
		t.Fatalf("folded writer: violations=%v writers=%d, want none over 1 writer", v, n)
	}
	other := "package p\n\nconst q = `\nUPDATE fact_work_items AS w\nSET status = 'retrying'\nWHERE w.status = 'superseded'\n`\n"
	if v, n := supersedeWriterViolations(t, "other.go", other); n != 0 || len(v) != 0 {
		t.Fatalf("a retry that only reads superseded: violations=%v writers=%d, want neither", v, n)
	}

	real, err := os.ReadFile("projector_queue_sql.go")
	if err != nil {
		t.Fatalf("read projector_queue_sql.go: %v", err)
	}
	if v, n := supersedeWriterViolations(t, "projector_queue_sql.go", string(real)); n != 3 || len(v) != 0 {
		t.Fatalf("real projector_queue_sql.go: violations=%v writers=%d, want 3 folded writers", v, n)
	}
	stripped := strings.ReplaceAll(string(real), "priorFailureDetailsSQL(", "somethingElse(")
	if v, n := supersedeWriterViolations(t, "projector_queue_sql.go", stripped); n != 3 || len(v) != 3 {
		t.Fatalf("stripped projector_queue_sql.go: violations=%v writers=%d, want all 3 reported", v, n)
	}
}

// TestPriorFailureFragmentShape pins the fragment's contract: the alias fills
// every column reference, the five fields are the only ones folded, and the old
// failure_details is passed through as text, never cast to jsonb (a non-JSON
// value would abort the claim statement for every worker).
func TestPriorFailureFragmentShape(t *testing.T) {
	got := priorFailureDetailsSQL("stale")
	if strings.Contains(got, priorFailureRowToken) {
		t.Fatalf("fragment leaves the alias token unfilled:\n%s", got)
	}
	for _, want := range []string{
		"stale.status IN ('failed', 'dead_letter')",
		"NULLIF(BTRIM(COALESCE(stale.failure_class, '')), '') IS NOT NULL",
		"NULLIF(BTRIM(COALESCE(stale.failure_message, '')), '') IS NOT NULL",
		"NULLIF(BTRIM(COALESCE(stale.failure_details, '')), '') IS NOT NULL",
		"'status', stale.status",
		"'failure_class', stale.failure_class",
		"'failure_message', stale.failure_message",
		"'failure_details', stale.failure_details,",
		"'updated_at', stale.updated_at",
		"ELSE '{}'::jsonb",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("fragment missing %q:\n%s", want, got)
		}
	}
	for _, banned := range []string{"attempt_count", "last_attempt_at", "created_at"} {
		if strings.Contains(got, banned) {
			t.Fatalf("fragment folds %q, which a supersede leaves in place", banned)
		}
	}
	if regexp.MustCompile(`(?i)failure_details\s*(::|\))?\s*::\s*jsonb|CAST\(`).MatchString(got) {
		t.Fatalf("fragment casts the old failure_details to jsonb:\n%s", got)
	}
	if priorFailureDetailsSQL("work") == got || !strings.Contains(priorFailureDetailsSQL("work"), "work.failure_details") {
		t.Fatal("fragment does not follow the alias")
	}
}
