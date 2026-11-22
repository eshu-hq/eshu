// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// recordingExecer records every statement and returns a fixed error.
type recordingExecer struct {
	statements []string
	err        error
}

func (r *recordingExecer) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	r.statements = append(r.statements, sql)
	return pgconn.CommandTag{}, r.err
}

// TestSeededTablesMaintenanceSQLVacuumsAndAnalyzesTheSeededTables pins the one
// maintenance statement the seed ends with (#11224): VACUUM (ANALYZE) over every
// bulk-written table, in the pinned table order. VACUUM sets the visibility map,
// which an Index Only Scan reads: without it the entity search walk read 32 to
// 512 buffers depending on how far autovacuum had got when the sweep started.
func TestSeededTablesMaintenanceSQLVacuumsAndAnalyzesTheSeededTables(t *testing.T) {
	t.Parallel()

	sql := seededTablesMaintenanceSQL()
	if !strings.HasPrefix(sql, "VACUUM (ANALYZE) ") {
		t.Fatalf("statement %q must start with VACUUM (ANALYZE)", sql)
	}
	if strings.Contains(strings.ToUpper(sql), "FULL") {
		t.Fatalf("statement %q must not be VACUUM FULL (it rewrites the tables and takes an exclusive lock)", sql)
	}
	want := "VACUUM (ANALYZE) " + strings.Join(analyzedSeededTables(), ", ")
	if sql != want {
		t.Fatalf("statement = %q, want %q", sql, want)
	}
	// The two read-model tables the seed fills before the vacuum (the infra read
	// model by the backfill stand-in, the winners table by ResweepWinners) are read
	// by budgeted routes, so they need the same steady-state visibility map.
	for _, table := range []string{
		"content_entities", "content_files", "fact_records",
		"infra_resource_entities", "supply_chain_impact_canonical_winners",
	} {
		if !strings.Contains(sql, table) {
			t.Errorf("statement %q lacks bulk-written table %s", sql, table)
		}
	}
}

func TestVacuumAnalyzeSeededTablesRunsExactlyOneStatement(t *testing.T) {
	t.Parallel()

	db := &recordingExecer{}
	if err := vacuumAnalyzeSeededTables(context.Background(), db); err != nil {
		t.Fatalf("vacuumAnalyzeSeededTables error = %v", err)
	}
	if len(db.statements) != 1 || db.statements[0] != seededTablesMaintenanceSQL() {
		t.Fatalf("statements = %q, want exactly [%q]", db.statements, seededTablesMaintenanceSQL())
	}

	boom := errors.New("vacuum failed")
	err := vacuumAnalyzeSeededTables(context.Background(), &recordingExecer{err: boom})
	if !errors.Is(err, boom) {
		t.Fatalf("error = %v, want it to wrap %v: a failed maintenance step must fail the seed", err, boom)
	}
}

// seedBody parses main.go and returns the body of the named function.
func seedBody(t *testing.T, fn string) (*token.FileSet, *ast.BlockStmt) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "main.go", nil, 0)
	if err != nil {
		t.Fatalf("parse main.go: %v", err)
	}
	for _, decl := range file.Decls {
		if fd, ok := decl.(*ast.FuncDecl); ok && fd.Name.Name == fn {
			return fset, fd.Body
		}
	}
	t.Fatalf("main.go has no function %s", fn)
	return nil, nil
}

// callName returns the identifier a call expression invokes, or "" for a
// selector or other call shape.
func callName(call *ast.CallExpr) string {
	if id, ok := call.Fun.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// postgresAliases returns the identifiers that name the Postgres pool or DSN in
// body: pool itself and every simple alias of it (`pg := pool`, `dsn :=
// opts.postgresDSN`), so a renamed handle cannot hide a late write.
func postgresAliases(body *ast.BlockStmt) map[string]bool {
	aliases := map[string]bool{"pool": true}
	isPostgres := func(e ast.Expr) bool {
		switch x := e.(type) {
		case *ast.Ident:
			return aliases[x.Name]
		case *ast.SelectorExpr:
			return x.Sel.Name == "postgresDSN"
		}
		return false
	}
	for changed := true; changed; {
		changed = false
		ast.Inspect(body, func(n ast.Node) bool {
			as, ok := n.(*ast.AssignStmt)
			if !ok || len(as.Lhs) != len(as.Rhs) {
				return true
			}
			for i, rhs := range as.Rhs {
				if id, ok := as.Lhs[i].(*ast.Ident); ok && isPostgres(rhs) && !aliases[id.Name] {
					aliases[id.Name] = true
					changed = true
				}
			}
			return true
		})
	}
	return aliases
}

// mentionsPostgres reports whether an expression passes the Postgres pool or the
// Postgres DSN, under any alias.
func mentionsPostgres(expr ast.Expr, aliases map[string]bool) bool {
	found := false
	ast.Inspect(expr, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.Ident:
			if aliases[x.Name] {
				found = true
			}
		case *ast.SelectorExpr:
			if x.Sel.Name == "postgresDSN" {
				found = true
			}
		}
		return !found
	})
	return found
}

// usesPostgres reports whether a call touches Postgres: it takes the pool or DSN
// (under any alias) as an argument, or it is a method called on the pool, such as
// pool.Exec or pool.CopyFrom.
func usesPostgres(call *ast.CallExpr, aliases map[string]bool) bool {
	for _, arg := range call.Args {
		if mentionsPostgres(arg, aliases) {
			return true
		}
	}
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		return mentionsPostgres(sel.X, aliases)
	}
	return false
}

// TestSeedVacuumsAfterEveryPostgresWriteAndBeforeTheSweep checks the call order
// in main.go structurally. The VACUUM runs exactly once, and after it no call in
// seed may touch Postgres: take the pool or the DSN as an argument, or be a
// method on the pool (pool.Exec, pool.CopyFrom), under the name pool or any
// simple alias of it. Any later call is a possible bulk write (including a second
// call to a writer already seen, and a writer added later) that would leave pages
// not all-visible again. The check does not follow a handle passed through a
// struct field or returned from another function. run() must call seed before the
// sweep's first read-model wait.
func TestSeedVacuumsAfterEveryPostgresWriteAndBeforeTheSweep(t *testing.T) {
	t.Parallel()

	_, body := seedBody(t, "seed")
	var vacuumPos token.Pos
	vacuumCalls := 0
	ast.Inspect(body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && callName(call) == "vacuumAnalyzeSeededTables" {
			vacuumCalls++
			vacuumPos = call.Pos()
		}
		return true
	})
	if vacuumCalls != 1 {
		t.Fatalf("seed calls vacuumAnalyzeSeededTables %d times, want exactly 1", vacuumCalls)
	}
	aliases := postgresAliases(body)
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || call.Pos() <= vacuumPos || callName(call) == "vacuumAnalyzeSeededTables" {
			return true
		}
		if usesPostgres(call, aliases) {
			t.Errorf("seed makes a Postgres call after the vacuum (offset %d): a later write leaves pages not all-visible", call.Pos())
		}
		return true
	})

	_, runBody := seedBody(t, "run")
	var seedCall, wait token.Pos
	ast.Inspect(runBody, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			switch callName(call) {
			case "seed":
				seedCall = call.Pos()
			case "awaitInfraReadModel":
				wait = call.Pos()
			}
		}
		return true
	})
	if seedCall == 0 || wait == 0 || seedCall > wait {
		t.Fatal("run must seed before it waits on the infra read model (the first step of the sweep)")
	}
}

// TestSeedPropagatesTheVacuumError pins that a failed maintenance step fails the
// seed: the call must sit in an `if err := ...; err != nil { return ... err }`. The
// helper returns the error; dropping it in main.go would sweep an unvacuumed
// database silently.
func TestSeedPropagatesTheVacuumError(t *testing.T) {
	t.Parallel()

	_, body := seedBody(t, "seed")
	propagated := false
	ast.Inspect(body, func(n ast.Node) bool {
		ifs, ok := n.(*ast.IfStmt)
		if !ok || ifs.Init == nil {
			return true
		}
		assign, ok := ifs.Init.(*ast.AssignStmt)
		if !ok || len(assign.Rhs) != 1 {
			return true
		}
		call, ok := assign.Rhs[0].(*ast.CallExpr)
		if !ok || callName(call) != "vacuumAnalyzeSeededTables" {
			return true
		}
		// The return must carry the error: `return nil` would swallow it.
		returns := false
		ast.Inspect(ifs.Body, func(m ast.Node) bool {
			ret, isRet := m.(*ast.ReturnStmt)
			if !isRet {
				return true
			}
			for _, result := range ret.Results {
				ast.Inspect(result, func(r ast.Node) bool {
					if id, isID := r.(*ast.Ident); isID && id.Name == "err" {
						returns = true
					}
					return true
				})
			}
			return true
		})
		if bin, isBin := ifs.Cond.(*ast.BinaryExpr); isBin && bin.Op == token.NEQ && returns {
			propagated = true
		}
		return true
	})
	if !propagated {
		t.Fatal("seed must call vacuumAnalyzeSeededTables as `if err := ...; err != nil { return ... }`")
	}
}
