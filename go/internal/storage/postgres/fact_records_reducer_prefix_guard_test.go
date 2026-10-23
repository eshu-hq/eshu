// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/types"
	"io/fs"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"golang.org/x/tools/go/packages"
)

// The #8750 destructive-successor guard sizes a prior on collector rows by
// excluding every fact_kind LIKE 'reducer\_%'. That is only true to the prior
// if every writer that inserts fact_records outside the collector commit path
// writes reducer_-prefixed kinds. These tests hold the whole module to it:
//
//  1. every non-test Go file that issues INSERT INTO fact_records is
//     enumerated, so a new insert site cannot appear unreviewed;
//  2. every reducer writer that reaches fact_records through the shared
//     factwrite helpers, and the search-document writer's own bulk insert,
//     is type-resolved to a constant reducer_-prefixed kind.
//
// The packages are type-checked from source (NeedDeps), not read from export
// data, so the scan does not depend on the local toolchain matching go.mod.

const reducerKindPrefix = "reducer_"

// factRecordsInsertSites enumerates every non-test Go file that inserts into
// fact_records, keyed by module-relative path.
var factRecordsInsertSites = map[string]string{
	"internal/storage/postgres/facts_streaming.go":                       "collector commit path: streams collector facts, never reducer_ kinds",
	"internal/reducer/factwrite/batch_insert.go":                         "reducer writer helper: Row batch insert; kinds are set by each writer and type-checked below",
	"internal/reducer/factwrite/batch_insert_versioned.go":               "reducer writer helper: VersionedRow batch insert; kinds are set by each writer and type-checked below",
	"internal/reducer/factwrite/single_insert.go":                        "reducer writer helper: single-row insert; kinds are set by each writer and type-checked below",
	"internal/reducer/eshusearch/eshu_search_document_writer_queries.go": "search-document writer's own unnest insert; kind is EshuSearchDocumentFactKind, type-checked below",
	"internal/storage/postgres/testfixtures/deferred_partition.go":       "test fixture seeding; not linked into any runtime binary",
	"internal/storage/postgres/testfixtures/targeted_pair.go":            "test fixture seeding; not linked into any runtime binary",
}

var factRecordsInsertPattern = regexp.MustCompile(`(?is)(insert\s+into|copy)\s+(public\.)?"?fact_records\b`)

// unenumeratedFactRecordsInserts returns the sorted violations of the
// enumeration rule over files: a non-test Go file with an insert that is not
// in expected, and an expected file that has no insert (a stale entry).
func unenumeratedFactRecordsInserts(files fs.FS, expected map[string]string) ([]string, error) {
	found := map[string]struct{}{}
	err := fs.WalkDir(files, ".", func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if name := entry.Name(); name == "vendor" || name == "testdata" || name == ".gocache" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		src, err := fs.ReadFile(files, p)
		if err != nil {
			return err
		}
		var code strings.Builder
		for _, line := range strings.Split(string(src), "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), "//") {
				code.WriteString(line)
				code.WriteString("\n")
			}
		}
		if factRecordsInsertPattern.MatchString(code.String()) {
			found[p] = struct{}{}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	var violations []string
	for p := range found {
		if _, ok := expected[p]; !ok {
			violations = append(violations, "unenumerated INSERT INTO fact_records: "+p)
		}
	}
	for p := range expected {
		if _, ok := found[p]; !ok {
			violations = append(violations, "enumerated insert site no longer inserts (stale entry): "+p)
		}
	}
	sort.Strings(violations)
	return violations, nil
}

// TestFactRecordsInsertSitesAreEnumerated fails when a new INSERT INTO
// fact_records appears outside the enumerated set.
func TestFactRecordsInsertSitesAreEnumerated(t *testing.T) {
	violations, err := unenumeratedFactRecordsInserts(os.DirFS("../../.."), factRecordsInsertSites)
	if err != nil {
		t.Fatalf("scan module: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("fact_records insert sites drifted from the enumeration; a new writer must write reducer_-prefixed kinds "+
			"(the #8750 guard excludes them from the prior's collector rows) and be listed here:\n%s", strings.Join(violations, "\n"))
	}
}

// TestFactRecordsInsertEnumerationSeededViolation is the enumeration's RED and
// GREEN pair: a planted insert site is reported, the clean tree is not, and a
// stale entry is reported.
func TestFactRecordsInsertEnumerationSeededViolation(t *testing.T) {
	insert := []byte("package x\nconst q = `\nINSERT INTO fact_records (fact_id) VALUES ($1)`\n")
	clean := fstest.MapFS{"a/writer.go": {Data: insert}, "a/other.go": {Data: []byte("package x\n// INSERT INTO fact_records is mentioned in a comment only\n")}}
	if got, err := unenumeratedFactRecordsInserts(clean, map[string]string{"a/writer.go": "ok"}); err != nil || len(got) != 0 {
		t.Fatalf("clean tree violations = %v (err %v), want none", got, err)
	}
	planted := fstest.MapFS{"a/writer.go": {Data: insert}, "b/new_writer.go": {Data: insert}}
	got, _ := unenumeratedFactRecordsInserts(planted, map[string]string{"a/writer.go": "ok"})
	if len(got) != 1 || !strings.Contains(got[0], "b/new_writer.go") {
		t.Fatalf("planted violations = %v, want the new site reported", got)
	}
	got, _ = unenumeratedFactRecordsInserts(fstest.MapFS{"a/writer.go": {Data: insert}}, map[string]string{"a/writer.go": "ok", "gone.go": "stale"})
	if len(got) != 1 || !strings.Contains(got[0], "gone.go") {
		t.Fatalf("stale violations = %v, want the stale entry reported", got)
	}
}

// loadFactwriteImporters loads, with types, every package that imports the
// reducer factwrite helpers, optionally overlaying synthetic files.
func loadFactwriteImporters(t *testing.T, overlay map[string][]byte) []*packages.Package {
	t.Helper()
	root := "../../.."
	env := append(os.Environ(), "GOWORK=off")
	pre, err := packages.Load(&packages.Config{Mode: packages.NeedName | packages.NeedImports, Dir: root, Env: env, Overlay: overlay}, "./...")
	if err != nil {
		t.Fatalf("list module packages: %v", err)
	}
	var importers []string
	for _, pkg := range pre {
		if strings.HasSuffix(pkg.PkgPath, "/reducer/factwrite/testutil") {
			continue // test helper package: reads rows, never chooses a kind
		}
		for imp := range pkg.Imports {
			if strings.HasSuffix(imp, "/reducer/factwrite") {
				importers = append(importers, pkg.PkgPath)
				break
			}
		}
	}
	if len(importers) < 10 {
		t.Fatalf("found %d factwrite importers, want at least 10: the scan is broken", len(importers))
	}
	loaded, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports | packages.NeedDeps,
		Dir: root, Env: env, Overlay: overlay,
	}, importers...)
	if err != nil {
		t.Fatalf("load factwrite importers: %v", err)
	}
	for _, pkg := range loaded {
		if len(pkg.Errors) > 0 {
			t.Fatalf("load %s: %v (run with the go.mod toolchain, GOTOOLCHAIN=go1.26.10)", pkg.PkgPath, pkg.Errors[0])
		}
	}
	return loaded
}

// factwriteRowType reports whether t is a factwrite struct that carries a
// FactKind field (Row, VersionedRow).
func factwriteRowType(t types.Type) bool {
	named, ok := types.Unalias(t).(*types.Named)
	if !ok || named.Obj().Pkg() == nil || !strings.HasSuffix(named.Obj().Pkg().Path(), "/reducer/factwrite") {
		return false
	}
	st, ok := named.Underlying().(*types.Struct)
	if !ok {
		return false
	}
	for i := 0; i < st.NumFields(); i++ {
		if st.Field(i).Name() == "FactKind" {
			return true
		}
	}
	return false
}

type kindChecker struct {
	pkg        *packages.Package
	violations []string
	checked    int
	proven     map[string]struct{} // distinct constant kinds proven
}

func (c *kindChecker) failf(pos ast.Node, format string, args ...any) {
	c.violations = append(c.violations, fmt.Sprintf("%s: %s", c.pkg.Fset.Position(pos.Pos()), fmt.Sprintf(format, args...)))
}

// constKind returns expr's constant string value.
func (c *kindChecker) constKind(expr ast.Expr) (string, bool) {
	tv, ok := c.pkg.TypesInfo.Types[expr]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
		return "", false
	}
	return constant.StringVal(tv.Value), true
}

// requireReducerKind checks expr resolves to a reducer_-prefixed constant. A
// function parameter is followed one hop to every call of the enclosing
// function in this package, each of which must pass such a constant.
func (c *kindChecker) requireReducerKind(site ast.Node, expr ast.Expr, enclosing *ast.FuncDecl) {
	c.checked++
	if kind, ok := c.constKind(expr); ok {
		c.provePrefix(site, kind)
		return
	}
	ident, isIdent := expr.(*ast.Ident)
	if !isIdent || enclosing == nil {
		c.failf(site, "fact kind is not a constant and not a parameter of the enclosing function; the reducer_ prefix is unprovable")
		return
	}
	index := -1
	i := 0
	for _, field := range enclosing.Type.Params.List {
		for _, name := range field.Names {
			if c.pkg.TypesInfo.Defs[name] == c.pkg.TypesInfo.Uses[ident] {
				index = i
			}
			i++
		}
	}
	if index < 0 || c.pkg.TypesInfo.Uses[ident] == nil {
		c.failf(site, "fact kind %q is not a parameter of %s; the reducer_ prefix is unprovable", ident.Name, enclosing.Name.Name)
		return
	}
	target := c.pkg.TypesInfo.Defs[enclosing.Name]
	callers := 0
	for _, file := range c.pkg.Syntax {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			var callee types.Object
			switch fn := call.Fun.(type) {
			case *ast.Ident:
				callee = c.pkg.TypesInfo.Uses[fn]
			case *ast.SelectorExpr:
				callee = c.pkg.TypesInfo.Uses[fn.Sel]
			}
			if callee == nil || callee != target {
				return true
			}
			callers++
			if index >= len(call.Args) {
				c.failf(call, "call to %s has no kind argument", enclosing.Name.Name)
				return true
			}
			kind, ok := c.constKind(call.Args[index])
			if !ok {
				c.failf(call, "call to %s passes a non-constant fact kind", enclosing.Name.Name)
				return true
			}
			c.provePrefix(call, kind)
			return true
		})
	}
	if callers == 0 {
		c.failf(site, "no caller of %s passes its fact kind in this package; the reducer_ prefix is unprovable", enclosing.Name.Name)
	}
}

func (c *kindChecker) provePrefix(site ast.Node, kind string) {
	if !strings.HasPrefix(kind, reducerKindPrefix) {
		c.failf(site, "fact kind %q lacks the %q prefix the #8750 guard excludes from the prior's collector rows", kind, reducerKindPrefix)
		return
	}
	c.proven[kind] = struct{}{}
}

// inspect walks pkg's files, tracking the enclosing function declaration.
func (c *kindChecker) inspect() {
	for _, file := range c.pkg.Syntax {
		for _, decl := range file.Decls {
			fn, _ := decl.(*ast.FuncDecl)
			ast.Inspect(decl, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.CompositeLit:
					c.checkRowLiteral(node, fn)
				case *ast.AssignStmt:
					c.checkFactKindAssign(node, fn)
				case *ast.CallExpr:
					c.checkSingleInsertCall(node, fn)
					c.checkFactKindsAppend(node, fn)
				}
				return true
			})
		}
	}
}

func (c *kindChecker) checkRowLiteral(lit *ast.CompositeLit, fn *ast.FuncDecl) {
	if t := c.pkg.TypesInfo.TypeOf(lit); t == nil || !factwriteRowType(t) {
		return
	}
	if len(lit.Elts) == 0 {
		return // zero value returned beside an error; never inserted
	}
	for _, el := range lit.Elts {
		if kv, ok := el.(*ast.KeyValueExpr); ok {
			if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "FactKind" {
				c.requireReducerKind(lit, kv.Value, fn)
				return
			}
		}
	}
	c.failf(lit, "factwrite row literal sets no FactKind")
}

func (c *kindChecker) checkFactKindAssign(assign *ast.AssignStmt, fn *ast.FuncDecl) {
	for i, lhs := range assign.Lhs {
		sel, ok := lhs.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "FactKind" || i >= len(assign.Rhs) {
			continue
		}
		if t := c.pkg.TypesInfo.TypeOf(sel.X); t != nil && factwriteRowType(derefType(t)) {
			c.requireReducerKind(assign, assign.Rhs[i], fn)
		}
	}
}

func derefType(t types.Type) types.Type {
	if ptr, ok := t.(*types.Pointer); ok {
		return ptr.Elem()
	}
	return t
}

// checkSingleInsertCall checks the kind argument of an Exec of
// factwrite.SingleInsertQuery: the query, then fact id, scope, generation,
// and the kind.
func (c *kindChecker) checkSingleInsertCall(call *ast.CallExpr, fn *ast.FuncDecl) {
	for i, arg := range call.Args {
		sel, ok := arg.(*ast.SelectorExpr)
		if !ok {
			continue
		}
		obj, ok := c.pkg.TypesInfo.Uses[sel.Sel].(*types.Const)
		if !ok || obj.Pkg() == nil || !strings.HasSuffix(obj.Pkg().Path(), "/reducer/factwrite") || obj.Name() != "SingleInsertQuery" {
			continue
		}
		if i+4 >= len(call.Args) {
			c.failf(call, "SingleInsertQuery call has no kind argument")
			return
		}
		c.requireReducerKind(call, call.Args[i+4], fn)
	}
}

// checkFactKindsAppend checks the search-document writer's unnest kind array:
// every append to a slice named factKinds in the eshusearch package.
func (c *kindChecker) checkFactKindsAppend(call *ast.CallExpr, fn *ast.FuncDecl) {
	if !strings.HasSuffix(c.pkg.PkgPath, "/reducer/eshusearch") {
		return
	}
	id, ok := call.Fun.(*ast.Ident)
	if !ok || id.Name != "append" || len(call.Args) < 2 {
		return
	}
	if dst, ok := call.Args[0].(*ast.Ident); !ok || dst.Name != "factKinds" {
		return
	}
	for _, arg := range call.Args[1:] {
		c.requireReducerKind(call, arg, fn)
	}
}

func checkReducerWriterKinds(pkgs []*packages.Package) (violations []string, checked int, proven map[string]struct{}) {
	proven = map[string]struct{}{}
	for _, pkg := range pkgs {
		c := &kindChecker{pkg: pkg, proven: proven}
		c.inspect()
		violations = append(violations, c.violations...)
		checked += c.checked
	}
	sort.Strings(violations)
	return violations, checked, proven
}

// TestReducerFactWritersWriteReducerPrefixedKinds type-resolves the fact kind
// of every reducer write into fact_records to a reducer_-prefixed constant.
func TestReducerFactWritersWriteReducerPrefixedKinds(t *testing.T) {
	violations, checked, proven := checkReducerWriterKinds(loadFactwriteImporters(t, nil))
	if checked < 15 || len(proven) < 15 {
		t.Fatalf("checked %d kind sites proving %d distinct kinds, want at least 15 of each: the scan is broken", checked, len(proven))
	}
	if _, ok := proven["reducer_eshu_search_document"]; !ok {
		t.Fatal("the search-document writer's reducer_eshu_search_document kind was not proven; its unnest insert is unchecked")
	}
	if len(violations) > 0 {
		t.Fatalf("reducer writers whose fact kind is not provably reducer_-prefixed:\n%s", strings.Join(violations, "\n"))
	}
}

// TestReducerFactWriterKindCheckSeededViolation is the checker's RED and GREEN
// pair. Synthetic writer files are overlaid into a real writer package, one
// file per case, and loaded once. A case is reported when it writes an
// unprefixed kind through a Row literal, a field assignment, a non-constant
// kind, or a call-fed parameter, and is clean when it writes a reducer_ kind.
func TestReducerFactWriterKindCheckSeededViolation(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string // substring of the single expected violation, "" for clean
	}{
		{"clean_literal", "func seededCleanLiteral() factwrite.Row { return factwrite.Row{FactKind: \"reducer_seeded\"} }\n", ""},
		{"unprefixed_literal", "func seededBadLiteral() factwrite.Row { return factwrite.Row{FactKind: \"seeded_kind\"} }\n", "lacks the \"reducer_\" prefix"},
		{"missing_kind", "func seededMissingKind() factwrite.Row { return factwrite.Row{FactID: \"x\"} }\n", "sets no FactKind"},
		{"unprefixed_assignment", "func seededAssign() { var r factwrite.Row; r.FactKind = \"seeded_kind\"; _ = r }\n", "lacks the \"reducer_\" prefix"},
		{"non_constant_kind", "func seededDynamic(kind string) factwrite.Row { var k = kind + \"x\"; return factwrite.Row{FactKind: k} }\n", "unprovable"},
		{"parameter_fed_unprefixed", "func seededParamBad(kind string) factwrite.Row { return factwrite.Row{FactKind: kind} }\nfunc seededCallerBad() { _ = seededParamBad(\"seeded_kind\") }\n", "lacks the \"reducer_\" prefix"},
		{"parameter_fed_prefixed", "func seededParamGood(kind string) factwrite.Row { return factwrite.Row{FactKind: kind} }\nfunc seededCallerGood() { _ = seededParamGood(\"reducer_seeded\") }\n", ""},
	}
	overlay := map[string][]byte{}
	for _, tc := range cases {
		abs, err := absFromModule(path.Join("internal/reducer/cloudinventory", "zz_seeded_"+tc.name+".go"))
		if err != nil {
			t.Fatal(err)
		}
		overlay[abs] = []byte("package cloudinventory\n\nimport \"github.com/eshu-hq/eshu/go/internal/reducer/factwrite\"\n\n" + tc.body)
	}
	violations, _, _ := checkReducerWriterKinds(onlyPackage(loadFactwriteImporters(t, overlay), "/reducer/cloudinventory"))
	for _, tc := range cases {
		var mine []string
		for _, v := range violations {
			if strings.Contains(v, "zz_seeded_"+tc.name+".go") {
				mine = append(mine, v)
			}
		}
		switch {
		case tc.want == "" && len(mine) != 0:
			t.Errorf("%s: clean seed reported violations: %v", tc.name, mine)
		case tc.want != "" && (len(mine) != 1 || !strings.Contains(mine[0], tc.want)):
			t.Errorf("%s: seeded violations = %v, want exactly one containing %q", tc.name, mine, tc.want)
		}
	}
	for _, v := range violations {
		if !strings.Contains(v, "zz_seeded_") {
			t.Errorf("production writer reported while checking the seeds: %s", v)
		}
	}
}

func onlyPackage(pkgs []*packages.Package, suffix string) []*packages.Package {
	var out []*packages.Package
	for _, pkg := range pkgs {
		if strings.HasSuffix(pkg.PkgPath, suffix) {
			out = append(out, pkg)
		}
	}
	return out
}

func absFromModule(rel string) (string, error) {
	root, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return path.Join(root, "../../..", rel), nil
}
