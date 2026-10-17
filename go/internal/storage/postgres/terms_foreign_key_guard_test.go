// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// Guard for #7427 PR3: migration 191 drops the eshu_search_index_terms
// generation foreign key, so deleting a scope_generations row no longer
// deletes that generation's terms. No code path may still count on that
// cascade or on the key rejecting a write:
//
//   - every statement that deletes or truncates scope_generations is listed
//     below with the path that removes the generation's terms first;
//   - the one listed site runs only from pruneCandidates, which
//     PruneSupersededGenerations calls after the search index pre-delete and
//     the drained-only admission;
//   - no production statement names the dropped constraint, and the search
//     document writer does not branch on a foreign-key violation (23503).
//
// Rows a late writer adds after the drain are the orphan census and sweep's
// (search/index/retention), and readers never see them (the active
// generation join, eshu_search_index_read_guard_test.go).
//
// Limit: the scan reads each declaration's string literals. A statement split
// across constants, or assembled at run time from parts that each lack the
// table name, passes it; such a path needs a manual review.

// scopeGenerationsDeletePattern matches a DELETE from, or a TRUNCATE that
// names, scope_generations.
var scopeGenerationsDeletePattern = regexp.MustCompile(
	`(?is)\bdelete\s+from\s+(only\s+)?(public\.)?scope_generations\b|\btruncate\b[^;]*\bscope_generations\b`)

// droppedTermsForeignKeyPattern matches either name the dropped key had.
var droppedTermsForeignKeyPattern = regexp.MustCompile(`\beshu_search_index_terms(_shadow)?_generation_id_fkey\b`)

// foreignKeyViolationPattern matches a branch on SQLSTATE 23503.
var foreignKeyViolationPattern = regexp.MustCompile(`23503|ForeignKeyViolation|foreign_key_violation`)

// scopeGenerationDeleteSites lists every reviewed statement that deletes
// scope_generations rows, keyed path:declaration, with what removes the
// generation's terms before it.
var scopeGenerationDeleteSites = map[string]string{
	"internal/storage/postgres/generation_retention_sql.go:deleteScopeGenerationsForRetentionQuery": "pruneCandidates; the pass pre-deletes the generation's terms and documents and admits only drained generations (#7427 PR1)",
}

// stringLiterals concatenates every string literal under node, so comments
// that mention a statement do not count as the statement.
func stringLiterals(node ast.Node) string {
	var text strings.Builder
	ast.Inspect(node, func(n ast.Node) bool {
		if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			if value, err := strconv.Unquote(lit.Value); err == nil {
				text.WriteString(value)
				text.WriteString("\n")
			}
		}
		return true
	})
	return text.String()
}

// goDeclarations calls visit with the name and node of each top-level
// declaration in src: each function and each const or var spec.
func goDeclarations(t *testing.T, rel, src string, visit func(name string, node ast.Node)) {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), rel, src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", rel, err)
	}
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			visit(d.Name.Name, d)
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				if value, ok := spec.(*ast.ValueSpec); ok && len(value.Names) > 0 {
					visit(value.Names[0].Name, value)
				}
			}
		}
	}
}

// termsForeignKeyFindings scans one production file. It returns the
// scope_generations delete sites it holds and every statement that relies on
// the dropped terms generation foreign key.
func termsForeignKeyFindings(t *testing.T, rel, src string) (sites, reliance []string) {
	t.Helper()
	if strings.HasSuffix(rel, ".sql") {
		var code strings.Builder
		for _, line := range strings.Split(src, "\n") {
			if before, _, _ := strings.Cut(line, "--"); strings.TrimSpace(before) != "" {
				code.WriteString(before + "\n")
			}
		}
		if scopeGenerationsDeletePattern.MatchString(code.String()) {
			sites = append(sites, rel)
		}
		return sites, nil
	}
	goDeclarations(t, rel, src, func(name string, node ast.Node) {
		literals := stringLiterals(node)
		if scopeGenerationsDeletePattern.MatchString(literals) {
			sites = append(sites, rel+":"+name)
		}
		if droppedTermsForeignKeyPattern.MatchString(literals) {
			reliance = append(reliance, rel+":"+name+" names the dropped terms generation foreign key")
		}
	})
	if strings.HasPrefix(rel, "internal/reducer/eshusearch/") && foreignKeyViolationPattern.MatchString(src) {
		reliance = append(reliance, rel+" branches on a foreign-key violation, which no longer rejects a terms row for a missing generation")
	}
	return sites, reliance
}

// pruneOrderViolations checks the retention store's source: the listed
// delete runs only from pruneCandidates, pruneCandidates runs only from
// PruneSupersededGenerations, and that method calls the search index
// pre-delete, then the admission, then pruneCandidates.
func pruneOrderViolations(t *testing.T, sources map[string]string) []string {
	t.Helper()
	var violations []string
	referrers := map[string][]string{}
	callOrder := map[string][]token.Pos{}
	for rel, src := range sources {
		goDeclarations(t, rel, src, func(name string, node ast.Node) {
			ast.Inspect(node, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.Ident:
					if x.Name == "deleteScopeGenerationsForRetentionQuery" || x.Name == "pruneCandidates" {
						if x != nodeName(node) {
							referrers[x.Name] = append(referrers[x.Name], name)
						}
					}
				case *ast.CallExpr:
					if sel, ok := x.Fun.(*ast.SelectorExpr); ok && name == "PruneSupersededGenerations" {
						callOrder[sel.Sel.Name] = append(callOrder[sel.Sel.Name], x.Pos())
					}
				}
				return true
			})
		})
	}
	for ident, want := range map[string]string{
		"deleteScopeGenerationsForRetentionQuery": "pruneCandidates",
		"pruneCandidates":                         "PruneSupersededGenerations",
	} {
		if got := slices.Compact(slices.Sorted(slices.Values(referrers[ident]))); !slices.Equal(got, []string{want}) {
			violations = append(violations, fmt.Sprintf("%s is referenced from %v, want only %s", ident, got, want))
		}
	}
	steps := []string{"predeleteSearchIndex", "admit", "pruneCandidates"}
	for i, step := range steps {
		if len(callOrder[step]) != 1 {
			violations = append(violations, fmt.Sprintf("PruneSupersededGenerations calls %s %d times, want once", step, len(callOrder[step])))
			return violations
		}
		if i > 0 && callOrder[step][0] < callOrder[steps[i-1]][0] {
			violations = append(violations, fmt.Sprintf("PruneSupersededGenerations calls %s before %s", step, steps[i-1]))
		}
	}
	return violations
}

// nodeName returns the identifier a declaration declares, so the scan does
// not count a declaration as its own referrer.
func nodeName(node ast.Node) *ast.Ident {
	switch d := node.(type) {
	case *ast.FuncDecl:
		return d.Name
	case *ast.ValueSpec:
		return d.Names[0]
	}
	return nil
}

// TestNoCodePathReliesOnTheTermsGenerationForeignKey runs the guard over the
// module's production Go files and the migrations.
func TestNoCodePathReliesOnTheTermsGenerationForeignKey(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	var sites, reliance []string
	retentionSources := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if name := entry.Name(); name == "vendor" || name == "testdata" || name == ".gocache" || name == "testfixtures" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		isGo := strings.HasSuffix(rel, ".go") && !strings.HasSuffix(rel, "_test.go")
		isMigration := strings.HasPrefix(rel, "internal/storage/postgres/migrations/") && strings.HasSuffix(rel, ".sql")
		if !isGo && !isMigration {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		found, relied := termsForeignKeyFindings(t, rel, string(src))
		sites = append(sites, found...)
		reliance = append(reliance, relied...)
		if isGo && strings.HasPrefix(rel, "internal/storage/postgres/generation_retention") {
			retentionSources[rel] = string(src)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk module: %v", err)
	}
	if len(retentionSources) < 5 {
		t.Fatalf("found %d retention store files, want at least 5: the scan is broken", len(retentionSources))
	}
	for _, site := range sites {
		if _, ok := scopeGenerationDeleteSites[site]; !ok {
			reliance = append(reliance, site+" deletes scope_generations rows without a reviewed terms delete: add the explicit search index delete and list it")
		}
	}
	for site := range scopeGenerationDeleteSites {
		if !slices.Contains(sites, site) {
			reliance = append(reliance, site+" is listed but no longer deletes scope_generations: remove the entry")
		}
	}
	reliance = append(reliance, pruneOrderViolations(t, retentionSources)...)
	if len(reliance) > 0 {
		t.Fatalf("code that relies on the dropped terms generation foreign key:\n%s", strings.Join(reliance, "\n"))
	}
}

// TestTermsForeignKeyGuardCatchesPlantedViolations is the guard's RED/GREEN
// pair.
func TestTermsForeignKeyGuardCatchesPlantedViolations(t *testing.T) {
	for name, tc := range map[string]struct {
		rel, src        string
		sites, reliance int
	}{
		"unlisted delete": {"internal/x/a.go", "package x\nconst q = `DELETE FROM scope_generations WHERE generation_id = $1`\n", 1, 0},
		"truncate list":   {"internal/x/b.go", "package x\nconst q = `TRUNCATE fact_work_items, scope_generations CASCADE`\n", 1, 0},
		"comment only":    {"internal/x/c.go", "package x\n// DELETE FROM scope_generations\nconst q = `SELECT 1 FROM scope_generations`\n", 0, 0},
		"sql delete":      {"internal/storage/postgres/migrations/999_x.sql", "DELETE FROM scope_generations WHERE false;\n", 1, 0},
		"sql comment":     {"internal/storage/postgres/migrations/998_x.sql", "-- DELETE FROM scope_generations\nSELECT 1;\n", 0, 0},
		"constraint name": {"internal/x/d.go", "package x\nconst q = `ALTER TABLE eshu_search_index_terms VALIDATE CONSTRAINT eshu_search_index_terms_shadow_generation_id_fkey`\n", 0, 1},
		"writer 23503":    {"internal/reducer/eshusearch/e.go", "package x\nfunc f(code string) bool { return code == \"23503\" }\n", 0, 1},
		"other 23503":     {"internal/x/f.go", "package x\nfunc f(code string) bool { return code == \"23503\" }\n", 0, 0},
	} {
		sites, reliance := termsForeignKeyFindings(t, tc.rel, tc.src)
		if len(sites) != tc.sites || len(reliance) != tc.reliance {
			t.Errorf("%s: sites %v, reliance %v; want %d and %d", name, sites, reliance, tc.sites, tc.reliance)
		}
	}

	const sqlFile = "package postgres\nconst deleteScopeGenerationsForRetentionQuery = `DELETE FROM scope_generations`\n"
	ordered := "package postgres\n" +
		"func (s S) pruneCandidates() { _ = deleteScopeGenerationsForRetentionQuery }\n" +
		"func (s S) PruneSupersededGenerations() { si := s.predeleteSearchIndex(); si.admit(); s.pruneCandidates() }\n"
	if got := pruneOrderViolations(t, map[string]string{"a.go": sqlFile, "b.go": ordered}); len(got) != 0 {
		t.Errorf("ordered pipeline: violations %v, want none", got)
	}
	for name, src := range map[string]string{
		"prune before admit": "package postgres\n" +
			"func (s S) pruneCandidates() { _ = deleteScopeGenerationsForRetentionQuery }\n" +
			"func (s S) PruneSupersededGenerations() { si := s.predeleteSearchIndex(); s.pruneCandidates(); si.admit() }\n",
		"no pre-delete": "package postgres\n" +
			"func (s S) pruneCandidates() { _ = deleteScopeGenerationsForRetentionQuery }\n" +
			"func (s S) PruneSupersededGenerations() { si := s.other(); si.admit(); s.pruneCandidates() }\n",
		"second delete caller": ordered + "func (s S) purge() { _ = deleteScopeGenerationsForRetentionQuery }\n",
		"second prune caller":  ordered + "func (s S) purge() { s.pruneCandidates() }\n",
	} {
		if got := pruneOrderViolations(t, map[string]string{"a.go": sqlFile, "b.go": src}); len(got) == 0 {
			t.Errorf("%s: no violation, want one", name)
		}
	}
}
