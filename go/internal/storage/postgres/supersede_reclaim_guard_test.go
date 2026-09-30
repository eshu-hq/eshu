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

// Guard for #7388, the sibling of the #7320 supersede guard: a statement that
// moves a live row back to retrying under the projector_stale_scope_reclaim
// marker rewrites failure_details, so it must fold the row's prior failure into
// them or the cause the last attempt failed with is erased. The writers are
// enumerated from the Go source of the whole module, like the supersede writers.

// reclaimAssignPattern is the marker assigned to failure_class in a SET list.
// It starts at an assignment, so the same text in a CASE condition
// (`stale.failure_class = '...'`) is not read as one.
var reclaimAssignPattern = regexp.MustCompile(`(?i)(?:^|[\s,])failure_class\s*=\s*'projector_stale_scope_reclaim'`)

// reclaimWriterViolations parses one Go source file and returns a message for
// every top-level declaration holding a fact_work_items UPDATE that assigns the
// reclaim marker without embedding the fragment constant for its alias in its
// failure_details assignment, plus how many reclaim writers it saw.
func reclaimWriterViolations(t *testing.T, filename, src string) (violations []string, writers int) {
	t.Helper()
	if !strings.Contains(src, "fact_work_items") || !strings.Contains(src, "projector_stale_scope_reclaim") {
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
		for _, loc := range updateWorkItemsPattern.FindAllStringSubmatchIndex(text, -1) {
			setList := text[loc[0]:]
			// The CASE condition in failure_details carries its own WHERE-less
			// text; the SET list ends at the statement's FROM or WHERE.
			if end := regexp.MustCompile(`(?i)\b(?:FROM|WHERE)\b`).FindStringIndex(setList); end != nil {
				setList = setList[:end[0]]
			}
			setList = commentPattern.ReplaceAllString(setList, " ")
			if !reclaimAssignPattern.MatchString(setList) {
				continue
			}
			writers++
			alias := ""
			if loc[2] >= 0 {
				alias = text[loc[2]:loc[3]]
			}
			want := priorFailureConstFor(alias)
			if want == "" || !foldOnFailureDetails(setList, want) {
				violations = append(violations, fmt.Sprintf("%s: a fact_work_items UPDATE aliased %q", filename, alias)+
					" reassigns projector_stale_scope_reclaim without "+want+" in its failure_details assignment (#7388)")
			}
		}
	}
	return violations, writers
}

// TestReclaimStatementsFoldPriorFailure holds the whole module to the rule. It
// also fails if the scan finds fewer writers than the two known when this guard
// landed, so a broken pattern cannot pass by matching nothing.
func TestReclaimStatementsFoldPriorFailure(t *testing.T) {
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
		found, count := reclaimWriterViolations(t, path, string(src))
		violations = append(violations, found...)
		writers += count
		return nil
	})
	if err != nil {
		t.Fatalf("walk module: %v", err)
	}
	if writers < 2 {
		t.Fatalf("found %d projector_stale_scope_reclaim writers in source, want at least 2: the scan is broken", writers)
	}
	if len(violations) > 0 {
		t.Fatalf("reclaim writers without the prior-failure fold:\n%s", strings.Join(violations, "\n"))
	}
}

// TestReclaimWriterGuardSeededViolation is the guard's RED/GREEN pair: a planted
// reclaim writer that rewrites the details without the fold is reported, the same
// writer with the constant for its alias is not, the other alias's constant is
// reported, and a constant that is only named in a comment or sits in another
// assignment is reported.
func TestReclaimWriterGuardSeededViolation(t *testing.T) {
	planted := "package p\n\nconst q = `\nUPDATE fact_work_items AS stale\nSET status = 'retrying',\n    failure_class = 'projector_stale_scope_reclaim',\n    failure_details = jsonb_build_object('k', 1)\nFROM locked WHERE stale.work_item_id = locked.work_item_id\n`\n"
	if v, n := reclaimWriterViolations(t, "planted.go", planted); n != 1 || len(v) != 1 {
		t.Fatalf("planted writer: violations=%v writers=%d, want 1 violation over 1 writer", v, n)
	}
	folded := strings.Replace(planted, "jsonb_build_object('k', 1)\n", "(jsonb_build_object('k', 1) || ` + priorFailureStaleSQL + `)::text\n", 1)
	if v, n := reclaimWriterViolations(t, "folded.go", folded); n != 1 || len(v) != 0 {
		t.Fatalf("folded writer: violations=%v writers=%d, want none over 1 writer", v, n)
	}
	// The shipped shape: the fold sits inside a CASE whose condition names the
	// marker again.
	cased := strings.Replace(planted, "jsonb_build_object('k', 1)\n",
		"CASE WHEN stale.failure_class = 'projector_stale_scope_reclaim' THEN stale.failure_details\n        ELSE (jsonb_build_object('k', 1) || ` + priorFailureStaleSQL + `)::text END\n", 1)
	if v, n := reclaimWriterViolations(t, "cased.go", cased); n != 1 || len(v) != 0 {
		t.Fatalf("CASE-wrapped writer: violations=%v writers=%d, want none over 1 writer", v, n)
	}
	wrongAlias := strings.Replace(folded, "priorFailureStaleSQL", "priorFailureWorkSQL", 1)
	if v, n := reclaimWriterViolations(t, "wrong.go", wrongAlias); n != 1 || len(v) != 1 {
		t.Fatalf("writer embedding the other alias's fragment: violations=%v writers=%d, want 1 violation", v, n)
	}
	commented := strings.Replace(planted, "jsonb_build_object('k', 1)\n", "jsonb_build_object('k', 1) -- priorFailureStaleSQL\n", 1)
	if v, n := reclaimWriterViolations(t, "commented.go", commented); n != 1 || len(v) != 1 {
		t.Fatalf("SQL comment naming the constant: violations=%v writers=%d, want 1 violation", v, n)
	}
	elsewhere := strings.Replace(planted, "    failure_details = jsonb_build_object('k', 1)\n", "    failure_details = jsonb_build_object('k', 1),\n    failure_message = ` + priorFailureStaleSQL + `\n", 1)
	if v, n := reclaimWriterViolations(t, "elsewhere.go", elsewhere); n != 1 || len(v) != 1 {
		t.Fatalf("constant in another assignment: violations=%v writers=%d, want 1 violation", v, n)
	}
}
