// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scopedtoken

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// queryRootImport is the in-tree query root #9669 drains this package's
// auth tests off. It is built by concatenation so this gate's own source
// does not match the module-prefixed import census.
const queryRootImport = "github.com/eshu-hq/eshu/go/internal/" + "query"

// queryRootDrainExceptions names the test files the arbiter ruling leaves on
// the query root (the removal PR retargets them): the middleware integration
// test and the postgres-restart identity test. Every other test file in this
// package must import go/internal/query/auth (spelled auth.*) instead.
var queryRootDrainExceptions = []string{
	"identity_postgres_restart_test.go",
	"middleware_integration_test.go",
}

// TestQueryRootDrained pins the #9669 auth-test drain: the set of files in
// this package importing the query root must equal exactly the leave-set
// above — the 7 drained auth tests (registry_test.go, chain_order_test.go)
// must not drift back, and no new file may join the root.
func TestQueryRootDrained(t *testing.T) {
	t.Parallel()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var offenders []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, filepath.Join(".", name), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range file.Imports {
			importPath, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if importPath == queryRootImport {
				offenders = append(offenders, name)
			}
		}
	}
	slices.Sort(offenders)
	want := slices.Sorted(slices.Values(queryRootDrainExceptions))
	if !slices.Equal(offenders, want) {
		t.Fatalf("files importing %q = %v, want exactly %v", queryRootImport, offenders, want)
	}
}
