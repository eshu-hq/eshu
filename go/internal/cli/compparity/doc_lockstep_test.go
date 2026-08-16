// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package compparity

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

// TestPackageStaysProcessNeutral is the standing guard behind the ownership
// claim doc.go, README.md and AGENTS.md all make: this package reads no cobra
// flag, touches no process stream, reads no environment, and never exits.
//
// Before this test the claim rested on a one-time `go list -deps | rg spf13`
// recorded in a pull request body, which no gate re-runs. A later change that
// imported cobra or reached for a flag -- the anti-pattern this package's own
// AGENTS.md warns against -- would have landed with nothing red.
//
// The os and fmt surfaces are asserted as SET EQUALITIES rather than as a list
// of banned names. A deny-list can only enumerate the failures someone already
// imagined, and mutation-proving one samples that same imagination, so it feels
// conclusive while os.LookupEnv, os.Environ, os.WriteFile and os.Create all sail
// through. Requiring the used set to equal the expected set instead fails on any
// call nobody thought of, which is how the claim actually decays. Widen the
// expected sets only by widening the sentence in the docs at the same time.
//
// Only non-test sources are walked. The claim describes what the package does;
// test helpers legitimately reach for os.ReadDir and friends, and folding them
// in would swell the expected set until it no longer described the boundary.
// This file is itself a _test.go, so the skip also keeps the guard from
// tripping over its own os.ReadDir.
func TestPackageStaysProcessNeutral(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}

	wantOS := []string{"IsNotExist", "ReadFile"}
	wantFmt := []string{"Errorf"}

	fset := token.NewFileSet()
	scanned, selectors := 0, 0
	gotOS, gotFmt := map[string]bool{}, map[string]bool{}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		// Mode 0, not parser.ImportsOnly: ImportsOnly stops at the import
		// block, ast.Inspect then walks nothing, and the selector sets come
		// back empty — a silent pass that only the floors below would catch.
		file, err := parser.ParseFile(fset, filepath.Clean(name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		scanned++

		for _, imp := range file.Imports {
			if strings.Contains(imp.Path.Value, "spf13/cobra") {
				t.Errorf("%s imports cobra; flag handling belongs in go/cmd/eshu's competitive_parity_cmd.go", name)
			}
		}

		// Matched against parsed selector expressions, not raw file text, so a
		// doc comment naming os.Getenv does not trip the guard while real code
		// does.
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
			case "fmt":
				gotFmt[sel.Sel.Name] = true
			}
			return true
		})
	}

	assertSelectorSet(t, "os", gotOS, wantOS,
		"process wiring belongs in go/cmd/eshu's competitive_parity_cmd.go")
	assertSelectorSet(t, "fmt", gotFmt, wantFmt,
		"this package returns errors and writes through its caller, never to the process stdout")

	// A scan that read no files, or walked no selectors, is not evidence.
	if scanned < 2 {
		t.Fatalf("scanned only %d non-test .go files; expected the package's sources to be present", scanned)
	}
	if selectors < 60 {
		t.Fatalf("walked only %d qualified selectors; the AST walk is not reaching the code", selectors)
	}
}

// assertSelectorSet fails when the qualified selectors used against pkg differ
// from want in either direction. An unexpected name means the code outgrew the
// documented boundary; a missing one means the docs describe a call the package
// no longer makes, and a stale expectation is how the next real drift gets
// waved through.
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
