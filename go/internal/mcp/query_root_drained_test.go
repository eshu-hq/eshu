// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
)

// queryRootImport is the in-tree query root #9669 drains go/internal/mcp off.
// Row-2 leaf 2a retargets the Group A/D/B files (dispatch envelope and
// budget, summaries and views, the in-process read seam) at querycontract
// (+answer for AnswerMetadata, +contentread for ContentSearchMaxOffset);
// only the Group C auth/policy files below still name it. It is built by
// concatenation so this gate's own source does not match the
// module-prefixed import census (the Group E string-literal trap in
// kind_real_consumer.go).
const queryRootImport = "github.com/eshu-hq/eshu/go/internal/" + "query"

// queryRootDrainExceptions names the Group C auth/policy files the row-2
// leaf-2a drain deliberately leaves on the query root (router/access.go,
// transport_auth.go). Every other non-test file under go/internal/mcp must
// not import the query root.
var queryRootDrainExceptions = []string{
	"router/access.go",
	"transport_auth.go",
}

// TestQueryRootDrained pins the row-2 leaf-2a drain: no non-test file under
// go/internal/mcp imports the query root except the two Group C exceptions.
// It parses imports (not raw text) so the Group E string literal in
// kind_real_consumer.go does not count as an import.
func TestQueryRootDrained(t *testing.T) {
	allowed := map[string]bool{}
	for _, name := range queryRootDrainExceptions {
		allowed[name] = true
	}
	var offenders []string
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".go" {
			return nil //nolint:nilerr // an unreadable entry is skipped, not fatal
		}
		if len(path) >= len("_test.go") && path[len(path)-len("_test.go"):] == "_test.go" {
			return nil
		}
		fset := token.NewFileSet()
		file, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			t.Fatalf("ParseFile(%s) error = %v, want nil", path, parseErr)
		}
		for _, spec := range file.Imports {
			importPath, unquoteErr := strconv.Unquote(spec.Path.Value)
			if unquoteErr != nil {
				t.Fatalf("Unquote(%s) error = %v, want nil", spec.Path.Value, unquoteErr)
			}
			if importPath == queryRootImport {
				offenders = append(offenders, path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir(.) error = %v, want nil", err)
	}
	sort.Strings(offenders)
	var unexpected []string
	for _, offender := range offenders {
		if !allowed[offender] {
			unexpected = append(unexpected, offender)
		}
	}
	if len(unexpected) > 0 {
		t.Fatalf("non-test files importing %q outside the Group C exceptions = %v, want none", queryRootImport, unexpected)
	}
}
