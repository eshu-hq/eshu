// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// unwindVariablePattern captures the variable an UNWIND clause binds.
var unwindVariablePattern = regexp.MustCompile(`(?i)\bUNWIND\s+\S+\s+AS\s+([A-Za-z_][A-Za-z0-9_]*)`)

// unwindFollowingMatchPattern finds a MATCH clause (plain or OPTIONAL).
var unwindFollowingMatchPattern = regexp.MustCompile(`(?i)\bMATCH\b`)

// unwindReturnTailPattern marks where a RETURN projection list ends.
var unwindReturnTailPattern = regexp.MustCompile(`(?i)\b(?:ORDER\s+BY|SKIP|LIMIT|UNION)\b|}`)

// unwindFollowingReturnPattern finds the start of a RETURN clause.
var unwindFollowingReturnPattern = regexp.MustCompile(`(?i)\bRETURN\b`)

// hasUnwindVariableReusedAsReturnAlias reports whether value binds a variable
// with UNWIND and later, after a MATCH, projects a RETURN column with that same
// name, either as an alias (`AS <name>`) or as the bare variable itself. NornicDB v1.3.3 names that column after the first UNWIND
// value instead of the alias (#6786, X9):
// `UNWIND $repo_ids AS repo_id MATCH (i:WorkloadInstance {repo_id: repo_id})
// RETURN i.repo_id AS repo_id` returns a column keyed "'r1'", so a reader
// looking up "repo_id" finds nothing. Each UNWIND is checked against the text
// that follows it, so an independent UNION branch that reuses a name with a
// different alias is not flagged.
func hasUnwindVariableReusedAsReturnAlias(value string) bool {
	for _, loc := range unwindVariablePattern.FindAllStringSubmatchIndex(value, -1) {
		name := value[loc[2]:loc[3]]
		rest := value[loc[1]:]
		matchLoc := unwindFollowingMatchPattern.FindStringIndex(rest)
		if matchLoc == nil {
			continue
		}
		afterMatch := rest[matchLoc[1]:]
		returnLoc := unwindFollowingReturnPattern.FindStringIndex(afterMatch)
		if returnLoc == nil {
			continue
		}
		returnBody := afterMatch[returnLoc[1]:]
		if next := unwindVariablePattern.FindStringIndex(returnBody); next != nil {
			returnBody = returnBody[:next[0]]
		}
		if end := unwindReturnTailPattern.FindStringIndex(returnBody); end != nil {
			returnBody = returnBody[:end[0]]
		}
		quoted := regexp.QuoteMeta(name)
		aliasPattern := regexp.MustCompile(`(?i)\bAS\s+` + quoted + `\b`)
		bareColumnPattern := regexp.MustCompile(`(?i)(?:^|,|\bDISTINCT\b)\s*` + quoted + `\s*(?:,|$)`)
		if aliasPattern.MatchString(returnBody) || bareColumnPattern.MatchString(returnBody) {
			return true
		}
	}
	return false
}

// scanFileUnwindAliasHits returns "relPath:line" for every string in file that
// hasUnwindVariableReusedAsReturnAlias flags, folding "+" chains the same way
// the other repo scans in this package do. A statement assembled around a
// function call (for example `... + access.GraphPredicate("repo") + ...`)
// cannot be folded, and when its UNWIND and its RETURN sit in different
// literals the scan cannot see the shape; the #6782 differential oracle covers
// those statements at runtime.
func scanFileUnwindAliasHits(fset *token.FileSet, relPath string, file *ast.File) []string {
	identMap := buildFileConstIdentMap(file)
	var hits []string
	record := func(pos token.Pos, value string) {
		if hasUnwindVariableReusedAsReturnAlias(value) {
			hits = append(hits, fmt.Sprintf("%s:%d", relPath, fset.Position(pos).Line))
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.BinaryExpr:
			if node.Op != token.ADD {
				return true
			}
			value, ok := foldStringExpr(node, identMap)
			if !ok {
				return true
			}
			record(node.Pos(), value)
			return false
		case *ast.BasicLit:
			if node.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(node.Value)
			if err != nil {
				return true
			}
			record(node.Pos(), value)
		}
		return true
	})
	return hits
}

// TestNoUnwindVariableReusedAsReturnAliasAcrossRepo is the repo-wide static
// guard for the NornicDB v1.3.3 X9 defect recorded in
// docs/internal/evidence/6786-nornicdb-400-409-exposure.md.
func TestNoUnwindVariableReusedAsReturnAliasAcrossRepo(t *testing.T) {
	t.Parallel()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	goModuleRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
	fset := token.NewFileSet()
	var hits []string
	scanned := 0
	for _, top := range []string{"cmd", "internal"} {
		root := filepath.Join(goModuleRoot, top)
		walkErr := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			scanned++
			file, parseErr := parser.ParseFile(fset, path, nil, 0)
			if parseErr != nil {
				return fmt.Errorf("parse %s: %w", path, parseErr)
			}
			relPath, relErr := filepath.Rel(goModuleRoot, path)
			if relErr != nil {
				return relErr
			}
			hits = append(hits, scanFileUnwindAliasHits(fset, relPath, file)...)
			return nil
		})
		if walkErr != nil {
			t.Fatalf("filepath.Walk(%s): %v", root, walkErr)
		}
	}
	if scanned == 0 {
		t.Fatal("scanned zero .go files; the scan is broken, not proof there is nothing to check")
	}
	if len(hits) > 0 {
		t.Fatalf(
			"found %d Cypher string(s) that reuse an UNWIND variable's name as a RETURN alias after a MATCH. "+
				"NornicDB v1.3.3 names that column after the first UNWIND value instead, so the reader finds no "+
				"such column. Rename the UNWIND variable (for example `UNWIND $repo_ids AS requested_repo_id`). "+
				"Violations:\n  %s",
			len(hits), strings.Join(hits, "\n  "),
		)
	}
}
