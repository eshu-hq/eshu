// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// md5CallPattern matches a SQL md5() function call. OpenSSL FIPS rejects the
// MD5 digest, so any production SQL calling md5() fails on FIPS-enabled
// PostgreSQL (#6753); production SQL must use sha256 instead. The word
// boundary keeps identifiers such as nomd5x( out, and the case-insensitive
// flag keeps MD5 ( out. Go-side crypto/md5 use is out of scope: import paths
// are string literals but contain no call paren and never match, and
// selector calls such as md5.New() never appear in a literal.
var md5CallPattern = regexp.MustCompile(`(?i)\bmd5\s*\(`)

// md5SweepGoRoots are the production trees, relative to the go/ module root,
// whose non-test Go string literals can carry SQL.
var md5SweepGoRoots = []string{"."}

// md5SweepSQLFiles are the shipped schema-SQL surfaces, relative to the
// repository root, that execute as SQL. Testdata, fixtures, docs evidence,
// and script proof helpers are not shipped schema and stay out of scope.
var md5SweepSQLFiles = []string{
	"go/internal/storage/postgres/migrations",
	"schema/data-plane/postgres",
}

// md5SweepMinLiterals is the floor on Go string literals the sweep must
// examine; the tree held well above this when the gate landed. It keeps a
// broken root or walker from passing vacuously.
const md5SweepMinLiterals = 1000

// md5CallSitesInLiterals returns the literals that call SQL md5().
func md5CallSitesInLiterals(literals []string) []string {
	var sites []string
	for _, literal := range literals {
		if md5CallPattern.MatchString(literal) {
			sites = append(sites, literal)
		}
	}
	return sites
}

// md5Literal is one Go string value the sweep examined, with its
// repo-root-relative file:line origin. Every occurrence is recorded, so
// identical values in different files report every site.
type md5Literal struct {
	value  string
	origin string
}

// foldMD5StringLiteral returns the value of a string literal, a
// package-level string constant resolve knows, or a + chain of them. Shapes
// it cannot fold report false so the walk descends and scans the fragments
// individually instead of dropping them.
func foldMD5StringLiteral(n ast.Node, resolve func(string) (string, bool)) (string, bool) {
	switch x := n.(type) {
	case *ast.BasicLit:
		if x.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(x.Value)
		return s, err == nil
	case *ast.Ident:
		return resolve(x.Name)
	case *ast.ParenExpr:
		return foldMD5StringLiteral(x.X, resolve)
	case *ast.BinaryExpr:
		if x.Op != token.ADD {
			return "", false
		}
		l, lok := foldMD5StringLiteral(x.X, resolve)
		r, rok := foldMD5StringLiteral(x.Y, resolve)
		return l + r, lok && rok
	}
	return "", false
}

// md5ParsedFile is one parsed production Go file awaiting the const and
// literal passes.
type md5ParsedFile struct {
	path string
	dir  string
	fset *token.FileSet
	file *ast.File
}

// collectGoStringLiterals parses every non-test .go file under root and
// returns every string value with its origin. A literal built from
// package-level string constants (prefix + "SELECT md5(...)") is folded
// first, so the sweep reads the whole statement the executor sees — the same
// shape as the sibling sweepIndexedWrites in
// go/internal/graph/index_key_guard_sweep_test.go.
func collectGoStringLiterals(t *testing.T, repoRoot, root string) []md5Literal {
	t.Helper()
	var files []md5ParsedFile
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		fileSet := token.NewFileSet()
		parsed, err := parser.ParseFile(fileSet, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		files = append(files, md5ParsedFile{path: path, dir: filepath.Dir(path), fset: fileSet, file: parsed})
		return nil
	})
	if err != nil {
		t.Fatalf("walk Go sources under %s: %v", root, err)
	}

	consts := map[string]map[string]string{} // package dir -> const name -> value
	resolver := func(dir string) func(string) (string, bool) {
		return func(name string) (string, bool) { v, ok := consts[dir][name]; return v, ok }
	}
	for pass := 0; pass < 6; pass++ {
		for _, pf := range files {
			for _, decl := range pf.file.Decls {
				gen, ok := decl.(*ast.GenDecl)
				if !ok || gen.Tok != token.CONST {
					continue
				}
				for _, spec := range gen.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok || len(vs.Names) != 1 || len(vs.Values) != 1 {
						continue
					}
					if text, ok := foldMD5StringLiteral(vs.Values[0], resolver(pf.dir)); ok {
						if consts[pf.dir] == nil {
							consts[pf.dir] = map[string]string{}
						}
						consts[pf.dir][vs.Names[0].Name] = text
					}
				}
			}
		}
	}

	relOrigin := func(path string, pos token.Pos, fset *token.FileSet) string {
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			t.Fatalf("relativize %s: %v", path, err)
		}
		return rel + ":" + strconv.Itoa(fset.Position(pos).Line)
	}
	var literals []md5Literal
	for _, pf := range files {
		ast.Inspect(pf.file, func(node ast.Node) bool {
			if _, isIdent := node.(*ast.Ident); isIdent {
				return true
			}
			value, ok := foldMD5StringLiteral(node, resolver(pf.dir))
			if !ok {
				return true
			}
			literals = append(literals, md5Literal{value: value, origin: relOrigin(pf.path, node.Pos(), pf.fset)})
			// The folded parent covers its fragments; prune them so one
			// statement reports once at its outermost site.
			return false
		})
	}
	return literals
}

// collectShippedSQLFiles returns the .sql files under the shipped schema
// roots. The walk is recursive so a .sql file added under a new subdirectory
// cannot silently escape the gate.
func collectShippedSQLFiles(t *testing.T, repoRoot string, rels []string) []string {
	t.Helper()
	var files []string
	for _, rel := range rels {
		root := filepath.Join(repoRoot, rel)
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
				return nil
			}
			files = append(files, path)
			return nil
		})
		if err != nil {
			t.Fatalf("walk shipped SQL root %s: %v", root, err)
		}
	}
	sort.Strings(files)
	return files
}

func repoRootFromTest(t *testing.T) string {
	t.Helper()
	// This file lives at go/internal/storage/postgres; the go/ module root
	// is three levels up and the repository root one more.
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}
	return filepath.Clean(filepath.Join(dir, "..", "..", "..", ".."))
}

// md5Violation formats one violation with a repo-root-relative site, so
// failure output is stable across machines.
func md5Violation(t *testing.T, repoRoot, path, site string) string {
	t.Helper()
	rel, err := filepath.Rel(repoRoot, path)
	if err != nil {
		t.Fatalf("relativize %s: %v", path, err)
	}
	return rel + ": " + site
}

// TestProductionSQLHasNoMD5Calls sweeps production Go string literals and
// shipped schema SQL for md5() calls, which fail on FIPS-enabled PostgreSQL
// (#6753). Tests, fixtures, and evidence files may still use md5(); only
// shipped SQL is in scope.
func TestProductionSQLHasNoMD5Calls(t *testing.T) {
	repoRoot := repoRootFromTest(t)
	goRoot := filepath.Join(repoRoot, "go")
	var literals []md5Literal
	for _, rel := range md5SweepGoRoots {
		literals = append(literals, collectGoStringLiterals(t, repoRoot, filepath.Join(goRoot, rel))...)
	}
	if len(literals) < md5SweepMinLiterals {
		t.Fatalf("examined %d Go string literals, want at least %d", len(literals), md5SweepMinLiterals)
	}
	t.Logf("sweep examined %d Go string literals", len(literals))
	var violations []string
	for _, lit := range literals {
		if md5CallPattern.MatchString(lit.value) {
			violations = append(violations, "go literal "+lit.origin+": "+lit.value)
		}
	}
	for _, path := range collectShippedSQLFiles(t, repoRoot, md5SweepSQLFiles) {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read shipped SQL %s: %v", path, err)
		}
		for _, site := range md5CallSitesInLiterals([]string{string(raw)}) {
			lines := strings.Split(site, "\n")
			for i, line := range lines {
				if md5CallPattern.MatchString(line) {
					violations = append(violations, md5Violation(t, repoRoot, path, strconv.Itoa(i+1)+": "+strings.TrimSpace(line)))
				}
			}
		}
	}
	if len(violations) > 0 {
		t.Fatalf("production SQL md5() calls (FIPS-incompatible, #6753):\n%s", strings.Join(violations, "\n"))
	}
}

// TestMD5SweepFlagsSeededViolation pins the detector's sensitivity: seeded
// production-shaped violations fail, and lookalikes pass.
func TestMD5SweepFlagsSeededViolation(t *testing.T) {
	violations := md5CallSitesInLiterals([]string{
		"SELECT md5(payload::text)",
		"MIN(MD5 (fingerprint))",
		"encode(md5(convert_to(key, 'UTF8')), 'hex')",
		"SELECT\n  md5(a || b)\nFROM t",
	})
	if len(violations) != 4 {
		t.Fatalf("seeded violations flagged = %d, want 4", len(violations))
	}
	clean := md5CallSitesInLiterals([]string{
		"SELECT sha256(convert_to(payload::text, 'UTF8'))",
		"SELECT nomd5x(payload)",
		"crypto/md5",
		"md5 without a call",
	})
	if len(clean) != 0 {
		t.Fatalf("clean literals flagged = %q, want none", clean)
	}
}

// TestMD5FoldResolvesConstsAndChains pins the const/+ folding behind review
// P2: package-level string constants and + chains resolve to the statement
// the executor sees, while unknown identifiers and non-string shapes report
// false so the walk still scans their fragments.
func TestMD5FoldResolvesConstsAndChains(t *testing.T) {
	resolve := func(name string) (string, bool) {
		if name == "sqlPrefix" {
			return "SELECT md5(", true
		}
		return "", false
	}
	str := func(s string) *ast.BasicLit {
		return &ast.BasicLit{Kind: token.STRING, Value: strconv.Quote(s)}
	}
	concat := func(x, y ast.Expr) *ast.BinaryExpr {
		return &ast.BinaryExpr{Op: token.ADD, X: x, Y: y}
	}
	if got, ok := foldMD5StringLiteral(str("SELECT md5(a)"), resolve); !ok || got != "SELECT md5(a)" {
		t.Fatalf("plain literal folds to %q, %v; want it verbatim with true", got, ok)
	}
	if got, ok := foldMD5StringLiteral(&ast.Ident{Name: "sqlPrefix"}, resolve); !ok || got != "SELECT md5(" {
		t.Fatalf("known const folds to %q, %v; want the resolved value with true", got, ok)
	}
	if got, ok := foldMD5StringLiteral(concat(&ast.Ident{Name: "sqlPrefix"}, str("a))")), resolve); !ok || got != "SELECT md5(a))" {
		t.Fatalf("const + literal folds to %q, %v; want the joined statement with true", got, ok)
	}
	if got, ok := foldMD5StringLiteral(&ast.Ident{Name: "unknown"}, resolve); ok || got != "" {
		t.Fatalf("unknown ident folds to %q, %v; want empty with false", got, ok)
	}
	if got, ok := foldMD5StringLiteral(&ast.BasicLit{Kind: token.INT, Value: "42"}, resolve); ok || got != "" {
		t.Fatalf("int literal folds to %q, %v; want empty with false", got, ok)
	}
	if got, ok := foldMD5StringLiteral(&ast.BinaryExpr{Op: token.SUB, X: str("a"), Y: str("b")}, resolve); ok || got != "" {
		t.Fatalf("non-add chain folds to %q, %v; want empty with false", got, ok)
	}
}

// TestCollectShippedSQLFilesRecurses pins the recursive collection behind
// review P2: a .sql file under a new subdirectory of a shipped root is
// collected, not silently skipped.
func TestCollectShippedSQLFilesRecurses(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "migrations", "backfill")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested fixture: %v", err)
	}
	top := filepath.Join(root, "migrations", "top.sql")
	deep := filepath.Join(nested, "deep.sql")
	for _, path := range []string{top, deep} {
		if err := os.WriteFile(path, []byte("SELECT 1;\n"), 0o600); err != nil {
			t.Fatalf("write fixture %s: %v", path, err)
		}
	}
	files := collectShippedSQLFiles(t, root, []string{"migrations"})
	if len(files) != 2 || files[0] != deep || files[1] != top {
		t.Fatalf("collected = %q, want [%q %q] sorted", files, deep, top)
	}
}
