// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package oidclogin

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
// the query root (the removal PR retargets them): none in this package —
// both auth tests (service_test.go, db_provider_config_test.go) drain now.
// It stays as an exact-set allowlist so a future re-add lands here,
// not silently on the root.
var queryRootDrainExceptions = []string{}

// TestQueryRootDrained pins the #9669 auth-test drain: no file in this
// package may import the query root — the 2 drained auth tests
// (service_test.go, db_provider_config_test.go) must import
// go/internal/query/auth (spelled auth.*) instead and must not drift back.
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
