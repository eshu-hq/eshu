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
	"strconv"
	"strings"
	"testing"
)

// Guard for #7427 c2: a superseded generation's search index rows are
// invisible to every reader. The search index pre-delete removes them in
// committed batches before the prune, so a reader that could see them would
// see a generation half-deleted. Every production statement that names
// eshu_search_index_terms or eshu_search_index_documents must therefore join
// ingestion_scopes.active_generation_id, except the owners of those rows'
// lifecycle below. The statements are enumerated from the module's Go source.

// searchIndexTablePattern names either search index table as a whole word.
var searchIndexTablePattern = regexp.MustCompile(`\beshu_search_index_(terms|documents)\b`)

// searchIndexLifecycleOwners are the paths, relative to the go/ module root,
// whose statements write or retire search index rows and so name the tables
// without reading through the active generation.
var searchIndexLifecycleOwners = []string{
	// The reducer projection writer: inserts, retires and clears one
	// generation's rows by (scope_id, generation_id).
	"internal/reducer/eshusearch/",
	// The #7427 pre-delete: deletes superseded generations' rows.
	"internal/storage/postgres/search/index/retention/",
	// Generation retention: counts a pruned generation's rows.
	"internal/storage/postgres/generation_retention_",
}

// searchIndexReadAllowlist names reviewed files whose statements name the
// tables without the active-generation join, with the reason.
var searchIndexReadAllowlist = map[string]string{
	"internal/storage/postgres/adapters.go": "COPY target table name for the reducer projection writer's term load",
}

// searchIndexReadViolations parses one Go file and reports every top-level
// declaration (each const or var spec, or each function) whose string
// literals name a search index table but never active_generation_id, plus how
// many such statements it saw. Literals, not comments, are read, so a comment
// that mentions the join is not the join.
func searchIndexReadViolations(t *testing.T, filename, src string) (violations []string, readers int) {
	t.Helper()
	if !searchIndexTablePattern.MatchString(src) {
		return nil, 0
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", filename, err)
	}
	check := func(node ast.Node, name string) {
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
		if !searchIndexTablePattern.MatchString(text.String()) {
			return
		}
		readers++
		if !strings.Contains(text.String(), "active_generation_id") {
			violations = append(violations, fmt.Sprintf("%s:%d %s names a search index table without the ingestion_scopes.active_generation_id join",
				filename, fset.Position(node.Pos()).Line, name))
		}
	}
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			check(d, d.Name.Name)
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				if value, ok := spec.(*ast.ValueSpec); ok && len(value.Names) > 0 {
					check(value, value.Names[0].Name)
				}
			}
		}
	}
	return violations, readers
}

// TestSearchIndexReadersJoinTheActiveGeneration holds every production reader
// of the search index tables to the active-generation join. It fails if the
// scan finds fewer readers than existed when it landed, so a broken pattern
// cannot pass by matching nothing.
func TestSearchIndexReadersJoinTheActiveGeneration(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	var violations []string
	readers := 0
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
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		for _, owner := range searchIndexLifecycleOwners {
			if strings.HasPrefix(rel, owner) {
				return nil
			}
		}
		if _, ok := searchIndexReadAllowlist[rel]; ok {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		found, count := searchIndexReadViolations(t, rel, string(src))
		violations = append(violations, found...)
		readers += count
		return nil
	})
	if err != nil {
		t.Fatalf("walk module: %v", err)
	}
	if readers < 6 {
		t.Fatalf("found %d search index readers, want at least 6: the scan is broken", readers)
	}
	if len(violations) > 0 {
		t.Fatalf("search index readers that can see a superseded generation:\n%s", strings.Join(violations, "\n"))
	}
}

// TestSearchIndexReadGuardSeededViolation is the guard's RED/GREEN pair.
func TestSearchIndexReadGuardSeededViolation(t *testing.T) {
	for name, tc := range map[string]struct {
		src  string
		want int
	}{
		"bare read": {"package p\nconst q = `SELECT document_id FROM eshu_search_index_terms WHERE scope_id = $1`\n", 1},
		"joined read": {"package p\nconst q = `SELECT t.document_id FROM eshu_search_index_terms t\n" +
			"JOIN ingestion_scopes s ON s.scope_id = t.scope_id AND s.active_generation_id = t.generation_id`\n", 0},
		"builder read": {"package p\nfunc q(b interface{ WriteString(string) }) {\n" +
			"\tb.WriteString(\"FROM eshu_search_index_documents AS doc\\n\")\n" +
			"\tb.WriteString(\" AND scope.active_generation_id = doc.generation_id\\n\")\n}\n", 0},
		"comment is no join": {"package p\n// active_generation_id\nconst q = `SELECT 1 FROM eshu_search_index_documents`\n", 1},
		"other table":        {"package p\nconst q = `SELECT 1 FROM eshu_search_index_stats`\n", 0},
	} {
		if got, _ := searchIndexReadViolations(t, name+".go", tc.src); len(got) != tc.want {
			t.Errorf("%s: violations = %v, want %d", name, got, tc.want)
		}
	}
}
