// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEnvelopeReaderParity holds every copy of the envelope value readers to
// the same source. cmd/eshu declares traceMap / traceSlice / traceString /
// traceInt / traceStrings; change, freshness, and component carry mapValue /
// sliceValue / stringValue / intValue / boolValue sets (component adds
// stringsValue). Not every reader lives everywhere: the bool reader's
// cmd/eshu original left with its last caller when the component family was
// extracted (#6059), and only families that render a string list carry the
// strings reader -- forcing a role into a package that never calls it would
// grow dead code the unused linter rejects. The copies table below is
// therefore per role, and each role asserts its own copy count.
//
// The sets are copies with identical bodies, for the same reason the
// transport classifier next door has copies: cmd/eshu is package main, so
// nothing can import its declarations, and each family needs its own set.
// The entitymap family keeps another, differently named set without the bool
// reader; TestEntityMapValueReadersAreTokenIdenticalToTraceHelpers pins that
// one against the originals, so it stays out of this table. Unlike the
// classifier, the copies here are unexported in every package, so a
// behavioral table cannot reach them from one test -- exporting the
// helpers to make that possible would widen the package APIs for a test's
// convenience. Comparing the declarations at the source level pins the same
// property without that cost, and pins every branch rather than only the
// branches a table happens to visit.
//
// This is the gap the comments alone left open. Separate comments told the
// next editor to change every copy, and some named only one other copy
// because the rest arrived on branches that predated them. A comment cannot
// go red. TestTransportErrorCodeParity pins the classifier the same way and
// #6117 is the edit that would have slipped past without it.
//
// A failure here means one copy's declaration no longer matches the others.
// That is either an edit that reached one copy and missed the rest -- fix the
// copies it missed -- or an intended divergence, in which case the comments
// above every set stop being true and need rewriting along with this test.
func TestEnvelopeReaderParity(t *testing.T) {
	t.Parallel()

	cmdDir := "."
	changeDir := filepath.Join("..", "..", "internal", "cli", "change")
	freshnessDir := filepath.Join("..", "..", "internal", "cli", "freshness")
	componentDir := filepath.Join("..", "..", "internal", "cli", "component")

	type copyRef struct {
		name  string
		dir   string
		local string
	}

	// Every copy of every reader belongs here. A copy left out is a copy
	// nothing pins, which is the hole this test exists to close, so both the
	// role count and each role's copy count are asserted below.
	roles := []struct {
		role       string
		wantCopies int
		copies     []copyRef
	}{
		{role: "map", wantCopies: 4, copies: []copyRef{
			{name: "cmd/eshu", dir: cmdDir, local: "traceMap"},
			{name: "internal/cli/change", dir: changeDir, local: "mapValue"},
			{name: "internal/cli/freshness", dir: freshnessDir, local: "mapValue"},
			{name: "internal/cli/component", dir: componentDir, local: "mapValue"},
		}},
		{role: "slice", wantCopies: 4, copies: []copyRef{
			{name: "cmd/eshu", dir: cmdDir, local: "traceSlice"},
			{name: "internal/cli/change", dir: changeDir, local: "sliceValue"},
			{name: "internal/cli/freshness", dir: freshnessDir, local: "sliceValue"},
			{name: "internal/cli/component", dir: componentDir, local: "sliceValue"},
		}},
		{role: "string", wantCopies: 4, copies: []copyRef{
			{name: "cmd/eshu", dir: cmdDir, local: "traceString"},
			{name: "internal/cli/change", dir: changeDir, local: "stringValue"},
			{name: "internal/cli/freshness", dir: freshnessDir, local: "stringValue"},
			{name: "internal/cli/component", dir: componentDir, local: "stringValue"},
		}},
		{role: "int", wantCopies: 4, copies: []copyRef{
			{name: "cmd/eshu", dir: cmdDir, local: "traceInt"},
			{name: "internal/cli/change", dir: changeDir, local: "intValue"},
			{name: "internal/cli/freshness", dir: freshnessDir, local: "intValue"},
			{name: "internal/cli/component", dir: componentDir, local: "intValue"},
		}},
		{role: "bool", wantCopies: 3, copies: []copyRef{
			{name: "internal/cli/change", dir: changeDir, local: "boolValue"},
			{name: "internal/cli/freshness", dir: freshnessDir, local: "boolValue"},
			{name: "internal/cli/component", dir: componentDir, local: "boolValue"},
		}},
		{role: "strings", wantCopies: 2, copies: []copyRef{
			{name: "cmd/eshu", dir: cmdDir, local: "traceStrings"},
			{name: "internal/cli/component", dir: componentDir, local: "stringsValue"},
		}},
	}

	if len(roles) != 6 {
		t.Fatalf("parity covers %d readers, want 6; move this number only alongside the reader you added or deleted, and say which one", len(roles))
	}

	for _, r := range roles {
		t.Run(r.role, func(t *testing.T) {
			t.Parallel()

			if len(r.copies) != r.wantCopies {
				t.Fatalf("the %q reader lists %d copies, want %d; a copy dropped from this slice is a copy nothing pins, so move the count only alongside the copy you added or deleted", r.role, len(r.copies), r.wantCopies)
			}

			decls := make(map[string]string, len(r.copies))
			for _, c := range r.copies {
				found := readerDeclarations(t, c.dir, map[string]bool{c.local: true})
				decl, ok := found[c.local]
				if !ok {
					// A rename or a move out of the package lands here rather
					// than quietly reducing the set this test compares.
					t.Fatalf("%s: no declaration of %s in %s; if it was renamed or moved, update this test and the comments above every copy", c.name, c.local, c.dir)
				}
				decls[c.name] = decl
			}

			want := decls[r.copies[0].name]
			disagreed := make([]string, 0, len(r.copies))
			for _, c := range r.copies[1:] {
				if decls[c.name] != want {
					disagreed = append(disagreed, c.name)
				}
			}
			// Naming the copies that disagreed separates the two failures that
			// land here. A subset means those copies are the ones an edit
			// missed. All of them means the first copy is the one that moved
			// and the others were left behind.
			if len(disagreed) > 0 {
				t.Fatalf("the %q reader differs across copies: %s do not match %s\n--- %s ---\n%s\n--- %s ---\n%s",
					r.role, strings.Join(disagreed, ", "), r.copies[0].name,
					r.copies[0].name, want,
					disagreed[0], decls[disagreed[0]])
			}
		})
	}
}

// readerDeclarations returns the wanted top-level functions in dir keyed by
// name, with the value being the function's signature and body rendered
// without its name or doc comment. Dropping the name is what lets the
// differently-named copies compare equal; dropping doc comments is what lets
// each copy explain itself in its own words. Test files are skipped so a
// helper in a _test.go file cannot shadow the production declaration.
//
// Only the wanted names are collected. Scanning everything would trip the
// duplicate check below on init, which a Go package may declare once per file.
func readerDeclarations(t *testing.T, dir string, wanted map[string]bool) map[string]string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}

	fset := token.NewFileSet()
	out := make(map[string]string)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(dir, name)
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Body == nil || !wanted[fn.Name.Name] {
				continue
			}
			if prior, dup := out[fn.Name.Name]; dup {
				// Two declarations of one name in a package means build tags
				// select between them, and this test would be comparing an
				// arbitrary one of the two.
				t.Fatalf("%s declares %s twice; parity cannot pick between them\n--- first ---\n%s", dir, fn.Name.Name, prior)
			}
			out[fn.Name.Name] = renderFunc(t, fset, fn)
		}
	}
	return out
}

// renderFunc prints a function's signature and body as source text. Parameter
// and result names are kept: a copy that renamed a parameter is still a copy
// that drifted from the comment claiming the sets are identical.
func renderFunc(t *testing.T, fset *token.FileSet, fn *ast.FuncDecl) string {
	t.Helper()

	var sig, body strings.Builder
	if err := printer.Fprint(&sig, fset, fn.Type); err != nil {
		t.Fatalf("print signature of %s: %v", fn.Name.Name, err)
	}
	if err := printer.Fprint(&body, fset, fn.Body); err != nil {
		t.Fatalf("print body of %s: %v", fn.Name.Name, err)
	}
	return sig.String() + " " + body.String()
}
