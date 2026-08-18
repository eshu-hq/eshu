// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pgarray

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

// TestPackageStaysStdlibAndReflectionFree is the standing guard behind the
// claim doc.go, README.md and AGENTS.md all make: this package depends on the
// standard library alone and does no reflection. The package exists because a
// third-party array helper had to be removed for supply-chain reasons, so the
// drift it catches is the likely kind -- someone reaching for pgtype, or
// re-adding a reflective GenericArray fallback because lib/pq had one.
//
// The import set and the fmt/strconv/strings selector sets are asserted as
// SET EQUALITIES rather than deny-lists. A deny-list only enumerates the
// failures someone already imagined; requiring the used set to equal the
// expected set fails on the import or call nobody thought of. Widen a set only
// by widening the sentence in the docs at the same time.
//
// Only non-test sources are walked, and sources are read FROM DISK with
// parser.ParseFile at mode 0 (not ImportsOnly, which would leave the selector
// walk empty and pass silently). A go test -overlay mutation therefore passes
// vacuously; prove this guard bites with an on-disk edit and restore.
func TestPackageStaysStdlibAndReflectionFree(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}

	wantImports := []string{
		"database/sql",
		"database/sql/driver",
		"fmt",
		"strconv",
		"strings",
	}
	// Errorf only: every failure is returned to the caller through
	// database/sql; nothing is printed.
	wantFmt := []string{"Errorf"}
	// AppendFloat renders Float64Array; ParseFloat scans it. Any other strconv
	// call means a new numeric array type appeared without its own table rows.
	wantStrconv := []string{"AppendFloat", "ParseFloat"}
	// IndexByte truncates at a zero byte and ReplaceAll doubles quotes, both in
	// QuoteIdentifier. The encoder itself uses no strings helper.
	wantStrings := []string{"IndexByte", "ReplaceAll"}

	gotImports := map[string]bool{}
	gotFmt, gotStrconv, gotStrings := map[string]bool{}, map[string]bool{}, map[string]bool{}
	fset := token.NewFileSet()
	scanned, selectors := 0, 0

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
			gotImports[strings.Trim(imp.Path.Value, `"`)] = true
		}
		// Matched against parsed selector expressions, not raw text, so a doc
		// comment naming reflect or pgtype does not trip the guard while real
		// code does.
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
			case "fmt":
				gotFmt[sel.Sel.Name] = true
			case "strconv":
				gotStrconv[sel.Sel.Name] = true
			case "strings":
				gotStrings[sel.Sel.Name] = true
			}
			return true
		})
	}

	assertSelectorSet(t, "import", gotImports, wantImports,
		"README.md states this package is standard-library only with no reflection; a new import -- pgtype, "+
			"reflect, or another Eshu package -- means that sentence and the reason the package exists both need revisiting")
	assertSelectorSet(t, "fmt", gotFmt, wantFmt,
		"this package returns errors through database/sql and never writes to a process stream")
	assertSelectorSet(t, "strconv", gotStrconv, wantStrconv,
		"a new numeric conversion means a new array type; add it as a typed array with table rows, not by widening a helper")
	assertSelectorSet(t, "strings", gotStrings, wantStrings,
		"the encoder is byte-oriented on purpose; a strings helper in the quoting path is where a selective-quoting "+
			"table would creep in")

	// A scan that read no files, or walked no selectors, is not evidence.
	// Three non-test sources today: doc.go, pgarray.go, parse.go. The floors
	// sit under the current counts so ordinary edits do not trip them, but
	// high enough that a walk reaching only the package clause fails.
	if scanned < 3 {
		t.Fatalf("scanned only %d non-test .go files; expected the package's sources to be present", scanned)
	}
	if selectors < 20 {
		t.Fatalf("walked only %d qualified selectors; the AST walk is not reaching the code", selectors)
	}
}

// assertSelectorSet fails when the qualified selectors used against pkg differ
// from want in either direction: an unexpected name means the code outgrew the
// documented boundary, a missing one means the docs describe a call the
// package no longer makes.
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
