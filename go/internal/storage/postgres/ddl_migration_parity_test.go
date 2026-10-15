// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The schema Job (bootstrap-data-plane, or bootstrap-index locally) runs
// ApplyBootstrap, which applies only the numbered migrations. Some stores
// also carry their own DDL in Go and apply it from an EnsureSchema call that
// only their owning binaries run (webhook-listener, workflow-coordinator). A
// column, index, or table that exists only in that Go DDL is missing on every
// database the schema Job built until an owner starts, and every other binary
// that reads or writes it fails (#7777, #7792). This guard fails the build when Go DDL
// declares schema the numbered migrations do not.

// ddlParityScanRoots are the Go trees, relative to the module root, whose
// non-test string literals the guard scans for DDL.
var ddlParityScanRoots = []string{"internal/storage/postgres", "cmd"}

// ddlParityGoOnlyTables names tables that Go DDL may create without a
// numbered migration. Each entry needs a reason and an issue link, and an
// entry that matches nothing fails the guard. It is empty: every table Go DDL
// creates today is also created by a migration.
var ddlParityGoOnlyTables = map[string]string{}

// ddlParityDynamicDDL names Go DDL whose target the scan cannot read because
// the name or a clause is formatted or joined at runtime, keyed by
// "<file relative to go/>|<top-level declaration>|<statement head>". Each
// entry needs a reason. A new dynamic statement fails the guard until it is
// listed here, and an entry that matches nothing fails until it is removed.
var ddlParityDynamicDDL = map[string]string{
	"internal/storage/postgres/schema_bootstrap_lock.go|schemaMigrationsTableSQL|CREATE TABLE IF NOT EXISTS %s": "the " +
		"eshu_schema_migrations ledger, schema-qualified at runtime; ApplyBootstrap creates it " +
		"itself before it applies any numbered migration, so the schema Job always has it (#7792)",
}

// TestDDLMigrationParity checks Go DDL in non-test files under
// go/internal/storage/postgres and go/cmd against the embedded numbered
// migrations the schema Job applies. It fails when Go DDL declares:
//
//  1. a column (CREATE TABLE column list or ALTER TABLE ADD [COLUMN]) on a
//     migration-created table that no migration declares;
//  2. an index name no migration creates, or one a migration puts on
//     another table;
//  3. a table no migration creates, unless ddlParityGoOnlyTables lists it;
//  4. a named constraint or trigger on a migration-created table, or a
//     function, that no migration declares;
//  5. a statement the scan cannot read, including an ALTER TABLE action that
//     matches no form in ddlAlterForms, unless ddlParityDynamicDDL lists it.
//
// A stale entry in either allowlist also fails. The guard compares names
// only. It does not see CREATE EXTENSION or ALTER COLUMN, and it does not
// compare definitions: column types and defaults, index columns and
// predicates, or constraint, trigger, and function bodies.
func TestDDLMigrationParity(t *testing.T) {
	t.Parallel()

	migInv, goInv := ddlParityRealTree(t)
	assertDDLParityFloor(t, migInv, goInv)
	for _, gap := range ddlParityGaps(goInv, migInv, ddlParityGoOnlyTables, ddlParityDynamicDDL) {
		t.Errorf("%s\n  The schema Job applies only go/internal/storage/postgres/migrations/*.sql; add a numbered migration that declares the same schema.", gap)
	}
}

// ddlParityRealTree builds the migration inventory from the embedded
// bootstrap definitions the schema Job applies, and the Go inventory from
// string literals under ddlParityScanRoots.
func ddlParityRealTree(t *testing.T) (ddlInventory, ddlInventory) {
	t.Helper()

	var migSources []ddlSource
	for _, def := range BootstrapDefinitions() {
		migSources = append(migSources, ddlSource{file: def.Path, line: 1, sql: def.SQL})
	}
	goSources, err := goStringLiteralSources(filepath.Join("..", "..", ".."), ddlParityScanRoots)
	if err != nil {
		t.Fatalf("scan Go string literals: %v", err)
	}
	return extractDDLInventory(migSources), extractDDLInventory(goSources)
}

// assertDDLParityFloor fails when the extractor reads implausibly little, so
// a broken extractor cannot turn the guard silently green.
func assertDDLParityFloor(t *testing.T, migInv, goInv ddlInventory) {
	t.Helper()

	migKinds, goKinds := ddlObjectKinds(migInv), ddlObjectKinds(goInv)
	t.Logf("migrations: %d tables, %d columns, %d indexes, objects %v; Go DDL: %d tables, %d columns, %d indexes, objects %v, %d unresolved",
		len(migInv.tables), len(migInv.columns), len(migInv.indexes), migKinds,
		len(goInv.tables), len(goInv.columns), len(goInv.indexes), goKinds, len(goInv.unresolved))
	floors := []struct {
		name      string
		got, want int
	}{
		{"migration tables", len(migInv.tables), 100},
		{"migration columns", len(migInv.columns), 1000},
		{"migration indexes", len(migInv.indexes), 300},
		{"Go DDL tables", len(goInv.tables), 20},
		{"Go DDL columns", len(goInv.columns), 200},
		{"Go DDL indexes", len(goInv.indexes), 50},
		{"migration constraints", migKinds["constraint"], 10},
		{"migration triggers", migKinds["trigger"], 3},
		{"migration functions", migKinds["function"], 3},
		// The Go-side object floors sit just under today's counts (6
		// constraints, 4 triggers, 3 functions). Every trigger and function
		// comes from the container image identity DDL
		// (schema_container_image_identity_*.go). Lower a floor only when
		// that DDL leaves Go, never to silence a broken extractor.
		{"Go DDL constraints", goKinds["constraint"], 3},
		{"Go DDL triggers", goKinds["trigger"], 3},
		{"Go DDL functions", goKinds["function"], 2},
	}
	for _, f := range floors {
		if f.got < f.want {
			t.Errorf("extracted %d %s, want at least %d; the DDL extractor is likely broken", f.got, f.name, f.want)
		}
	}
	if _, ok := goInv.columns[ddlColumn{table: "webhook_refresh_triggers", column: "claim_fencing_token"}]; !ok {
		t.Error("Go DDL scan missed webhook_refresh_triggers.claim_fencing_token from the webhook store EnsureSchema")
	}
}

// ddlObjectKinds counts an inventory's named objects by kind.
func ddlObjectKinds(inv ddlInventory) map[string]int {
	counts := map[string]int{}
	for obj := range inv.objects {
		counts[obj.kind]++
	}
	return counts
}

// goStringLiteralSources parses every non-test .go file under roots
// (relative to goRoot), skipping the migrations directory, and returns each
// string literal as a ddlSource. The migrations are read from the embedded
// definitions instead, and testdata trees are not compiled. A chain of string
// literals joined with + is folded into one source. Comments are never read, so DDL in a doc comment
// does not count.
func goStringLiteralSources(goRoot string, roots []string) ([]ddlSource, error) {
	var sources []ddlSource
	fset := token.NewFileSet()
	migrationsDir := filepath.Join(goRoot, "internal", "storage", "postgres", "migrations")
	for _, root := range roots {
		err := filepath.WalkDir(filepath.Join(goRoot, root), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if path == migrationsDir || d.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			fileSources, err := goFileStringLiterals(fset, goRoot, path)
			sources = append(sources, fileSources...)
			return err
		})
		if err != nil {
			return nil, err
		}
	}
	sort.SliceStable(sources, func(i, j int) bool {
		if sources[i].file != sources[j].file {
			return sources[i].file < sources[j].file
		}
		return sources[i].line < sources[j].line
	})
	return sources, nil
}

// goFileStringLiterals returns the string literals of one Go file, with
// literal-only + chains folded and each tagged with the top-level
// declaration that holds it. A file that never mentions TABLE, INDEX,
// TRIGGER, or FUNCTION in any case cannot hold DDL and is not parsed.
func goFileStringLiterals(fset *token.FileSet, goRoot, path string) ([]ddlSource, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lower := bytes.ToLower(src)
	if !bytes.Contains(lower, []byte("table")) && !bytes.Contains(lower, []byte("index")) &&
		!bytes.Contains(lower, []byte("trigger")) && !bytes.Contains(lower, []byte("function")) {
		return nil, nil
	}
	file, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(goRoot, path)
	if err != nil {
		return nil, err
	}
	rel = filepath.ToSlash(rel)
	var sources []ddlSource
	visit := func(scope string, root ast.Node) {
		ast.Inspect(root, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.BinaryExpr:
				if text, ok := foldStringConcat(node); ok {
					sources = append(sources, ddlSource{file: rel, scope: scope, line: fset.Position(node.Pos()).Line, sql: text})
					return false
				}
			case *ast.BasicLit:
				if node.Kind == token.STRING {
					if text, err := strconv.Unquote(node.Value); err == nil {
						sources = append(sources, ddlSource{file: rel, scope: scope, line: fset.Position(node.Pos()).Line, sql: text})
					}
				}
			}
			return true
		})
	}
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			visit(goFuncScope(d), d)
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				visit(goSpecScope(spec), spec)
			}
		}
	}
	return sources, nil
}

// foldStringConcat returns the value of an expression made only of string
// literals joined with +, and false for anything else.
func foldStringConcat(expr ast.Expr) (string, bool) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", false
		}
		text, err := strconv.Unquote(e.Value)
		return text, err == nil
	case *ast.ParenExpr:
		return foldStringConcat(e.X)
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return "", false
		}
		left, ok := foldStringConcat(e.X)
		if !ok {
			return "", false
		}
		right, ok := foldStringConcat(e.Y)
		return left + right, ok
	}
	return "", false
}

// goFuncScope names a function or method declaration, such as "T.Method".
func goFuncScope(d *ast.FuncDecl) string {
	if d.Recv == nil || len(d.Recv.List) == 0 {
		return d.Name.Name
	}
	recv := d.Recv.List[0].Type
	if star, ok := recv.(*ast.StarExpr); ok {
		recv = star.X
	}
	if index, ok := recv.(*ast.IndexExpr); ok {
		recv = index.X
	}
	if ident, ok := recv.(*ast.Ident); ok {
		return ident.Name + "." + d.Name.Name
	}
	return d.Name.Name
}

// goSpecScope names a const, var, or type spec by its identifiers.
func goSpecScope(spec ast.Spec) string {
	switch s := spec.(type) {
	case *ast.ValueSpec:
		names := make([]string, 0, len(s.Names))
		for _, name := range s.Names {
			names = append(names, name.Name)
		}
		return strings.Join(names, ",")
	case *ast.TypeSpec:
		return s.Name.Name
	}
	return ""
}
