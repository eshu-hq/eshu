// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"

	linksfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
)

// advisoryCall matches the start of any advisory-lock function call.
var advisoryCall = regexp.MustCompile(`pg_(?:try_)?advisory(?:_xact)?_lock(?:_shared)?\s*\(`)

// lockClass is the class (first key) of one two-integer advisory lock site in
// the tree, resolved to what PostgreSQL will compute: an integer, or a string
// the site passes to hashtext().
type lockClass struct {
	site     string
	integer  *int64
	hashtext *string
}

// lockSite is one two-integer advisory call found in a SQL text: its first
// argument, and the Go context needed to resolve a bind parameter.
type lockSite struct {
	pos      string
	firstArg string
	constRef string        // name of the Go const holding the SQL, if any
	literal  *ast.BasicLit // the Go string literal holding the SQL
	dir      string
}

// callArgs returns the top-level arguments of the call whose "(" is at open.
func callArgs(text string, open int) ([]string, bool) {
	depth, start, inQuote := 0, open+1, false
	var args []string
	for i := open; i < len(text); i++ {
		switch c := text[i]; {
		case c == '\'':
			inQuote = !inQuote
		case inQuote:
		case c == '(':
			depth++
		case c == ')':
			depth--
			if depth == 0 {
				return append(args, strings.TrimSpace(text[start:i])), true
			}
		case c == ',' && depth == 1:
			args = append(args, strings.TrimSpace(text[start:i]))
			start = i + 1
		}
	}
	return nil, false
}

// twoIntegerSites lists the two-argument advisory calls in one SQL text.
func twoIntegerSites(text string) []string {
	var firsts []string
	for _, loc := range advisoryCall.FindAllStringIndex(text, -1) {
		if args, ok := callArgs(text, loc[1]-1); ok && len(args) == 2 {
			firsts = append(firsts, args[0])
		}
	}
	return firsts
}

var (
	intLiteral   = regexp.MustCompile(`^-?\d+$`)
	hashLiteral  = regexp.MustCompile(`^hashtext\(\s*'((?:[^']|'')*)'\s*\)$`)
	bindParam    = regexp.MustCompile(`^(?:hashtext\(\s*)?\$(\d+)(?:::\w+)?\s*\)?$`)
	stripSQLNote = regexp.MustCompile(`(?m)--.*$`)
)

// scanAdvisoryClasses resolves every two-integer advisory lock class under
// root, skipping the directory skipDir (the package under test). A site whose
// class cannot be resolved is returned as unresolved: the caller fails on it,
// so a new lock shape cannot slip past the check silently.
func scanAdvisoryClasses(t *testing.T, root, skipDir string) (classes []lockClass, unresolved []string) {
	t.Helper()
	fset := token.NewFileSet()
	consts := map[string]map[string]*ast.BasicLit{} // dir -> name -> value
	constsByName := map[string][]*ast.BasicLit{}
	var sites []lockSite
	var files []*ast.File
	var fileDirs []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == skipDir || d.Name() == "vendor" || d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		dir := filepath.Dir(path)
		switch {
		case strings.HasSuffix(path, ".sql"):
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, first := range twoIntegerSites(stripSQLNote.ReplaceAllString(string(raw), "")) {
				sites = append(sites, lockSite{pos: path, firstArg: first, dir: dir})
			}
		case strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go"):
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if !strings.Contains(string(raw), "advisory") && !strings.Contains(string(raw), "const") {
				return nil
			}
			file, err := parser.ParseFile(fset, path, raw, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			files = append(files, file)
			fileDirs = append(fileDirs, dir)
			ast.Inspect(file, func(n ast.Node) bool {
				spec, ok := n.(*ast.ValueSpec)
				if !ok {
					return true
				}
				for i, name := range spec.Names {
					if i < len(spec.Values) {
						if lit, ok := spec.Values[i].(*ast.BasicLit); ok {
							if consts[dir] == nil {
								consts[dir] = map[string]*ast.BasicLit{}
							}
							consts[dir][name.Name] = lit
							constsByName[name.Name] = append(constsByName[name.Name], lit)
						}
					}
				}
				return true
			})
			ast.Inspect(file, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				text, err := strconv.Unquote(lit.Value)
				if err != nil {
					return true
				}
				for _, first := range twoIntegerSites(text) {
					sites = append(sites, lockSite{pos: fset.Position(lit.Pos()).String(), firstArg: first, literal: lit, dir: dir})
				}
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan %s: %v", root, err)
	}
	// Name the const that holds each Go SQL literal.
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			spec, ok := n.(*ast.ValueSpec)
			if !ok {
				return true
			}
			for j, value := range spec.Values {
				for k := range sites {
					if sites[k].literal != nil && sites[k].literal == value && j < len(spec.Names) {
						sites[k].constRef = spec.Names[j].Name
					}
				}
			}
			return true
		})
	}
	resolveArg := func(expr ast.Expr, dir string) (*ast.BasicLit, bool) {
		switch e := expr.(type) {
		case *ast.BasicLit:
			return e, true
		case *ast.Ident:
			if lit, ok := consts[dir][e.Name]; ok {
				return lit, true
			}
		case *ast.SelectorExpr:
			if lits := constsByName[e.Sel.Name]; len(lits) == 1 {
				return lits[0], true
			}
		}
		return nil, false
	}
	// bindArg finds the Go call that passes the site's SQL and returns its
	// n-th bind argument.
	bindArg := func(site lockSite, n int) (*ast.BasicLit, bool) {
		for i, file := range files {
			var found *ast.BasicLit
			ast.Inspect(file, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok || found != nil {
					return found == nil
				}
				for a, arg := range call.Args {
					match := false
					switch v := arg.(type) {
					case *ast.BasicLit:
						match = v == site.literal
					case *ast.Ident:
						match = site.constRef != "" && v.Name == site.constRef
					case *ast.SelectorExpr:
						match = site.constRef != "" && v.Sel.Name == site.constRef
					}
					if match && a+n < len(call.Args) {
						if lit, ok := resolveArg(call.Args[a+n], fileDirs[i]); ok {
							found = lit
						}
					}
				}
				return found == nil
			})
			if found != nil {
				return found, true
			}
		}
		return nil, false
	}
	for _, site := range sites {
		class := lockClass{site: site.pos}
		switch first := site.firstArg; {
		case intLiteral.MatchString(first):
			v, _ := strconv.ParseInt(first, 10, 64)
			class.integer = &v
		case hashLiteral.MatchString(first):
			s := strings.ReplaceAll(hashLiteral.FindStringSubmatch(first)[1], "''", "'")
			class.hashtext = &s
		case bindParam.MatchString(first) && site.literal != nil:
			n, _ := strconv.Atoi(bindParam.FindStringSubmatch(first)[1])
			lit, ok := bindArg(site, n)
			if !ok {
				unresolved = append(unresolved, fmt.Sprintf("%s: %s (no resolvable call argument)", site.pos, first))
				continue
			}
			switch lit.Kind {
			case token.INT:
				v, _ := strconv.ParseInt(lit.Value, 10, 64)
				class.integer = &v
			case token.STRING:
				s, _ := strconv.Unquote(lit.Value)
				if !strings.HasPrefix(first, "hashtext") {
					unresolved = append(unresolved, fmt.Sprintf("%s: %s bound to a string without hashtext", site.pos, first))
					continue
				}
				class.hashtext = &s
			default:
				unresolved = append(unresolved, fmt.Sprintf("%s: %s bound to %s", site.pos, first, lit.Value))
				continue
			}
		default:
			unresolved = append(unresolved, fmt.Sprintf("%s: class expression %q", site.pos, first))
			continue
		}
		classes = append(classes, class)
	}
	return classes, unresolved
}

// collisions evaluates every class in PostgreSQL and lists those equal to
// want.
func (l *ledgerDB) collisions(t *testing.T, classes []lockClass, want int64) []string {
	t.Helper()
	var hits []string
	for _, c := range classes {
		var v int64
		if c.integer != nil {
			v = *c.integer
		} else if err := l.raw.QueryRowContext(l.ctx, `SELECT hashtext($1)`, *c.hashtext).Scan(&v); err != nil {
			t.Fatalf("hashtext: %v", err)
		}
		if v == want {
			hits = append(hits, c.site)
		}
	}
	return hits
}

func goModuleRoot(t *testing.T) (root, self string) {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	self = filepath.Dir(file)
	return filepath.Clean(filepath.Join(self, "..", "..", "..", "..", "..")), self
}

// TestSlotLockClassDiffersFromTreeKeys derives every two-integer advisory
// lock class in the Go module from the code (review P3(f): no hand-kept list)
// and checks the full-link slot class against each. Single-bigint keys are a
// separate key space and are not compared.
func TestSlotLockClassDiffersFromTreeKeys(t *testing.T) {
	l := openLedgerDB(t)
	root, self := goModuleRoot(t)
	classes, unresolved := scanAdvisoryClasses(t, root, self)
	if len(unresolved) > 0 {
		t.Fatalf("advisory lock sites whose class the scan cannot resolve (teach the scan, do not skip them):\n%s",
			strings.Join(unresolved, "\n"))
	}
	if len(classes) < 4 {
		t.Fatalf("scan found only %d two-integer lock classes; the tree has at least four (schema bootstrap, shared projection leases, content search finalizer, deferred maintenance)", len(classes))
	}
	if hits := l.collisions(t, classes, linksfreshnessstore.SlotLockClass); len(hits) > 0 {
		t.Fatalf("slot lock class %d collides with %v", linksfreshnessstore.SlotLockClass, hits)
	}
	for _, c := range classes {
		t.Logf("class site: %s", c.site)
	}
	t.Logf("checked %d two-integer advisory lock classes", len(classes))
}

// TestSlotLockClassScanSeesAPlantedCollision is the seeded RED of the check
// above: a scratch tree with a SQL file and a Go file that each take the
// slot class must be reported.
func TestSlotLockClassScanSeesAPlantedCollision(t *testing.T) {
	l := openLedgerDB(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "planted.sql"),
		[]byte(fmt.Sprintf("-- planted\nSELECT pg_advisory_xact_lock(%d, 1);\n", linksfreshnessstore.SlotLockClass)), 0o644); err != nil {
		t.Fatalf("plant sql: %v", err)
	}
	goSrc := fmt.Sprintf(`package planted

const plantedClass = %d

const plantedQuery = "SELECT pg_try_advisory_xact_lock($1, $2)"

func take(db interface{ Exec(string, ...any) }) { db.Exec(plantedQuery, plantedClass, 1) }
`, linksfreshnessstore.SlotLockClass)
	if err := os.WriteFile(filepath.Join(dir, "planted.go"), []byte(goSrc), 0o644); err != nil {
		t.Fatalf("plant go: %v", err)
	}
	classes, unresolved := scanAdvisoryClasses(t, dir, "")
	if len(unresolved) > 0 {
		t.Fatalf("planted sites unresolved: %v", unresolved)
	}
	if hits := l.collisions(t, classes, linksfreshnessstore.SlotLockClass); len(hits) != 2 {
		t.Fatalf("planted collisions found = %v, want the SQL and the Go site", hits)
	}
}
