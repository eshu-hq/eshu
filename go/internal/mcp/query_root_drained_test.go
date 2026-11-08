// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"testing"
)

// queryRootImport is the in-tree query root #9669 drains go/internal/mcp off.
// Row-2 leaf 2a retargeted the Group A/D/B files (dispatch envelope and
// budget, summaries and views, the in-process read seam) at querycontract
// (+answer for AnswerMetadata, +contentread for ContentSearchMaxOffset);
// row-2 leaf 2b moved the Group C auth ledgers (route classification, the
// scoped-token allowlist decision, the browser-session/all-scope route
// policy, and the route-denial signal) to query/auth, which router/access.go
// and transport_auth.go now import instead. It is built by concatenation so
// this gate's own source does not match the module-prefixed import census
// (the Group E string-literal trap in kind_real_consumer.go).
const queryRootImport = "github.com/eshu-hq/eshu/go/internal/" + "query"

// queryRootDrainExceptions names non-test files under go/internal/mcp still
// allowed to import the query root. Row-2 leaf 2b drained the last two
// Group C files, so this is empty by construction: the P3-hardened equality
// assertion below fails on any offender AND on any stale entry, so the list
// cannot silently rot in either direction.
var queryRootDrainExceptions = []string{}

// TestQueryRootDrained pins the row-2 drain: no non-test file under
// go/internal/mcp imports the query root. It parses imports (not raw text)
// so the Group E string literal in kind_real_consumer.go does not count as
// an import.
//
// P3 hardening: the offender set must EQUAL the allowed set, not merely be
// contained in it. A subset check stays green when an allowed file stops
// importing the root (a stale exception hiding an incomplete drain); the
// equality check reds that drift the same way it reds a new offender.
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
	allowedList := append([]string(nil), queryRootDrainExceptions...)
	sort.Strings(allowedList)
	if !reflect.DeepEqual(offenders, allowedList) {
		t.Fatalf("query-root importers = %v, allowed = %v: the offender set must equal the allowed set (row-2 leaf 2b pins both empty)", offenders, allowedList)
	}
}
