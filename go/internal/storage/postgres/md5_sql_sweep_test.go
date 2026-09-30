// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// md5CallPattern matches a SQL md5() function call. OpenSSL FIPS rejects the
// MD5 digest, so any production SQL calling md5() fails on FIPS-enabled
// PostgreSQL (#6753); production SQL must use sha256 instead. The word
// boundary keeps identifiers such as nomd5x( out, and the case-insensitive
// flag keeps MD5 ( out. Go-side crypto/md5 use is out of scope: import paths
// are string literals but contain no call paren and never match, and
// selector calls such as md5.New() never appear in a literal.
var md5CallPattern = regexp.MustCompile(`(?i)\bmd5\s*\(`)

// md5SweepGoRoots are the production trees, relative to the go/ module root,
// whose non-test Go string literals can carry SQL.
var md5SweepGoRoots = []string{"."}

// md5SweepSQLFiles are the shipped schema-SQL surfaces, relative to the
// repository root, that execute as SQL. Testdata, fixtures, docs evidence,
// and script proof helpers are not shipped schema and stay out of scope.
var md5SweepSQLFiles = []string{
	"go/internal/storage/postgres/migrations",
	"schema/data-plane/postgres",
}

// md5SweepMinLiterals is the floor on Go string literals the sweep must
// examine; the tree held well above this when the gate landed. It keeps a
// broken root or walker from passing vacuously.
const md5SweepMinLiterals = 1000

// md5CallSitesInLiterals returns the literals that call SQL md5().
func md5CallSitesInLiterals(literals []string) []string {
	var sites []string
	for _, literal := range literals {
		if md5CallPattern.MatchString(literal) {
			sites = append(sites, literal)
		}
	}
	return sites
}

// collectGoStringLiterals walks root for non-test .go files and returns every
// string literal value with its file and line.
func collectGoStringLiterals(t *testing.T, root string) ([]string, map[string]string) {
	t.Helper()
	var literals []string
	origins := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		fileSet := token.NewFileSet()
		parsed, err := parser.ParseFile(fileSet, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind.String() != "STRING" {
				return true
			}
			value, err := strconv.Unquote(literal.Value)
			if err != nil {
				return true
			}
			literals = append(literals, value)
			position := fileSet.Position(literal.Pos())
			origins[value] = position.String()
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk Go sources under %s: %v", root, err)
	}
	return literals, origins
}

// collectShippedSQLFiles returns the .sql files under the shipped schema
// roots.
func collectShippedSQLFiles(t *testing.T, repoRoot string) []string {
	t.Helper()
	var files []string
	for _, rel := range md5SweepSQLFiles {
		root := filepath.Join(repoRoot, rel)
		entries, err := os.ReadDir(root)
		if err != nil {
			t.Fatalf("read shipped SQL root %s: %v", root, err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
				continue
			}
			files = append(files, filepath.Join(root, entry.Name()))
		}
	}
	sort.Strings(files)
	return files
}

func repoRootFromTest(t *testing.T) string {
	t.Helper()
	// This file lives at go/internal/storage/postgres; the go/ module root
	// is three levels up and the repository root one more.
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}
	return filepath.Clean(filepath.Join(dir, "..", "..", "..", ".."))
}

// TestProductionSQLHasNoMD5Calls sweeps production Go string literals and
// shipped schema SQL for md5() calls, which fail on FIPS-enabled PostgreSQL
// (#6753). Tests, fixtures, and evidence files may still use md5(); only
// shipped SQL is in scope.
func TestProductionSQLHasNoMD5Calls(t *testing.T) {
	repoRoot := repoRootFromTest(t)
	goRoot := filepath.Join(repoRoot, "go")
	var literals []string
	origins := map[string]string{}
	for _, rel := range md5SweepGoRoots {
		found, foundOrigins := collectGoStringLiterals(t, filepath.Join(goRoot, rel))
		literals = append(literals, found...)
		for value, origin := range foundOrigins {
			origins[value] = origin
		}
	}
	if len(literals) < md5SweepMinLiterals {
		t.Fatalf("examined %d Go string literals, want at least %d", len(literals), md5SweepMinLiterals)
	}
	var violations []string
	for _, site := range md5CallSitesInLiterals(literals) {
		violations = append(violations, "go literal "+origins[site]+": "+site)
	}
	for _, path := range collectShippedSQLFiles(t, repoRoot) {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read shipped SQL %s: %v", path, err)
		}
		for _, site := range md5CallSitesInLiterals([]string{string(raw)}) {
			lines := strings.Split(site, "\n")
			for i, line := range lines {
				if md5CallPattern.MatchString(line) {
					violations = append(violations, path+":"+strconv.Itoa(i+1)+": "+strings.TrimSpace(line))
				}
			}
		}
	}
	if len(violations) > 0 {
		t.Fatalf("production SQL md5() calls (FIPS-incompatible, #6753):\n%s", strings.Join(violations, "\n"))
	}
}

// TestMD5SweepFlagsSeededViolation pins the detector's sensitivity: seeded
// production-shaped violations fail, and lookalikes pass.
func TestMD5SweepFlagsSeededViolation(t *testing.T) {
	violations := md5CallSitesInLiterals([]string{
		"SELECT md5(payload::text)",
		"MIN(MD5 (fingerprint))",
		"encode(md5(convert_to(key, 'UTF8')), 'hex')",
		"SELECT\n  md5(a || b)\nFROM t",
	})
	if len(violations) != 4 {
		t.Fatalf("seeded violations flagged = %d, want 4", len(violations))
	}
	clean := md5CallSitesInLiterals([]string{
		"SELECT sha256(convert_to(payload::text, 'UTF8'))",
		"SELECT nomd5x(payload)",
		"crypto/md5",
		"md5 without a call",
	})
	if len(clean) != 0 {
		t.Fatalf("clean literals flagged = %q, want none", clean)
	}
}
