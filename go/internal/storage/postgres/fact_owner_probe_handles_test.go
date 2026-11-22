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

// factOwnerWriteHandles is every storage type that wraps a transaction (SQLTx
// wraps a *sql.Tx; the rest wrap a db.Transaction) and is
// handed to a reducer fact writer as its write handle. Each must be able to run
// the #11213 owner probe: a handle that cannot resolves a write shortfall to
// nothing, so a fact id another scope holds would be skipped silently.
func factOwnerWriteHandles() map[string]factwrite.Execer {
	return map[string]factwrite.Execer{
		"containerImageIdentityTx":        containerImageIdentityTx{},
		"awsCloudRuntimeDriftAdmissionTx": awsCloudRuntimeDriftAdmissionTx{},
		"serviceMaterializationTx":        serviceMaterializationTx{},
		"supplyChainImpactTx":             supplyChainImpactTx{},
		"SQLTx":                           SQLTx{},
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

// TestEveryTransactionWrapperIsAFactOwnerWriteHandle finds the types of this
// package that hold a db.Transaction, a *sql.Tx or a *sql.Conn and expose
// ExecContext, so a new wrapper cannot reach a fact writer without being listed
// in factOwnerWriteHandles (and so without the probe test above covering it). It
// reads only the root storage/postgres package: a wrapper declared in a
// subpackage or in a reducer package is outside what this test sees.
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
	if missing := unlistedWriteHandleWrappers(files, factOwnerWriteHandles(), factOwnerNotWriteHandles()); len(missing) > 0 {
		t.Fatalf("transaction wrappers %v are not in factOwnerWriteHandles: list them (and give them QueryFactOwners) or name why they never reach a reducer fact writer", missing)
	}
}

// factOwnerNotWriteHandles are wrappers that expose ExecContext over a
// transaction or connection but never reach a reducer fact writer, with the
// reason.
func factOwnerNotWriteHandles() map[string]string {
	return map[string]string{
		"instrumentedTransaction":  "a db.Queryer itself; wraps SQLTx for telemetry",
		"schemaConnectionExecutor": "runs schema bootstrap DDL on a dedicated connection; no reducer writer receives it",
	}
}

// unlistedWriteHandleWrappers returns the struct types in files that hold a
// db.Transaction, *sql.Tx or *sql.Conn field and declare ExecContext, minus the
// listed handles and the named exemptions.
func unlistedWriteHandleWrappers(files []*ast.File, known map[string]factwrite.Execer, exempt map[string]string) []string {
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
						if holdsTransaction(field.Type) {
							wrappers[ts.Name.Name] = true
						}
					}
				}
			}
		}
	}
	var missing []string
	for name := range wrappers {
		if !execs[name] {
			continue
		}
		if _, ok := known[name]; ok {
			continue
		}
		if _, ok := exempt[name]; ok {
			continue
		}
		missing = append(missing, name)
	}
	sort.Strings(missing)
	return missing
}

// holdsTransaction reports whether a field type is db.Transaction, *sql.Tx or
// *sql.Conn.
func holdsTransaction(expr ast.Expr) bool {
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	switch sel.Sel.Name {
	case "Transaction", "Tx", "Conn":
		return true
	}
	return false
}

// TestTransactionWrapperScanSeededViolations proves the scan is not vacuous: a
// planted wrapper over each holder shape is reported, and a listed or exempt one
// is not.
func TestTransactionWrapperScanSeededViolations(t *testing.T) {
	t.Parallel()
	src := `package postgres
import ("database/sql"; "context")
type plantedTransactionTx struct{ tx db.Transaction }
func (plantedTransactionTx) ExecContext(context.Context, string, ...any) (sql.Result, error) { return nil, nil }
type plantedSQLTx struct{ tx *sql.Tx }
func (*plantedSQLTx) ExecContext(context.Context, string, ...any) (sql.Result, error) { return nil, nil }
type plantedSQLConn struct{ conn *sql.Conn }
func (plantedSQLConn) ExecContext(context.Context, string, ...any) (sql.Result, error) { return nil, nil }
type plantedNoExec struct{ tx *sql.Tx }
type plantedListed struct{ tx *sql.Tx }
func (plantedListed) ExecContext(context.Context, string, ...any) (sql.Result, error) { return nil, nil }
type plantedExempt struct{ tx *sql.Tx }
func (plantedExempt) ExecContext(context.Context, string, ...any) (sql.Result, error) { return nil, nil }
`
	file, err := parser.ParseFile(token.NewFileSet(), "planted.go", src, 0)
	if err != nil {
		t.Fatalf("parse seeded source: %v", err)
	}
	got := unlistedWriteHandleWrappers([]*ast.File{file},
		map[string]factwrite.Execer{"plantedListed": nil},
		map[string]string{"plantedExempt": "never reaches a fact writer"})
	want := []string{"plantedSQLConn", "plantedSQLTx", "plantedTransactionTx"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("unlisted wrappers = %v, want %v", got, want)
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
