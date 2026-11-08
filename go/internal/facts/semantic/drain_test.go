// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semantic

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestQueryRootDrained fails while any non-test file in this package still
// imports the handler-heavy query root (row 9 of
// REMOVAL_CONSUMER_DRAIN_CHECKLIST.md). The family rows record
// `NoReducerEvidence: "read consumption in go/internal/query"` strings only;
// the payload package must not import the root, so the AST-exact census must
// stay at zero here even though the loose re-prove grep still names this
// package.
func TestQueryRootDrained(t *testing.T) {
	t.Parallel()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, filepath.Join(".", name), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range file.Imports {
			if strings.Trim(imp.Path.Value, `"`) == "github.com/eshu-hq/eshu/go/internal/query" {
				t.Errorf("%s still imports the query root; fact payloads may only name it in NoReducerEvidence strings", name)
			}
		}
	}
}
