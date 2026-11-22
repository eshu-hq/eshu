// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
)

// factOwnerWriteHandles is every storage type that wraps a db.Transaction and is
// handed to a reducer fact writer as its write handle. Each must be able to run
// the #11213 owner probe: a handle that cannot resolves a write shortfall to
// nothing, so a fact id another scope holds would be skipped silently.
func factOwnerWriteHandles() map[string]factwrite.Execer {
	return map[string]factwrite.Execer{
		"containerImageIdentityTx":        containerImageIdentityTx{},
		"awsCloudRuntimeDriftAdmissionTx": awsCloudRuntimeDriftAdmissionTx{},
		"serviceMaterializationTx":        serviceMaterializationTx{},
		"supplyChainImpactTx":             supplyChainImpactTx{},
	}
}

// TestFactOwnerWriteHandlesCanProbe pins that every transaction wrapper reaches
// the owner probe, directly or through factwrite.OwnerProbeQueryer.
func TestFactOwnerWriteHandlesCanProbe(t *testing.T) {
	t.Parallel()
	for name, handle := range factOwnerWriteHandles() {
		if !factwrite.CanProbeOwners(handle) {
			t.Errorf("%s cannot run the fact owner probe: add QueryFactOwners (factwrite.OwnerProbeQueryer) so a foreign-held fact id is reported, not skipped", name)
		}
	}
	for name, handle := range map[string]factwrite.Execer{"SQLDB": SQLDB{}, "SQLTx": SQLTx{}} {
		if !factwrite.CanProbeOwners(handle) {
			t.Errorf("%s cannot run the fact owner probe", name)
		}
	}
}

// TestEveryTransactionWrapperIsAFactOwnerWriteHandle finds the storage types
// that wrap a db.Transaction and expose ExecContext, so a new wrapper cannot
// reach a fact writer without being listed in factOwnerWriteHandles (and so
// without the probe test above covering it).
func TestEveryTransactionWrapperIsAFactOwnerWriteHandle(t *testing.T) {
	t.Parallel()
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	var files []*ast.File
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, entry.Name(), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", entry.Name(), err)
		}
		files = append(files, file)
	}
	wrappers := map[string]bool{}
	execs := map[string]bool{}
	for _, file := range files {
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Recv != nil && d.Name.Name == "ExecContext" {
					execs[receiverName(d.Recv.List[0].Type)] = true
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					st, ok := ts.Type.(*ast.StructType)
					if !ok {
						continue
					}
					for _, field := range st.Fields.List {
						if sel, ok := field.Type.(*ast.SelectorExpr); ok && sel.Sel.Name == "Transaction" {
							wrappers[ts.Name.Name] = true
						}
					}
				}
			}
		}
	}
	known := factOwnerWriteHandles()
	// Wrappers that never reach a reducer fact writer, with the reason.
	notFactHandles := map[string]string{
		"instrumentedTransaction": "a db.Queryer itself; wraps SQLTx for telemetry",
	}
	var missing []string
	for name := range wrappers {
		if !execs[name] {
			continue
		}
		if _, ok := known[name]; ok {
			continue
		}
		if _, ok := notFactHandles[name]; ok {
			continue
		}
		missing = append(missing, name)
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("transaction wrappers %v are not in factOwnerWriteHandles: list them (and give them QueryFactOwners) or name why they never reach a reducer fact writer", missing)
	}
}

func receiverName(expr ast.Expr) string {
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	if ident, ok := expr.(*ast.Ident); ok {
		return ident.Name
	}
	return ""
}
