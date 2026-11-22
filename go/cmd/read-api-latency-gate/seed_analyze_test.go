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
	for _, table := range []string{"content_entities", "content_files", "fact_records"} {
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

// TestSeedVacuumsAfterTheLastBulkWriteAndBeforeTheSweep checks the call order in
// main.go structurally: the VACUUM must follow every bulk Postgres write in seed
// (a later write would leave pages not all-visible again) and seed must run
// before run starts the sweep's first read-model wait.
func TestSeedVacuumsAfterTheLastBulkWriteAndBeforeTheSweep(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "main.go", nil, 0)
	if err != nil {
		t.Fatalf("parse main.go: %v", err)
	}
	callPositions := func(fn string) map[string]token.Pos {
		positions := map[string]token.Pos{}
		for _, decl := range file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Name.Name != fn {
				continue
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				if id, ok := call.Fun.(*ast.Ident); ok {
					if _, seen := positions[id.Name]; !seen {
						positions[id.Name] = call.Pos()
					}
				}
				return true
			})
		}
		return positions
	}

	seedCalls := callPositions("seed")
	vacuum, ok := seedCalls["vacuumAnalyzeSeededTables"]
	if !ok {
		t.Fatal("seed does not call vacuumAnalyzeSeededTables")
	}
	for _, write := range []string{
		"SeedPostgres", "SeedIaCFacts", "SeedSharedIntents", "SeedWinnersFactRecords",
		"rebuildSeededWinners", "SeedInfraContentEntities", "SeedContentFiles",
		"finalizeContentSearchIndexes", "backfillSeededInfraReadModel",
	} {
		pos, called := seedCalls[write]
		if !called {
			continue
		}
		if pos > vacuum {
			t.Errorf("seed calls %s after vacuumAnalyzeSeededTables: a later bulk write leaves pages not all-visible", write)
		}
	}

	runCalls := callPositions("run")
	seedCall, ok := runCalls["seed"]
	if !ok {
		t.Fatal("run does not call seed")
	}
	if wait, ok := runCalls["awaitInfraReadModel"]; !ok || seedCall > wait {
		t.Fatal("run must seed before it waits on the infra read model (the first step of the sweep)")
	}
}
