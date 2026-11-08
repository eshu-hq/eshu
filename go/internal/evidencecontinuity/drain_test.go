// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidencecontinuity

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
// REMOVAL_CONSUMER_DRAIN_CHECKLIST.md). The verifier names query packages in
// `go test` proof-ref strings in tests only; runtime code must not import the
// root, so the re-prove grep must stay silent on non-test files here.
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
				t.Errorf("%s still imports the query root; the verifier may only name query packages in proof-ref strings", name)
			}
		}
	}
}
