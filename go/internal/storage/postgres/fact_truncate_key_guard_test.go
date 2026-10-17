// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
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

// factTruncateStatement matches a TRUNCATE whose table list names
// fact_records, or a parent whose CASCADE reaches it.
var factTruncateStatement = regexp.MustCompile(`(?is)\bTRUNCATE\s+(TABLE\s+)?(ONLY\s+)?[^;]*?\b(fact_records|scope_generations|ingestion_scopes)\b`)

// factTruncateSite classifies one truncate of the fact parents: the key
// sidecar tables its literal must name next to fact_records, and why that
// set is complete for its schema.
type factTruncateSite struct {
	keyTables []string
	reason    string
}

// factTruncateSites is every TRUNCATE of fact_records (or a CASCADE from its
// parents) in Go under go/, tests included, keyed by "<path under go/>:<func
// or const>". Since migrations 188 to 190 no foreign key makes a bare
// TRUNCATE fact_records fail or cascade to the key sidecars (#7420), so a new
// truncate must be classified here with the key tables it empties beside the
// facts. Production code truncates none of these tables.
var factTruncateSites = map[string]factTruncateSite{
	"internal/storage/postgres/relationship_family_index_write_tax_helpers_test.go:runRelationshipFamilyWriteTaxRound": {
		keyTables: []string{"relationship_reference_candidate_keys"},
		reason:    "the proof schema (deferredPartitionProofSchemaSQL) holds fact_records and only the relationship key sidecar",
	},
	"internal/storage/postgres/supersede_prior_failure_cost_live_test.go:costReset": {
		reason: "seeds only scopes, generations and queue rows; it writes no facts or key rows",
	},
	"internal/storage/postgres/terms_foreign_key_guard_test.go:TestTermsForeignKeyGuardCatchesPlantedViolations": {
		reason: "a planted literal in the terms foreign key guard's seeded RED table: it is parsed by the guard under test and never executed",
	},
}

// TestFactRecordTruncateSitesAreKeySidecarSafe fails on a TRUNCATE of
// fact_records (or a CASCADE from its parents) anywhere in go/, tests
// included, that is unclassified or does not name its classified key
// sidecar tables in the same statement.
func TestFactRecordTruncateSitesAreKeySidecarSafe(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	sources := parseTruncateGuardSources(t, fset, "../../..")
	if violations := factTruncateGuardViolations(fset, sources, factTruncateSites); len(violations) > 0 {
		t.Fatalf("fact_records truncates not key-sidecar safe:\n%s", strings.Join(violations, "\n"))
	}
}

// TestFactRecordTruncateGuardCatchesPlantedViolations is the guard's seeded
// RED: each planted truncate must be reported and the clean twin must not.
func TestFactRecordTruncateGuardCatchesPlantedViolations(t *testing.T) {
	t.Parallel()

	const clean = `package p
func reset() { exec("TRUNCATE fact_records, relationship_reference_candidate_keys"); exec("TRUNCATE fact_work_items") }
func exempt() { exec("TRUNCATE scope_generations CASCADE") }
`
	sites := map[string]factTruncateSite{
		"p/p_test.go:reset":  {keyTables: []string{"relationship_reference_candidate_keys"}},
		"p/p_test.go:exempt": {reason: "writes no facts"},
	}
	for _, tc := range []struct {
		name, source string
		wantClean    bool
	}{
		{"clean", clean, true},
		{"keys-missing", strings.Replace(clean, "fact_records, relationship_reference_candidate_keys", "fact_records", 1), false},
		{"unclassified", strings.Replace(clean, `exec("TRUNCATE fact_work_items")`, `exec("TRUNCATE TABLE ONLY fact_records")`, 1), false},
		{"cascade-from-scopes", strings.Replace(clean, "func exempt()", "func other()", 1), false},
		{"stale-site", strings.Replace(clean, `exec("TRUNCATE fact_records, relationship_reference_candidate_keys"); `, "", 1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "p_test.go", tc.source, 0)
			if err != nil {
				t.Fatalf("parse planted source: %v", err)
			}
			sources := []truncateGuardSource{{path: "p/p_test.go", file: file}}
			violations := factTruncateGuardViolations(fset, sources, sites)
			if tc.wantClean && len(violations) > 0 {
				t.Fatalf("clean source reported %v", violations)
			}
			if !tc.wantClean && len(violations) == 0 {
				t.Fatalf("planted %s violation not reported", tc.name)
			}
		})
	}
}

// truncateGuardSource is one Go file, tests included, by its path under go/.
type truncateGuardSource struct {
	path string
	file *ast.File
}

func parseTruncateGuardSources(t *testing.T, fset *token.FileSet, root string) []truncateGuardSource {
	t.Helper()
	var sources []truncateGuardSource
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		// The guard's own planted sources are literals; skip them.
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "fact_truncate_key_guard_test.go") {
			return nil
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !bytes.Contains(bytes.ToUpper(source), []byte("TRUNCATE")) {
			return nil
		}
		file, err := parser.ParseFile(fset, path, source, 0)
		if err != nil {
			return err
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		_, rel, ok := strings.Cut(filepath.ToSlash(abs), "/go/")
		if !ok {
			rel = filepath.ToSlash(path)
		}
		sources = append(sources, truncateGuardSource{path: rel, file: file})
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return sources
}

// factTruncateGuardViolations reports each fact truncate string literal that
// is unclassified or omits a classified key table, and each classification
// that names no truncate.
func factTruncateGuardViolations(fset *token.FileSet, sources []truncateGuardSource, sites map[string]factTruncateSite) []string {
	var violations []string
	found := map[string]bool{}
	for _, source := range sources {
		for _, decl := range source.file.Decls {
			name := ""
			switch d := decl.(type) {
			case *ast.FuncDecl:
				name = d.Name.Name
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					if value, ok := spec.(*ast.ValueSpec); ok && len(value.Names) > 0 {
						name = value.Names[0].Name
					}
				}
			}
			key := source.path + ":" + name
			ast.Inspect(decl, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				text, err := strconv.Unquote(lit.Value)
				if err != nil || !factTruncateStatement.MatchString(text) {
					return true
				}
				found[key] = true
				site, ok := sites[key]
				if !ok {
					violations = append(violations, fmt.Sprintf("%s: unclassified fact truncate %s", fset.Position(lit.Pos()), key))
					return true
				}
				for _, table := range site.keyTables {
					if !regexp.MustCompile(`\b` + table + `\b`).MatchString(text) {
						violations = append(violations, fmt.Sprintf("%s: %s truncates fact_records without %s", fset.Position(lit.Pos()), key, table))
					}
				}
				return true
			})
		}
	}
	for key := range sites {
		if !found[key] {
			violations = append(violations, "stale truncate classification with no truncate: "+key)
		}
	}
	slices.Sort(violations)
	return violations
}
