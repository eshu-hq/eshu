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

// mergeOpenPattern and createClausePattern locate the MERGE and CREATE clause
// keywords hasNodeMergeThenCreate scans for. Both are case-insensitive with
// \b word boundaries so e.g. "UNMERGED" or "RECREATE" never count as the
// keyword, and \s* tolerates any amount of whitespace (including a newline)
// between the keyword and its opening paren, so "MERGE(", "merge  (", and
// "MERGE\n(" all match the same as "MERGE (". createClausePattern requires
// CREATE be immediately followed by "(" (modulo whitespace), so it matches a
// real CREATE clause opening a node/relationship pattern and never
// "ON CREATE SET" -- there CREATE is followed by "SET", not "(".
var (
	mergeOpenPattern    = regexp.MustCompile(`(?i)\bMERGE\s*\(`)
	createClausePattern = regexp.MustCompile(`(?i)\bCREATE\s*\(`)
)

// hasNodeMergeThenCreate flags the exact statement class orneryd/NornicDB#359
// silently drops: a statement that opens a node pattern with MERGE ( and
// later, in the SAME statement, adds a real CREATE clause. NornicDB's
// executeMultipleMerges splitter (splitMultipleMerges, pkg/cypher/merge.go on
// the NornicDB side) does not treat CREATE as a clause boundary, and its loop
// has no CREATE branch, so the CREATE clause is silently never executed. The
// canonical repro:
//
//	MERGE (s:Workload {id:$s}) MERGE (t:Workload {id:$t}) CREATE (s)-[:DEPENDS_ON]->(t)
//
// reports success with 1 node written and 0 relationships, where Neo4j
// reports 2 nodes and 1 relationship. See
// docs/public/reference/nornicdb-write-shape-pitfalls.md ("Pitfall: A Node
// MERGE Followed By CREATE In One Statement Silently Drops The CREATE") for
// the full writeup and the proven-safe alternative shapes.
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
//     placed AFTER a foldable literal pair does not hide it: scanFileHits'
//     AST walk visits and re-attempts the fold at every BinaryExpr node, so
//     the inner `"MERGE (...) " + "CREATE (...)"` pair still folds and is
//     caught as its own subtree before the outer node's failed fold is even
//     reached.
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
//   - fmt.Sprintf and other runtime template assembly;
//   - an identifier resolved from another file or package;
//   - a function-local const or var, even one of the exact same shape as a
//     package-level one (buildFileConstIdentMap only walks file.Decls);
//   - `+=` augmented-assignment concatenation;
//   - a const/var whose own declared value is itself a concatenation rather
//     than a single string literal;
//   - an unresolvable concatenation leaf (a function call, a struct field, a
//     package selector) positioned BEFORE the MERGE/CREATE literal pair in a
//     "+" chain -- left-associativity means that leaf's fold failure can
//     prevent the pair from ever forming its own foldable subtree; the same
//     leaf positioned after a foldable pair does not hide it (see
//     foldStringExpr's doc comment for why);
//   - a ';' inside a Cypher comment or a quoted string property value, which
//     the plain-character statement-boundary split in hasNodeMergeThenCreate
//     cannot distinguish from a real statement terminator.
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
				"use a relationship MERGE instead of CREATE, MATCH ... MATCH ... CREATE, a "+
				"comma-pattern CREATE, or two separate statements. Violations:\n  %s",
			len(hits), strings.Join(hits, "\n  "),
		)
	}
}

// TestHasNodeMergeThenCreate is the unit-level proof of hasNodeMergeThenCreate,
// including the two mandatory false-positive exclusions ("ON CREATE SET" and
// a CREATE in a different statement) and keyword whitespace/case tolerance.
func TestHasNodeMergeThenCreate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		value string
		want  bool
	}{
		{
			name:  "canonical repro: node MERGE, node MERGE, relationship CREATE",
			value: `MERGE (s:Workload {id:$s}) MERGE (t:Workload {id:$t}) CREATE (s)-[:DEPENDS_ON]->(t)`,
			want:  true,
		},
		{
			name: "SET between the MERGEs and the CREATE does not change the result",
			value: `MERGE (s:Workload {id:$s})
MERGE (t:Workload {id:$t})
SET s.updated_at = $now
CREATE (s)-[:DEPENDS_ON]->(t)`,
			want: true,
		},
		{
			name: "a second CREATE after the first still counts",
			value: `MERGE (s:Workload {id:$s})
MERGE (t:Workload {id:$t})
CREATE (s)-[:DEPENDS_ON]->(t)
CREATE (t)-[:USED_BY]->(s)`,
			want: true,
		},
		{
			name:  "single MERGE followed directly by CREATE",
			value: `MERGE (n:Repository {id:$id}) CREATE (n)-[:HAS_TAG]->(:Tag {name:$tag})`,
			want:  true,
		},
		{
			name:  "ON CREATE SET is a normal MERGE action, not a CREATE clause",
			value: `MERGE (n:Repository {id:$id}) ON CREATE SET n.first_seen = $now`,
			want:  false,
		},
		{
			name:  "ON MATCH SET is a normal MERGE action too",
			value: `MERGE (n:Repository {id:$id}) ON MATCH SET n.last_seen = $now`,
			want:  false,
		},
		{
			name: "ON CREATE SET beside a real CREATE clause still flags the real clause",
			value: `MERGE (n:Repository {id:$id}) ON CREATE SET n.first_seen = $now
CREATE (n)-[:HAS_TAG]->(:Tag {name:$tag})`,
			want: true,
		},
		{
			name:  "CREATE in a different statement (after a semicolon) does not count",
			value: `MERGE (n:Repository {id:$id}) SET n.last_seen = $now; CREATE (:AuditEvent {id:$eventId})`,
			want:  false,
		},
		{
			name:  "CREATE before the MERGE, same statement, does not count",
			value: `CREATE (:AuditEvent {id:$eventId}) MERGE (n:Repository {id:$id})`,
			want:  false,
		},
		{
			name: "relationship MERGE instead of CREATE is the safe shape",
			value: `MERGE (s:Workload {id:$s})
MERGE (t:Workload {id:$t})
MERGE (s)-[:DEPENDS_ON]->(t)`,
			want: false,
		},
		{
			name:  "MATCH ... MATCH ... CREATE (no MERGE at all) is the safe shape",
			value: `MATCH (s:Workload {id:$s}) MATCH (t:Workload {id:$t}) CREATE (s)-[:DEPENDS_ON]->(t)`,
			want:  false,
		},
		{
			name:  "comma-pattern CREATE with no MERGE is the safe shape",
			value: `MATCH (s:Workload {id:$s}) MATCH (t:Workload {id:$t}) CREATE (s)-[:DEPENDS_ON]->(t), (t)-[:USED_BY]->(s)`,
			want:  false,
		},
		{
			name:  "no MERGE at all",
			value: `MATCH (n:Repository {id:$id}) RETURN n`,
			want:  false,
		},
		{
			name:  "MERGE with no CREATE anywhere",
			value: `MERGE (n:Repository {id:$id}) RETURN n`,
			want:  false,
		},
		{
			name:  "keyword-like substrings never match (UNMERGED / RECREATE)",
			value: `// UNMERGED (n) RECREATE (m)`,
			want:  false,
		},
		{
			name:  "lowercase keywords with no space before the paren still match",
			value: `merge(s:Workload {id:$s}) merge(t:Workload {id:$t}) create(s)-[:DEPENDS_ON]->(t)`,
			want:  true,
		},
		{
			name:  "mixed-case keywords with extra internal spaces still match",
			value: `Merge  (s:Workload {id:$s}) MeRgE  (t:Workload {id:$t}) CrEaTe  (s)-[:DEPENDS_ON]->(t)`,
			want:  true,
		},
		{
			name: "a newline between the keyword and its opening paren still matches",
			value: `MERGE
(s:Workload {id:$s}) MERGE
(t:Workload {id:$t}) CREATE
(s)-[:DEPENDS_ON]->(t)`,
			want: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := hasNodeMergeThenCreate(tc.value); got != tc.want {
				t.Fatalf("hasNodeMergeThenCreate(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

// TestScanFileHitsFoldsStringConcatenation proves scanFileHits closes the
// concatenation gap a plain per-literal scan has: a MERGE-then-CREATE
// statement built as `mergePart + "CREATE (...)"` (a package-level identifier
// resolved by buildFileConstIdentMap) or as two adjacent literals joined by
// "+" is caught even though neither half alone contains the banned shape. It
// also proves several things must NOT be flagged: a split literal whose
// folded text is the safe "ON CREATE SET" shape, a statement assembled by
// fmt.Sprintf, a function-local const of the identical shape as the caught
// package-level one, and an unresolvable leaf (a package selector) that DOES
// carry the full MERGE/CREATE text but sits to the left of it in the "+"
// chain -- see foldStringExpr's doc comment for why left-position matters.
// Asserts the exact set of hit lines, not just a count, so a false hit
// silently replacing a missed real one cannot pass unnoticed.
func TestScanFileHitsFoldsStringConcatenation(t *testing.T) {
	t.Parallel()

	const src = `package fixture

import "fmt"

const mergePart = "MERGE (s:Workload {id:$s}) MERGE (t:Workload {id:$t}) "

var viaSameFileConstIdent = mergePart + "CREATE (s)-[:DEPENDS_ON]->(t)"

var viaTwoLiterals = "MERGE (s:Workload {id:$s}) MERGE (t:Workload {id:$t}) " + "CREATE (s)-[:DEPENDS_ON]->(t)"

var viaSplitOnCreateSetIsSafe = "MERGE (n:Repository {id:$id}) ON CREATE SET n.first_seen = " + "$now"

var viaSprintfIsInvisible = fmt.Sprintf("MERGE (s:Workload {id:%s}) MERGE (t:Workload {id:%s}) %s (s)-[:DEPENDS_ON]->(t)", "a", "b", "CREATE")

func viaFunctionLocalConstIsInvisible() string {
	const localMergePart = "MERGE (s:Workload {id:$s}) MERGE (t:Workload {id:$t}) "
	return localMergePart + "CREATE (s)-[:DEPENDS_ON]->(t)"
}

var viaUnresolvedLeafBeforeTheLiteralsIsInvisible = unknownPkg.Fragment + "MERGE (s:Workload {id:$s}) MERGE (t:Workload {id:$t}) " + "CREATE (s)-[:DEPENDS_ON]->(t)"
`

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "fixture.go", src, 0)
	if err != nil {
		t.Fatalf("parse fixture source: %v", err)
	}

	hits := scanFileHits(fset, "fixture.go", file)
	want := []string{"fixture.go:7", "fixture.go:9"} // viaSameFileConstIdent, viaTwoLiterals
	if !slicesEqual(hits, want) {
		t.Fatalf(
			"scanFileHits found %v, want exactly %v: viaSameFileConstIdent (line 7) and "+
				"viaTwoLiterals (line 9) must be caught by folding; every other candidate must "+
				"NOT be -- viaSplitOnCreateSetIsSafe (folded text is a normal MERGE action, not "+
				"a CREATE clause), viaSprintfIsInvisible (runtime template assembly), "+
				"viaFunctionLocalConstIsInvisible (only package-level consts resolve), and "+
				"viaUnresolvedLeafBeforeTheLiteralsIsInvisible (an unresolvable leaf before the "+
				"literal pair prevents the pair from ever forming its own foldable subtree)",
			hits, want,
		)
	}
}

// slicesEqual reports whether a and b contain the same strings in the same
// order.
func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
