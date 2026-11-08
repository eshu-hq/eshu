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

// fencingReaderUses scans one non-test Go source file for fencing constructor
// calls whose argument expression reaches Access.Reader() or
// Access.StaleReader(). It parses the file so a constructor split across
// lines is still caught, and it ignores _test.go files.
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
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		var name string
		switch fun := call.Fun.(type) {
		case *ast.Ident:
			name = fun.Name
		case *ast.SelectorExpr:
			name = fun.Sel.Name
		}
		if !wanted[name] {
			return true
		}
		ast.Inspect(call, func(inner ast.Node) bool {
			sel, ok := inner.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if sel.Sel.Name != "Reader" && sel.Sel.Name != "StaleReader" {
				return true
			}
			violations = append(violations, fencingReaderViolation{
				file: path,
				line: fset.Position(call.Pos()).Line,
				call: name,
			})
			return false
		})
		return true
	})
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
