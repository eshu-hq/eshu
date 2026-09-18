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

// lineLedBooleanPattern matches a Cypher boolean operator (AND, OR, XOR) whose
// preceding character is a newline or a tab instead of a space. NornicDB
// v1.3.3 mis-evaluates a WHERE containing that shape (#6786, X4):
// `MATCH (n:Workload) WHERE n.id = $x\n\tAND n.repo_id = $r` returns no rows
// where Neo4j returns the matching row, and with a pattern-predicate disjunct
// it drops the first condition and returns extra rows instead. The keyword is
// only recognised when a space character precedes it, so `\n  AND` and
// `\n\t AND` are correct. The \b after the keyword keeps identifiers such as
// ORDER, ORIGIN or ANDROID from matching.
var lineLedBooleanPattern = regexp.MustCompile(`[\n\t](?:AND|OR|XOR)\b`)

// cypherMarkerPattern decides whether a string is Cypher rather than SQL, so
// the scan does not flag Postgres statements (which NornicDB never parses).
// A Cypher statement has a MATCH/MERGE clause opening a pattern, a
// relationship arrow, or a named $parameter. Eshu's SQL uses positional $1
// parameters, which the `\$[A-Za-z_]` branch excludes.
var cypherMarkerPattern = regexp.MustCompile(`(?i)\b(?:MATCH|MERGE)\s*\(|\]->|<-\[|\$[A-Za-z_]`)

// hasLineLedBooleanOperator reports whether value looks like Cypher and
// contains a boolean operator directly after a newline or a tab.
func hasLineLedBooleanOperator(value string) bool {
	return lineLedBooleanPattern.MatchString(value) && cypherMarkerPattern.MatchString(value)
}

// scanFileLineLedBooleanHits returns "relPath:line" for every string in file
// that hasLineLedBooleanOperator flags. It folds "+" concatenation and
// same-file string identifiers with foldStringExpr / buildFileConstIdentMap
// (see merge_then_create_repo_scan_test.go for their exact reach), then scans
// every remaining literal on its own. A fragment that begins with the keyword
// and relies on the caller's template for the preceding whitespace (for
// example "AND x" appended after a format string ending in "\n\t") is not
// visible to a static scan; the #6782 differential oracle covers that at
// runtime.
func scanFileLineLedBooleanHits(fset *token.FileSet, relPath string, file *ast.File) []string {
	identMap := buildFileConstIdentMap(file)
	var hits []string
	record := func(pos token.Pos, value string) {
		if hasLineLedBooleanOperator(value) {
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

// scanForLineLedBooleanOperators walks every non-_test.go file under go/cmd
// and go/internal and returns every scanFileLineLedBooleanHits site.
// scannedFiles lets the caller tell a clean scan from one that walked nothing.
func scanForLineLedBooleanOperators(t *testing.T) (hits []string, scannedFiles int) {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	goModuleRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
	fset := token.NewFileSet()
	for _, top := range []string{"cmd", "internal"} {
		root := filepath.Join(goModuleRoot, top)
		walkErr := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			scannedFiles++
			file, parseErr := parser.ParseFile(fset, path, nil, 0)
			if parseErr != nil {
				return fmt.Errorf("parse %s: %w", path, parseErr)
			}
			relPath, relErr := filepath.Rel(goModuleRoot, path)
			if relErr != nil {
				return relErr
			}
			hits = append(hits, scanFileLineLedBooleanHits(fset, relPath, file)...)
			return nil
		})
		if walkErr != nil {
			t.Fatalf("filepath.Walk(%s): %v", root, walkErr)
		}
	}
	return hits, scannedFiles
}

// TestNoLineLedBooleanOperatorsInCypherAcrossRepo is the repo-wide static
// guard for the NornicDB v1.3.3 WHERE-parsing defect recorded as X4 in
// docs/internal/evidence/6786-nornicdb-400-409-exposure.md. It fails when any
// production Cypher string puts AND, OR or XOR directly after a newline or a
// tab, the shape gofmt-indented raw strings produce naturally.
func TestNoLineLedBooleanOperatorsInCypherAcrossRepo(t *testing.T) {
	t.Parallel()

	hits, scannedFiles := scanForLineLedBooleanOperators(t)
	if scannedFiles == 0 {
		t.Fatal("scanned zero .go files under go/cmd and go/internal; the scan is broken, not proof there is nothing to check")
	}
	if len(hits) > 0 {
		t.Fatalf(
			"found %d Cypher string(s) with AND/OR/XOR directly after a newline or tab. "+
				"NornicDB v1.3.3 mis-evaluates such a WHERE (no rows, or a dropped condition). "+
				"Put a space before the keyword (for example \"\\n\\t\\t  AND ...\") or keep the "+
				"predicate on one line. Violations:\n  %s",
			len(hits), strings.Join(hits, "\n  "),
		)
	}
}
