// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reposelector

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

// TestPackageStaysCobraAndEnvFree is the standing guard behind the ownership
// claim doc.go, README.md and AGENTS.md all make: this package reads no cobra
// flag, reads no environment variable, touches no process stream, writes no
// file, and never exits.
//
// The name is deliberately literal about what is pinned, because this package
// is NOT inert and a broader name would imply it is. It reads the real
// filesystem on every match -- filepath.EvalSymlinks resolving a selector and
// each candidate path -- and it reaches the network on every Resolve, through
// the Getter the wrapper injects. Both are the package's job. What the guard
// covers is process contact: cobra, the environment, the standard streams, and
// os.Exit. A reader who greps for this test must not come away thinking the
// package touches nothing.
//
// Without this test the claim would rest on nothing a gate re-runs. The
// package was lifted out of go/cmd/eshu, where cobra, os.Exit and the process
// environment are all one import away and all normal, so the drift this
// catches is the likely kind: a later change reaching back for the wrapper's
// conveniences because they used to be in scope.
//
// The os and fmt surfaces are asserted as SET EQUALITIES rather than as a list
// of banned names. A deny-list can only enumerate the failures someone already
// imagined, and mutation-proving one samples that same imagination, so it
// feels conclusive while os.LookupEnv, os.Environ, os.WriteFile and os.Create
// all sail through. Requiring the used set to equal the expected set instead
// fails on any call nobody thought of, which is how the claim actually decays.
// The os expectation here is EMPTY: this package does not touch os at all, and
// the first call that does should be a decision someone makes on purpose.
// Widen either set only by widening the sentence in the docs at the same time.
//
// Only non-test sources are walked. The claim describes what the package does;
// test helpers legitimately reach for os.PathSeparator and os.ReadDir, and
// folding them in would swell the expected set until it no longer described
// the boundary. This file is itself a _test.go, so the skip also keeps the
// guard from tripping over its own os.ReadDir.
func TestPackageStaysCobraAndEnvFree(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}

	// Empty: no os call belongs in this package. See the doc comment above.
	wantOS := []string{}
	// Errorf only. Every error here is returned to the caller; nothing is
	// printed, because the wrapper in go/cmd/eshu owns the operator's streams.
	wantFmt := []string{"Errorf"}

	// README.md states this package has no intra-repo dependency and depends on
	// the standard library alone. The os and fmt sets cannot see that: a direct
	// os/exec, net, net/http or syscall import would leave both sets untouched,
	// and so would importing another Eshu package. Pinning the import set
	// catches those, and catches them for the package nobody thought to ban
	// rather than for a list someone remembered -- the same reason the selector
	// checks are equalities. A genuinely new import then needs this list
	// edited, which is the intended trade.
	//
	// "No DIRECT network call" is the honest phrasing, and the distinction is
	// load-bearing: Resolve does reach the network, through the Getter the
	// wrapper injects. What the pin guarantees is that this package cannot
	// acquire its own transport.
	//
	// Be exact about the limit, because the honest version of this comment is
	// the part that keeps working. The pin is over DIRECT imports, and today
	// that is stdlib only, so there is no allowed dependency to reach through:
	// nothing on this list can grow a network call or a file write behind the
	// package's back. That property is a consequence of the list being all
	// stdlib, not of the mechanism. The first intra-repo import added here
	// gives up that guarantee, and whoever adds it owns checking that the
	// functions they call on it stay pure.
	wantImports := []string{
		"fmt",
		"path/filepath",
		"slices",
		"strings",
	}
	gotImports := map[string]bool{}

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
		// back empty -- a silent pass that only the floors below would catch.
		file, err := parser.ParseFile(fset, filepath.Clean(name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		scanned++

		for _, imp := range file.Imports {
			gotImports[strings.Trim(imp.Path.Value, `"`)] = true
		}

		// Matched against parsed selector expressions, not raw file text, so a
		// doc comment naming os.Getenv does not trip the guard while real code
		// does. This matters more here than in a sibling package: doc.go and
		// the comments in reposelector.go both name filepath.EvalSymlinks and
		// filepath.Clean in prose.
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
		"this package makes no process contact; flags, streams and exit codes belong to go/cmd/eshu's repository_selector.go")
	assertSelectorSet(t, "fmt", gotFmt, wantFmt,
		"this package returns errors and is rendered by its caller, never writing to the process stdout")
	assertSelectorSet(t, "import", gotImports, wantImports,
		"README.md states this package depends on the standard library alone, makes no subprocess call, and opens no "+
			"transport of its own (Resolve reaches the network only through the injected Getter); a new import here "+
			"means that sentence needs revisiting, and cobra in particular belongs in go/cmd/eshu's "+
			"repository_selector.go")

	// A scan that read no files, or walked no selectors, is not evidence. Two
	// non-test sources today: doc.go and reposelector.go. The selector floor is
	// set well under the current count so ordinary edits do not trip it, but
	// high enough that a walk reaching only the package clause fails.
	if scanned < 2 {
		t.Fatalf("scanned only %d non-test .go files; expected the package's sources to be present", scanned)
	}
	if selectors < 30 {
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
