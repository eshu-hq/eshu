// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fencingStoreConstructors names the writer-backed fencing stores that must
// never be built from a guarded reader expression. Claims, leases, fences,
// pins, and commits must read their own writes, so a replica that lags by
// seconds would corrupt fencing decisions (#10416). Later slices extend this
// seed list as the family census lands.
var fencingStoreConstructors = []string{
	"NewClaimStore",
	"NewLeaseStore",
	"NewPinStore",
	"NewCommitHandler",
	"NewRetirementFencer",
	"NewRetirementPurger",
	"NewWriteFence",
	"NewWriteFenceForRuntime",
	"NewRunFence",
}

// fencingReaderViolation ties a fencing constructor call to the file and line
// where a guarded reader expression feeds it.
type fencingReaderViolation struct {
	file string
	line int
	call string
}

// isReaderCall reports whether an expression is a direct Access.Reader() or
// Access.StaleReader() call. Only this form creates an alias: a value merely
// computed from a reader (for example `n := countRows(access.Reader())`) is
// not the reader itself.
func isReaderCall(expr ast.Node) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	return sel.Sel.Name == "Reader" || sel.Sel.Name == "StaleReader"
}

// applyInit records aliases from an if/for/switch initializer, which runs
// before the controlled body.
func applyInit(stmt ast.Stmt, aliases map[string]bool) {
	if assign, ok := stmt.(*ast.AssignStmt); ok {
		applyAssign(assign, aliases)
	}
}

// applyValueSpec records the names a var declaration binds a guarded
// reader call to, positionally. A declaration without values, or with any
// other value, removes the names.
func applyValueSpec(spec *ast.ValueSpec, aliases map[string]bool) {
	for i, name := range spec.Names {
		if name.Name == "_" {
			continue
		}
		reaches := false
		if len(spec.Values) == len(spec.Names) {
			reaches = isReaderCall(spec.Values[i])
		} else {
			for _, value := range spec.Values {
				if isReaderCall(value) {
					reaches = true
					break
				}
			}
		}
		if reaches {
			aliases[name.Name] = true
		} else {
			delete(aliases, name.Name)
		}
	}
}

// applyAssign records the identifiers an assignment binds a guarded reader
// call to, or removes them when any other value is assigned. Only
// single-hop aliases are tracked: `rs := access.Reader()` followed by a
// fencing call on `rs` is caught, while a second hop through another
// variable is out of scope by design (documented in the test).
func applyAssign(assign *ast.AssignStmt, aliases map[string]bool) {
	for i, lhs := range assign.Lhs {
		id, ok := lhs.(*ast.Ident)
		if !ok || id.Name == "_" {
			continue
		}
		reaches := false
		if len(assign.Rhs) == len(assign.Lhs) {
			// Positional pairing covers :=, =, and multi-value forms.
			reaches = isReaderCall(assign.Rhs[i])
		} else {
			for _, rhs := range assign.Rhs {
				if isReaderCall(rhs) {
					reaches = true
					break
				}
			}
		}
		if reaches {
			aliases[id.Name] = true
		} else {
			delete(aliases, id.Name)
		}
	}
}

// fencingReaderUses scans one non-test Go source file for fencing constructor
// calls whose argument expression reaches Access.Reader() or
// Access.StaleReader(), either inline or through a same-function alias. It
// parses the file so a constructor split across lines is still caught, and
// it ignores _test.go files.
func fencingReaderUses(path, src string) []fencingReaderViolation {
	if strings.HasSuffix(path, "_test.go") {
		return nil
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		return []fencingReaderViolation{{file: path, call: "unparseable"}}
	}
	wanted := map[string]bool{}
	for _, name := range fencingStoreConstructors {
		wanted[name] = true
	}
	var violations []fencingReaderViolation
	flagCall := func(call *ast.CallExpr, name string, aliases map[string]bool) {
		hit := false
		ast.Inspect(call, func(inner ast.Node) bool {
			switch expr := inner.(type) {
			case *ast.SelectorExpr:
				if expr.Sel.Name == "Reader" || expr.Sel.Name == "StaleReader" {
					hit = true
					return false
				}
			case *ast.Ident:
				if aliases[expr.Name] {
					hit = true
					return false
				}
			}
			return true
		})
		if hit {
			violations = append(violations, fencingReaderViolation{
				file: path,
				line: fset.Position(call.Pos()).Line,
				call: name,
			})
		}
	}
	callName := func(call *ast.CallExpr) string {
		switch fun := call.Fun.(type) {
		case *ast.Ident:
			return fun.Name
		case *ast.SelectorExpr:
			return fun.Sel.Name
		}
		return ""
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		// Single in-order pass over every statement, including nested
		// blocks: a fencing call sees only the aliases assigned by
		// earlier statements, so a variable reused for the writer after
		// the call is not misread, and one assigned the reader after
		// the call does not taint it. Nested scopes share the map: a
		// conditionally assigned reader that reaches a fencing store
		// on any path is worth the flag.
		aliases := map[string]bool{}
		var scanBlock func(list []ast.Stmt)
		// flagLevel reports fencing calls in one statement without
		// descending into nested blocks or closures, which each get
		// their own pass below.
		flagLevel := func(stmt ast.Stmt, scope map[string]bool) {
			ast.Inspect(stmt, func(node ast.Node) bool {
				switch node.(type) {
				case *ast.BlockStmt, *ast.FuncLit, *ast.CaseClause, *ast.CommClause:
					return false
				}
				call, ok := node.(*ast.CallExpr)
				if !ok || !wanted[callName(call)] {
					return true
				}
				flagCall(call, callName(call), scope)
				return true
			})
		}
		scanBlock = func(list []ast.Stmt) {
			for _, stmt := range list {
				flagLevel(stmt, aliases)
				switch stmt := stmt.(type) {
				case *ast.AssignStmt:
					applyAssign(stmt, aliases)
				case *ast.DeclStmt:
					if gen, ok := stmt.Decl.(*ast.GenDecl); ok && gen.Tok == token.VAR {
						for _, spec := range gen.Specs {
							if vs, ok := spec.(*ast.ValueSpec); ok {
								applyValueSpec(vs, aliases)
							}
						}
					}
				case *ast.IfStmt:
					applyInit(stmt.Init, aliases)
					scanBlock(stmt.Body.List)
					switch els := stmt.Else.(type) {
					case *ast.BlockStmt:
						scanBlock(els.List)
					case *ast.IfStmt:
						scanBlock([]ast.Stmt{els})
					}
				case *ast.ForStmt:
					applyInit(stmt.Init, aliases)
					scanBlock(stmt.Body.List)
				case *ast.RangeStmt:
					scanBlock(stmt.Body.List)
				case *ast.SwitchStmt:
					applyInit(stmt.Init, aliases)
					scanBlock(stmt.Body.List)
				case *ast.TypeSwitchStmt:
					applyInit(stmt.Init, aliases)
					scanBlock(stmt.Body.List)
				case *ast.SelectStmt:
					scanBlock(stmt.Body.List)
				case *ast.CaseClause:
					scanBlock(stmt.Body)
				case *ast.CommClause:
					scanBlock(stmt.Body)
				}
				// Closures see a copy of the current scope: an outer
				// alias used inside is still caught, while anything
				// assigned inside never leaks out.
				ast.Inspect(stmt, func(node ast.Node) bool {
					lit, ok := node.(*ast.FuncLit)
					if !ok {
						return true
					}
					inner := map[string]bool{}
					for name := range aliases {
						inner[name] = true
					}
					outer := aliases
					aliases = inner
					scanBlock(lit.Body.List)
					aliases = outer
					return false
				})
			}
		}
		scanBlock(fn.Body.List)
	}
	return violations
}

func TestFencingReaderGuardBitesOnSeededViolation(t *testing.T) {
	t.Parallel()
	violated := `package wiring

func wire(access *Access) {
	store := NewClaimStore(access.Reader())
	_ = store
}
`
	got := fencingReaderUses("wiring.go", violated)
	if len(got) != 1 || got[0].call != "NewClaimStore" {
		t.Fatalf("seeded Reader() violation = %+v, want one NewClaimStore hit", got)
	}
	stale := `package wiring

func wire(access *Access) {
	store := NewLeaseStore(access.StaleReader())
	_ = store
}
`
	if got := fencingReaderUses("wiring.go", stale); len(got) != 1 || got[0].call != "NewLeaseStore" {
		t.Fatalf("seeded StaleReader() violation = %+v, want one NewLeaseStore hit", got)
	}
	fixed := `package wiring

func wire(access *Access, writer *sql.DB) {
	store := NewClaimStore(writer)
	_ = store
}
`
	if got := fencingReaderUses("wiring.go", fixed); len(got) != 0 {
		t.Fatalf("writer-backed wiring violations = %+v, want none", got)
	}
}

func TestFencingReaderGuardTracksAliases(t *testing.T) {
	t.Parallel()
	staleAlias := `package wiring

func wire(access *Access) {
	rs := access.StaleReader()
	store := NewWriteFence(rs)
	_ = store
}
`
	if got := fencingReaderUses("wiring.go", staleAlias); len(got) != 1 || got[0].call != "NewWriteFence" {
		t.Fatalf("aliased StaleReader() violation = %+v, want one NewWriteFence hit", got)
	}
	readerAlias := `package wiring

func wire(access *Access) {
	r := access.Reader()
	store := NewClaimStore(r)
	_ = store
}
`
	if got := fencingReaderUses("wiring.go", readerAlias); len(got) != 1 || got[0].call != "NewClaimStore" {
		t.Fatalf("aliased Reader() violation = %+v, want one NewClaimStore hit", got)
	}
	reassigned := `package wiring

func wire(access *Access, writer *sql.DB) {
	rs := access.StaleReader()
	rs = writer
	store := NewWriteFence(rs)
	_ = store
}
`
	if got := fencingReaderUses("wiring.go", reassigned); len(got) != 0 {
		t.Fatalf("reassigned-away alias violations = %+v, want none", got)
	}
	chained := `package wiring

func wire(access *Access) {
	rs := access.StaleReader()
	other := rs
	store := NewWriteFence(other)
	_ = store
}
`
	if got := fencingReaderUses("wiring.go", chained); len(got) != 0 {
		t.Fatalf("chained alias violations = %+v, want none (single-hop only, documented)", got)
	}
	varDecl := `package wiring

func wire(access *Access) {
	var rs = access.StaleReader()
	store := NewWriteFence(rs)
	_ = store
}
`
	if got := fencingReaderUses("wiring.go", varDecl); len(got) != 1 || got[0].call != "NewWriteFence" {
		t.Fatalf("var-declared StaleReader() violation = %+v, want one NewWriteFence hit", got)
	}
	nested := `package wiring

func wire(access *Access, cond bool) {
	if cond {
		rs := access.StaleReader()
		store := NewWriteFence(rs)
		_ = store
	}
}
`
	if got := fencingReaderUses("wiring.go", nested); len(got) != 1 || got[0].call != "NewWriteFence" {
		t.Fatalf("block-nested StaleReader() violation = %+v, want one NewWriteFence hit", got)
	}
}

// TestFencingReaderGuardCleanTree proves no non-test go/cmd wiring builds a
// fenced store from a guarded reader today. A later wiring that does fails
// here until the fencing family moves back to the writer.
func TestFencingReaderGuardCleanTree(t *testing.T) {
	t.Parallel()
	root := goModuleRoot(t)
	cmd := filepath.Join(root, "cmd")
	var violations []fencingReaderViolation
	err := filepath.Walk(cmd, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		violations = append(violations, fencingReaderUses(path, string(src))...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("fencing stores built from a guarded reader: %+v", violations)
	}
}

// goModuleRoot returns the directory holding go.mod, walking up from this
// test file so the guard scans the real cmd tree regardless of the caller's
// working directory.
func goModuleRoot(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test working directory")
		}
		dir = parent
	}
}
