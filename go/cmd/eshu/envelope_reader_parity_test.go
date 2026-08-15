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
// traceInt / traceStrings; change, freshness, component, and trace carry
// mapValue / sliceValue / stringValue / intValue / boolValue sets (component
// and trace add stringsValue). Not every reader lives everywhere: the bool
// reader's cmd/eshu original left with its last caller when the component
// family was extracted (#6139), and only families that render a string list
// carry the strings reader -- forcing a role into a package that never calls
// it would grow dead code the unused linter rejects. Each copy below
// therefore maps only the roles it declares, and declares the rest absent.
//
// The sets are copies with identical bodies, for the same reason the
// transport classifier next door has copies: cmd/eshu is package main, so
// nothing can import its declarations, and each family needs its own set.
// The entitymap family keeps another, differently named set;
// TestEntityMapValueReadersAreTokenIdenticalToTraceHelpers pins that one
// against the originals, so it stays out of this table. Unlike the
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

	// Every copy of the reader set belongs here. A copy left out is a copy
	// nothing pins, which is the hole this test exists to close, so the count
	// is asserted below. Directories are relative to this package.
	//
	// A copy may carry fewer readers than the full role list. internal/cli/trace
	// reads no boolean out of an envelope, and the `unused` linter rejects a
	// reader kept for symmetry alone, so that copy declares no bool reader.
	// Absence has to be declared in absent below rather than simply left out of
	// names: the loop checks that a role listed there really is undeclared, so a
	// bool reader added to that package later fails here until it is registered.
	copies := []struct {
		name   string
		dir    string
		names  map[string]string
		absent map[string]string
	}{
		{
			name: "cmd/eshu",
			dir:  ".",
			names: map[string]string{
				"map":     "traceMap",
				"slice":   "traceSlice",
				"string":  "traceString",
				"int":     "traceInt",
				"strings": "traceStrings",
			},
			absent: map[string]string{
				"bool": "the cmd/eshu original left with its last caller when the component family was extracted (#6139)",
			},
		},
		{
			name: "internal/cli/change",
			dir:  filepath.Join("..", "..", "internal", "cli", "change"),
			names: map[string]string{
				"map":    "mapValue",
				"slice":  "sliceValue",
				"string": "stringValue",
				"int":    "intValue",
				"bool":   "boolValue",
			},
			absent: map[string]string{
				"strings": "the change family renders no string list out of an envelope",
			},
		},
		{
			name: "internal/cli/freshness",
			dir:  filepath.Join("..", "..", "internal", "cli", "freshness"),
			names: map[string]string{
				"map":    "mapValue",
				"slice":  "sliceValue",
				"string": "stringValue",
				"int":    "intValue",
				"bool":   "boolValue",
			},
			absent: map[string]string{
				"strings": "the freshness family renders no string list out of an envelope",
			},
		},
		{
			name: "internal/cli/component",
			dir:  filepath.Join("..", "..", "internal", "cli", "component"),
			names: map[string]string{
				"map":     "mapValue",
				"slice":   "sliceValue",
				"string":  "stringValue",
				"int":     "intValue",
				"bool":    "boolValue",
				"strings": "stringsValue",
			},
		},
		{
			name: "internal/cli/trace",
			dir:  filepath.Join("..", "..", "internal", "cli", "trace"),
			names: map[string]string{
				"map":     "mapValue",
				"slice":   "sliceValue",
				"string":  "stringValue",
				"int":     "intValue",
				"strings": "stringsValue",
			},
			absent: map[string]string{
				"bool": "the trace family reads no boolean out of an envelope, and `unused` rejects a reader carried for symmetry alone",
			},
		},
	}

	// Reader roles, in the order the comments list them. Naming the roles
	// rather than iterating a map keeps the failure output stable and makes a
	// role dropped from a copy a failure instead of a shorter loop.
	roles := []string{"map", "slice", "string", "int", "bool", "strings"}

	if len(copies) != 5 {
		t.Fatalf("parity covers %d copies, want 5; a copy dropped from this slice is a copy nothing pins, so move this number only alongside the copy you added or deleted", len(copies))
	}
	if len(roles) != 6 {
		t.Fatalf("parity covers %d readers, want 6; move this number only alongside the reader you added or deleted, and say which one", len(roles))
	}

	// Every local name any copy uses for a role. A copy that declares a role as
	// absent is checked against all of them, so re-introducing the reader under
	// a sibling package's spelling still fails until it is registered above.
	roleNames := make(map[string]map[string]bool, len(roles))
	for _, role := range roles {
		roleNames[role] = make(map[string]bool, len(copies))
	}
	for _, c := range copies {
		for role, local := range c.names {
			roleNames[role][local] = true
		}
	}

	// role -> copy name -> normalized declaration.
	decls := make(map[string]map[string]string, len(roles))
	for _, role := range roles {
		decls[role] = make(map[string]string, len(copies))
	}

	for _, c := range copies {
		wanted := make(map[string]bool, len(c.names))
		for _, local := range c.names {
			wanted[local] = true
		}
		// Names this copy says it does not declare are collected too, so their
		// absence is verified rather than assumed.
		for role := range c.absent {
			for local := range roleNames[role] {
				wanted[local] = true
			}
		}
		found := readerDeclarations(t, c.dir, wanted)
		for _, role := range roles {
			local, ok := c.names[role]
			if !ok {
				reason, declared := c.absent[role]
				if !declared {
					t.Fatalf("%s: no name mapped for the %q reader and no absence declared; every copy must either map the role or say in absent why it has none", c.name, role)
				}
				for candidate := range roleNames[role] {
					if _, exists := found[candidate]; exists {
						t.Fatalf("%s declares %s, but this test records the %q reader as absent there (%s); add it to names so parity pins it", c.name, candidate, role, reason)
					}
				}
				continue
			}
			decl, ok := found[local]
			if !ok {
				// A rename or a move out of the package lands here rather than
				// quietly reducing the set this test compares.
				t.Fatalf("%s: no declaration of %s in %s; if it was renamed or moved, update this test and the comments above every copy", c.name, local, c.dir)
			}
			decls[role][c.name] = decl
		}
	}

	for _, role := range roles {
		t.Run(role, func(t *testing.T) {
			t.Parallel()

			// The baseline is the first copy that declares the role; a copy
			// with the role absent cannot anchor the comparison.
			baselineName := ""
			var want string
			disagreed := make([]string, 0, len(copies))
			for _, c := range copies {
				if _, absent := c.absent[role]; absent {
					continue
				}
				decl := decls[role][c.name]
				if baselineName == "" {
					baselineName, want = c.name, decl
					continue
				}
				if decl != want {
					disagreed = append(disagreed, c.name)
				}
			}
			if baselineName == "" {
				t.Fatalf("no copy declares the %q reader; a role every copy records as absent should be deleted from roles alongside its last copy", role)
			}
			// Naming the copies that disagreed separates the two failures that
			// land here. A subset means those copies are the ones an edit
			// missed. All of them means the baseline copy is the one that moved
			// and the others were left behind.
			if len(disagreed) > 0 {
				t.Fatalf("the %q reader differs across copies: %s do not match %s\n--- %s ---\n%s\n--- %s ---\n%s",
					role, strings.Join(disagreed, ", "), baselineName,
					baselineName, want,
					disagreed[0], decls[role][disagreed[0]])
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
