// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package firstrunbench

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

// TestPackageStaysProcessNeutral is the standing guard behind the three
// boundary claims README.md and AGENTS.md make about this package:
//
//   - "Standard library only. No cobra, no environment reads"
//   - "No process wiring here. No cobra flag, no environment read, no exit
//     decision."
//   - "RenderVerdict writes only to the io.Writer it is handed; ReadEnvelope's
//     only filesystem call is os.ReadFile(path)."
//
// All three were true and none was enforced. The last one is a totality claim
// -- "only filesystem call" -- so it is asserted as a SET EQUALITY rather than
// a list of banned names: the os selectors this package uses must be exactly
// {ReadFile}. That is strictly stronger than a deny-list, because it also
// fails on an os call nobody thought to ban, which is the way a totality claim
// actually decays.
func TestPackageStaysProcessNeutral(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}

	// allowedModuleImports is the non-standard-library surface README.md's
	// Dependencies section permits: internal/cli/firstrun, for the envelope
	// contract and QuoteIfEmpty. Everything else outside the standard library
	// is a dependency the docs do not describe. Widen this only by widening
	// that section in the same change.
	allowedModuleImports := map[string]bool{
		"github.com/eshu-hq/eshu/go/internal/cli/firstrun": true,
	}

	// The claim names exactly one filesystem call. Widen this only by
	// widening the sentence in AGENTS.md at the same time.
	wantOSSelectors := []string{"ReadFile"}
	bannedPrints := map[string]bool{"Print": true, "Printf": true, "Println": true}

	fset := token.NewFileSet()
	scanned, selectors := 0, 0
	gotOS := map[string]bool{}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Clean(name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		scanned++

		for _, imp := range file.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if allowedModuleImports[path] {
				continue
			}
			// A standard-library path's first segment carries no dot; every
			// module path's does (github.com/..., gopkg.in/...).
			if strings.Contains(strings.SplitN(path, "/", 2)[0], ".") {
				t.Errorf("%s imports %q; README.md's Dependencies section allows the standard library plus internal/cli/firstrun and nothing else — cobra flags and process wiring belong in go/cmd/eshu's first_run.go",
					name, path)
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
				if bannedPrints[sel.Sel.Name] {
					t.Errorf("%s uses fmt.%s at %s; RenderVerdict writes only to the io.Writer it is handed, never to the process stdout",
						name, sel.Sel.Name, fset.Position(sel.Pos()))
				}
			}
			return true
		})
	}

	got := make([]string, 0, len(gotOS))
	for name := range gotOS {
		got = append(got, name)
	}
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(wantOSSelectors, ",") {
		t.Errorf("os selectors used = %v, want exactly %v; AGENTS.md states os.ReadFile is the only filesystem call, so any other os call means the code and the claim have diverged",
			got, wantOSSelectors)
	}

	// A scan that read no files, or walked no selectors, is not evidence.
	if scanned < 3 {
		t.Fatalf("scanned only %d non-test .go files; expected the package's sources to be present", scanned)
	}
	if selectors < 50 {
		t.Fatalf("walked only %d qualified selectors; the AST walk is not reaching the code", selectors)
	}
}
