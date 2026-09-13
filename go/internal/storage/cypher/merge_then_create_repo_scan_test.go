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

// pathBindingFragment is the regex fragment shared by mergeOpenPattern and
// createClausePattern for an optional named-path binding -- Cypher's
// `path = (...)` form, where a MERGE or CREATE clause names the whole path it
// opens. It accepts a plain identifier or a backtick-quoted one, then "=".
// Both surrounding patterns require this fragment (when present) to sit
// directly between the keyword and its opening paren, with no other text in
// between.
const pathBindingFragment = "(?:`[^`]+`|[A-Za-z_][A-Za-z0-9_]*)\\s*=\\s*"

// mergeOpenPattern and createClausePattern locate the MERGE and CREATE clause
// keywords hasNodeMergeThenCreate scans for. Both are case-insensitive with a
// \b word boundary on BOTH sides of the keyword: the leading \b means e.g.
// "UNMERGED" or "RECREATE" never count as the keyword, and the trailing \b is
// required too -- without it, the keyword would happily match as a PREFIX of
// a longer identifier, so `r.created_at = ($now)` read as CREATE (the first
// six letters of "created_at") immediately followed by the "d_at" path
// binding and "=", a false positive fixed by adding the trailing \b (the
// same fix applies to mergeOpenPattern, where `a.merged_at = (...)` was
// misread as MERGE plus a "d_at" path binding). \s* tolerates any amount of
// whitespace (including a newline) between the keyword and its opening
// paren, so "MERGE(", "merge  (", and "MERGE\n(" all match the same as
// "MERGE (". Both also accept an optional pathBindingFragment between the
// keyword and "(", so a named-path clause (`MERGE p = (...)`,
// `CREATE p = (...)`, `CREATE p=(...)`) matches the same as the plain form.
// createClausePattern in particular never matches "ON CREATE SET" -- there,
// after CREATE and its whitespace, "SET" is not itself followed by "=", and
// no "(" follows either, so neither the optional-path branch nor the bare
// "(" branch matches. The same holds for "ON MATCH SET" against
// mergeOpenPattern: the scan does reach that text, it just never matches it,
// because mergeOpenPattern only recognizes the literal keyword "MERGE", not
// "MATCH".
var (
	mergeOpenPattern    = regexp.MustCompile("(?i)\\bMERGE\\b\\s*(?:" + pathBindingFragment + ")?\\(")
	createClausePattern = regexp.MustCompile("(?i)\\bCREATE\\b\\s*(?:" + pathBindingFragment + ")?\\(")
)

// hasNodeMergeThenCreate flags the exact statement class orneryd/NornicDB#359
// silently drops: a statement that opens a node pattern with MERGE ( and
// later, in the SAME statement, adds a real CREATE clause. NornicDB's
// executeMultipleMerges splitter (splitMultipleMerges, pkg/cypher/merge.go on
// the NornicDB side) does not treat CREATE as a clause boundary: the trailing
// CREATE text is glued onto the second MERGE's segment and parsed as part of
// it, and the segment loop has no CREATE branch at all, so neither the
// second node MERGE nor the CREATE executes. The canonical repro:
//
//	MERGE (s:Workload {id:$s}) MERGE (t:Workload {id:$t}) CREATE (s)-[:DEPENDS_ON]->(t)
//
// reports success with 1 node written and 0 relationships, where Neo4j
// reports 2 nodes and 1 relationship. See
// docs/public/reference/nornicdb-write-shape-pitfalls.md ("Pitfall: A Node
// MERGE Followed By CREATE In One Statement Silently Drops The Second MERGE
// And The CREATE") for the full writeup and the shapes that avoid it -- only
// a relationship MERGE is idempotent enough for Eshu writers; the others
// avoid the NornicDB drop but run an unconditional CREATE.
//
// value is split on the literal character ';' before scanning, so a real
// CREATE clause in a DIFFERENT statement never counts -- only a CREATE that
// textually follows a MERGE( within the same statement segment does. This
// split has no Cypher comment or string-literal awareness: a ';' inside a
// Cypher `//` comment or inside a quoted property value ends the "statement"
// here even though it is not a real statement boundary, which can hide a
// violation that spans it (undercounts, never overcounts).
//
// This function scans one already-flattened string; it is not itself a
// Cypher parser. scanFileHits is the layer that flattens a "+"-concatenated
// Cypher expression into one string before calling this, and that layer's
// doc comment states the full, current list of what still cannot be seen.
func hasNodeMergeThenCreate(value string) bool {
	for _, statement := range strings.Split(value, ";") {
		mergeLoc := mergeOpenPattern.FindStringIndex(statement)
		if mergeLoc == nil {
			continue
		}
		if createClausePattern.MatchString(statement[mergeLoc[1]:]) {
			return true
		}
	}
	return false
}

// mergeThenCreateAllowlist names "path:line" locations (relative to the go
// module root, as produced by scanFileHits) that legitimately contain the
// MERGE-then-CREATE textual shape without hitting the NornicDB drop -- for
// example a statement gated behind a backend branch that never runs against
// NornicDB. Keep this list empty if possible; add an entry only with a
// comment proving why that specific statement is safe on every backend Eshu
// runs.
var mergeThenCreateAllowlist = map[string]bool{}

// buildFileConstIdentMap collects every PACKAGE-LEVEL const/var identifier in
// file whose declared value is a single string literal, keyed by identifier
// name. It walks only file.Decls, the file's top-level declarations, so a
// function-local const or var of the identical shape (declared inside a
// function body as an *ast.DeclStmt) is never collected and never resolves --
// foldStringExpr's lookup simply misses it, which fails the fold and falls
// back to scanning that function-local literal on its own. It is also
// intentionally shallow: an identifier whose value is itself a concatenation
// (`const chainB = chainA + "..."`), a function call, or anything else
// non-literal is left out, not partially resolved. This is the "cheap"
// same-file identifier resolution foldStringExpr uses; resolving identifiers
// declared in another file or package is out of scope.
func buildFileConstIdentMap(file *ast.File) map[string]string {
	identMap := make(map[string]string)
	for _, decl := range file.Decls {
		genDecl, isGenDecl := decl.(*ast.GenDecl)
		if !isGenDecl || (genDecl.Tok != token.CONST && genDecl.Tok != token.VAR) {
			continue
		}
		for _, spec := range genDecl.Specs {
			valueSpec, isValueSpec := spec.(*ast.ValueSpec)
			if !isValueSpec {
				continue
			}
			for i, ident := range valueSpec.Names {
				if i >= len(valueSpec.Values) {
					continue
				}
				lit, isLit := valueSpec.Values[i].(*ast.BasicLit)
				if !isLit || lit.Kind != token.STRING {
					continue
				}
				value, err := strconv.Unquote(lit.Value)
				if err != nil {
					continue
				}
				identMap[ident.Name] = value
			}
		}
	}
	return identMap
}

// foldStringExpr attempts to resolve expr to a single string value by
// folding string-literal "+" concatenation and package-level identifiers
// found in identMap (see buildFileConstIdentMap). It returns ok=false the
// moment it hits anything it cannot resolve statically -- a function call
// (fmt.Sprintf and friends), a struct/package selector, a non-string
// operand, or an identifier not in identMap -- so a caller never scans a
// partially-folded, misleading string. This closes the gap a plain
// per-literal scan has on Cypher built as
// `mergeAndCreateClause := mergePart + "CREATE (" + ... + ")"`.
//
// Known gaps, beyond the identifier-resolution ones on buildFileConstIdentMap:
//   - `+=` augmented assignment is a different AST shape (*ast.AssignStmt)
//     that this function never sees, so `q := mergePart; q += "CREATE (...)"`
//     is invisible to the fold (the standalone "CREATE (...)" literal alone
//     has no MERGE, so it is not flagged either).
//   - Because "+" is left-associative and every leaf of a BinaryExpr must
//     resolve for that node to fold, an unresolvable leaf positioned BEFORE
//     the MERGE/CREATE literal pair (e.g. `nonLiteral() + "MERGE (...) " +
//     "CREATE (...)"`, which parses as `(nonLiteral() + "MERGE (...) ") +
//     "CREATE (...)"`) prevents the literal pair from ever forming its own
//     foldable subtree, so the pair is missed. The same unresolvable leaf
//     placed AFTER a foldable literal pair does not hide it: ast.Inspect
//     walks pre-order, so scanFileHits visits and tries to fold the OUTER
//     BinaryExpr node first; that fold fails (the unresolvable leaf is still
//     part of it), the walk returns true and descends, and only then does it
//     reach the INNER `"MERGE (...) " + "CREATE (...)"` pair, which folds
//     successfully on its own and is caught.
func foldStringExpr(expr ast.Expr, identMap map[string]string) (value string, ok bool) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", false
		}
		v, err := strconv.Unquote(e.Value)
		if err != nil {
			return "", false
		}
		return v, true
	case *ast.ParenExpr:
		return foldStringExpr(e.X, identMap)
	case *ast.Ident:
		v, found := identMap[e.Name]
		return v, found
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return "", false
		}
		left, leftOK := foldStringExpr(e.X, identMap)
		if !leftOK {
			return "", false
		}
		right, rightOK := foldStringExpr(e.Y, identMap)
		if !rightOK {
			return "", false
		}
		return left + right, true
	default:
		return "", false
	}
}

// scanFileHits inspects one already-parsed file for hasNodeMergeThenCreate
// matches and returns their "relPath:line" sites, skipping anything named in
// mergeThenCreateAllowlist. It scans two shapes:
//   - a "+"-concatenation expression -- including same-file identifiers that
//     resolve to a single string literal via buildFileConstIdentMap -- is
//     folded into one string with foldStringExpr and scanned once, so a
//     statement split across `mergePart + "CREATE (...)"` is visible even
//     though neither half alone contains the banned shape;
//   - every remaining string literal not already covered by a folded
//     expression is scanned on its own.
//
// What this still cannot see (the complete, current list):
//   - an identifier resolved from another file or package;
//   - a function-local const or var, even one of the exact same shape as a
//     package-level one (buildFileConstIdentMap only walks file.Decls);
//   - `+=` augmented-assignment concatenation;
//   - an unresolvable concatenation leaf (a function call, a struct field, a
//     package selector) positioned BEFORE the MERGE/CREATE literal pair in a
//     "+" chain -- left-associativity means that leaf's fold failure can
//     prevent the pair from ever forming its own foldable subtree; the same
//     leaf positioned after a foldable pair does not hide it (see
//     foldStringExpr's doc comment for why).
//
// Three related limits are narrower than they sound, and are precisely
// stated on buildFileConstIdentMap and hasNodeMergeThenCreate rather than
// repeated loosely here: a fmt.Sprintf format string that alone holds the
// whole shape IS caught by the per-literal scan -- only a shape assembled
// across the format string AND its arguments is invisible; a const/var whose
// own declared value is itself a concatenation IS folded and scanned at its
// own declaration -- it is missed only when used as an operand inside
// ANOTHER "+" chain; and a ';' inside a Cypher comment or a quoted string
// property value hides a violation only when it falls between the LAST
// MERGE( and the CREATE( -- one earlier in the statement is still caught.
//
// A clean scan means "no textually-visible violation," not "this file's
// Cypher is provably safe."
func scanFileHits(fset *token.FileSet, relPath string, file *ast.File) []string {
	identMap := buildFileConstIdentMap(file)
	var hits []string
	record := func(pos token.Pos, value string) {
		if !hasNodeMergeThenCreate(value) {
			return
		}
		site := fmt.Sprintf("%s:%d", relPath, fset.Position(pos).Line)
		if mergeThenCreateAllowlist[site] {
			return
		}
		hits = append(hits, site)
	}

	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.BinaryExpr:
			if node.Op != token.ADD {
				return true
			}
			value, ok := foldStringExpr(node, identMap)
			if !ok {
				// Could not fold the whole chain (a non-literal, non-const
				// leaf such as a function call or a package selector) --
				// fall through so the individual BasicLit leaves this walk
				// still reaches get scanned on their own below.
				return true
			}
			record(node.Pos(), value)
			// Every leaf under this node was already covered by the fold;
			// descending further would double-count them.
			return false
		case *ast.BasicLit:
			if node.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(node.Value)
			if err != nil {
				// Raw (backtick) string literals unquote fine via
				// strconv.Unquote too; skip anything that fails rather than
				// fail the whole scan on an unrelated parse edge case.
				return true
			}
			record(node.Pos(), value)
			return true
		}
		return true
	})
	return hits
}

// scanForNodeMergeThenCreate walks every non-_test.go file under go/cmd and
// go/internal, parses it, and returns every scanFileHits match across the
// tree. scannedFiles reports how many .go files were inspected, so the
// caller can tell a clean scan from a scan that silently walked nothing.
func scanForNodeMergeThenCreate(t *testing.T) (hits []string, scannedFiles int) {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	// thisFile is go/internal/storage/cypher/merge_then_create_repo_scan_test.go;
	// the go module root is three directories up.
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
			hits = append(hits, scanFileHits(fset, relPath, file)...)
			return nil
		})
		if walkErr != nil {
			t.Fatalf("filepath.Walk(%s): %v", root, walkErr)
		}
	}
	return hits, scannedFiles
}

// TestNoNodeMergeThenCreateCyphersAcrossRepo is the repo-wide static guard
// for orneryd/NornicDB#359. It parses every non-test .go file under go/cmd
// and go/internal, collects every string literal's value (folding "+"
// concatenation first), and fails if any of them contain a node MERGE(
// pattern followed by a real CREATE( clause in the same statement -- the
// exact shape NornicDB silently drops. See hasNodeMergeThenCreate and
// scanFileHits for what this scan can and cannot see, and
// docs/public/reference/nornicdb-write-shape-pitfalls.md for the full
// writeup.
func TestNoNodeMergeThenCreateCyphersAcrossRepo(t *testing.T) {
	t.Parallel()

	hits, scannedFiles := scanForNodeMergeThenCreate(t)
	if scannedFiles == 0 {
		t.Fatal("scanned zero .go files under go/cmd and go/internal -- the scan itself is broken, not proof there is nothing to check")
	}
	if len(hits) > 0 {
		t.Fatalf(
			"found %d Cypher string(s) with a node MERGE( pattern followed by a CREATE( "+
				"clause in the same statement (orneryd/NornicDB#359: NornicDB silently drops "+
				"the CREATE clause, reporting success with fewer nodes/relationships than "+
				"requested). Never write a node MERGE followed by CREATE in one statement -- "+
				"use a relationship MERGE instead of CREATE (MERGE (s) MERGE (t) MERGE "+
				"(s)-[:REL]->(t)) to fix it AND keep it idempotent. Violations:\n  %s",
			len(hits), strings.Join(hits, "\n  "),
		)
	}
}

// TestHasNodeMergeThenCreate and TestScanFileHitsFoldsStringConcatenation,
// the unit-level proofs for hasNodeMergeThenCreate and scanFileHits, live in
// the sibling file merge_then_create_unit_test.go -- split out to keep this
// file under the repo's 500-line cap.
