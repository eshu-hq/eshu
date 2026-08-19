// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evbundle

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestPackageStaysProcessNeutral is the standing guard behind the invariant
// AGENTS.md states and doc.go repeats: "No cobra flags, no reads of Eshu config
// or a credential from the process environment, no os.Stdin/os.Stdout, no
// time.Now, no os.Exit."
//
// That claim was true and unenforced. It is also a deliberately NARROWED claim,
// which is why the expected sets are what they are: AGENTS.md explicitly permits
// ReadBundleInput and WriteBundle to call os.ReadFile / os.WriteFile on a path
// the caller supplies, citing internal/cli/mcpsetup as precedent, and tells
// future editors not to push those into the wrapper. A blanket "this package
// must not touch os" would therefore be the wrong guard -- it would contradict
// the documented design rather than defend it.
//
// The surfaces are asserted as SET EQUALITIES, not deny-lists. A deny-list only
// enumerates the failures someone already imagined, and mutation-proving one
// samples that same imagination: os.LookupEnv, os.Environ and os.Create would
// all sail through a list that bans os.Getenv. Requiring the used set to equal
// the expected set fails on the call nobody thought of, which is how a claim
// like this actually decays.
//
// The time set is the sharpest clause. `time.Time` appears only as the result
// type of ExportLive's `now func() time.Time` parameter, which is exactly the
// point -- the caller owns the clock and hands the function in, and this
// package decides when to call it, never which clock it is. Pinning the set to
// {Time} means introducing time.Now turns this red without anyone having to
// predict it.
//
// Only non-test sources are walked: the claim describes the package, and test
// helpers legitimately reach for os.ReadDir and t.TempDir. This file is itself
// a _test.go, so the skip also stops the guard tripping over its own os.ReadDir.
func TestPackageStaysProcessNeutral(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}

	// Widen either set only by widening the sentence in AGENTS.md at the same
	// time. ReadFile/WriteFile are the documented caller-path carve-out.
	wantOS := []string{"ReadFile", "WriteFile"}
	wantTime := []string{"Time"}

	fset := token.NewFileSet()
	scanned, selectors := 0, 0
	gotOS, gotTime := map[string]bool{}, map[string]bool{}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		// Mode 0, not parser.ImportsOnly: ImportsOnly stops at the import
		// block, so ast.Inspect would walk nothing, both sets would come back
		// empty, and the guard would pass while checking nothing.
		file, err := parser.ParseFile(fset, filepath.Clean(name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		scanned++

		for _, imp := range file.Imports {
			if strings.Contains(imp.Path.Value, "spf13/cobra") {
				t.Errorf("%s imports cobra; flag handling belongs in go/cmd/eshu's evidence.go", name)
			}
		}

		// Parsed selector expressions, not raw file text. doc.go's sentence
		// "never calls os.Exit" names the identifier without using it, and a
		// text scan reports that as a violation of the very rule it states.
		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			selectors++
			switch pkg.Name {
			case "os":
				gotOS[sel.Sel.Name] = true
			case "time":
				gotTime[sel.Sel.Name] = true
			}
			return true
		})
	}

	assertSelectorSet(t, "os", gotOS, wantOS,
		"AGENTS.md permits only caller-supplied-path file access here; streams, environment and exit belong in go/cmd/eshu's evidence.go")
	assertSelectorSet(t, "time", gotTime, wantTime,
		"the caller owns the clock and passes a now func() time.Time in; AGENTS.md states this package never calls time.Now")

	// A scan that read no files, or walked no selectors, is not evidence.
	if scanned < 2 {
		t.Fatalf("scanned only %d non-test .go files; expected the package's sources to be present", scanned)
	}
	if selectors < 80 {
		t.Fatalf("walked only %d qualified selectors; the AST walk is not reaching the code", selectors)
	}
}

// assertSelectorSet fails when the qualified selectors used against pkg differ
// from want in either direction. An unexpected name means the code outgrew the
// documented boundary; a missing one means the expectation lists a call the
// package no longer makes, and a stale permissive entry is how the set quietly
// stops being an equality at all.
func assertSelectorSet(t *testing.T, pkg string, got map[string]bool, want []string, remedy string) {
	t.Helper()
	names := make([]string, 0, len(got))
	for name := range got {
		names = append(names, name)
	}
	sort.Strings(names)
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("%s selectors used = %v, want exactly %v; %s", pkg, names, want, remedy)
	}
}
