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

// readerSelectorReached reports whether an expression subtree contains a
// direct Access.Reader() or Access.StaleReader() selector.
func readerSelectorReached(expr ast.Node) bool {
	reached := false
	ast.Inspect(expr, func(inner ast.Node) bool {
		sel, ok := inner.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if sel.Sel.Name == "Reader" || sel.Sel.Name == "StaleReader" {
			reached = true
			return false
		}
		return true
	})
	return reached
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
		// Single in-order pass: a fencing call sees only the aliases
		// assigned by earlier statements, so a variable reused for the
		// writer after the call is not misread, and one assigned the
		// reader after the call does not taint it.
		aliases := map[string]bool{}
		var track func(stmt ast.Stmt)
		track = func(stmt ast.Stmt) {
			ast.Inspect(stmt, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok || !wanted[callName(call)] {
					return true
				}
				flagCall(call, callName(call), aliases)
				return true
			})
			assign, ok := stmt.(*ast.AssignStmt)
			if ok {
				applyAssign(assign, aliases)
			}
		}
		for _, stmt := range fn.Body.List {
			track(stmt)
		}
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
