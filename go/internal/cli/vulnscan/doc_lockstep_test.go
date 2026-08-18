// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vulnscan

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

// TestPackageStaysCobraFreeAndDeclaresItsProcessContact is the standing guard
// behind the ownership claim doc.go, README.md and AGENTS.md make: this
// package reads no cobra flag, decides nothing from the process environment,
// and never calls os.Exit -- flags, exit codes and the process's own streams
// belong to go/cmd/eshu.
//
// The name says "declares" rather than "avoids" on purpose, because this
// package is NOT process-neutral and a broader name would imply it is. It
// reaches the network on every RunRepo through the injected RepoClient, and
// the local runtime reaches it directly: it listens on a loopback port to
// reserve one and polls /healthz over HTTP. It starts child processes
// (os/exec, with the binary path from procexec.Executable), reads the
// filesystem (os.Stat), has one os.Getenv reference -- handed to
// localsupervisor.ChildOverrides as its getenv, which reads what it needs to
// compose a child's environment, not to decide anything here -- and wires the
// local owner child's stdout and stderr to the process's os.Stderr, which is
// the one place this package touches a process stream. Every one of those is
// the package's job, and each is named below so the next reader knows the
// list is complete rather than guessing which contact was overlooked.
//
// The os and fmt surfaces are asserted as SET EQUALITIES rather than as a list
// of banned names. A deny-list can only enumerate the failures someone already
// imagined; requiring the used set to equal the expected set fails on any
// call nobody thought of -- os.Exit, os.Stdout, os.LookupEnv, fmt.Println --
// which is how the claim actually decays. Widen a set only by widening the
// sentence in the docs at the same time.
//
// The direct import set is pinned for the same reason. The selector sets
// cannot see a new "flag", "syscall" or cobra import, and the doc claim that
// cobra is absent transitively is guarded separately by
// `go list -deps ./internal/cli/vulnscan | rg spf13`, which this test cannot
// run. What the pin guarantees is that a new dependency is a decision someone
// makes on purpose, in the docs as well as the code.
//
// The selector sets key on the identifiers os and fmt, so an aliased import
// (`import osx "os"`) would leave the import set identical while osx.Exit
// escaped both sets. The walk therefore refuses an alias on either package
// outright rather than trying to follow it.
//
// Only non-test sources are walked. The claim describes what the package
// does; test files legitimately reach for os.ReadDir and t.TempDir, and
// folding them in would swell the expected sets until they no longer
// described the boundary.
func TestPackageStaysCobraFreeAndDeclaresItsProcessContact(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}

	// Each name here is one of the contacts the doc comment above spells out.
	// Stderr is the local owner child's stream wiring in localruntime.go;
	// Getenv is the ChildOverrides argument; the rest are the workspace
	// lookups the local runtime makes. No Exit, no Stdout, no Args.
	wantOS := []string{"DirEntry", "ErrNotExist", "Getenv", "Stat", "Stderr"}
	// Errorf and Sprintf build errors and messages; Fprintf appears only in
	// writef, which every renderer routes through, and in the two local
	// runtime notices written to the writer the wrapper passes. No Print,
	// Println or Printf: nothing here writes to the process stdout.
	wantFmt := []string{"Errorf", "Fprintf", "Sprintf"}
	wantImports := []string{
		"context",
		"encoding/json",
		"errors",
		"fmt",
		"github.com/eshu-hq/eshu/go/internal/buildinfo",
		"github.com/eshu-hq/eshu/go/internal/cli/localsupervisor",
		"github.com/eshu-hq/eshu/go/internal/cli/procexec",
		"github.com/eshu-hq/eshu/go/internal/cli/reposelector",
		"github.com/eshu-hq/eshu/go/internal/cli/scan",
		"github.com/eshu-hq/eshu/go/internal/eshulocal",
		"github.com/eshu-hq/eshu/go/internal/exports",
		"github.com/eshu-hq/eshu/go/internal/query",
		"github.com/eshu-hq/eshu/go/internal/vulnerabilityparity",
		"github.com/eshu-hq/eshu/go/internal/vulnerabilityparityproof",
		"io",
		"net",
		"net/http",
		"net/url",
		"os",
		"os/exec",
		"path/filepath",
		"sort",
		"strconv",
		"strings",
		"time",
	}

	fset := token.NewFileSet()
	scanned, selectors := 0, 0
	gotOS, gotFmt, gotImports := map[string]bool{}, map[string]bool{}, map[string]bool{}

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
			path := strings.Trim(imp.Path.Value, `"`)
			gotImports[path] = true
			if (path == "os" || path == "fmt") && imp.Name != nil {
				t.Fatalf("%s imports %q as %s; the os and fmt selector pins below key on the bare package name, so an alias would let a call escape them", name, path, imp.Name.Name)
			}
		}
		// Matched against parsed selector expressions, not raw file text, so
		// a doc comment naming os.Exit does not trip the guard while real
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
			case "os":
				gotOS[sel.Sel.Name] = true
			case "fmt":
				gotFmt[sel.Sel.Name] = true
			}
			return true
		})
	}

	assertSelectorSet(t, "os", gotOS, wantOS,
		"the package's process contact is enumerated in this test's doc comment and in README.md; a new os call means that list, and the docs, need the same edit -- and os.Exit, os.Stdout and flag reading belong to go/cmd/eshu")
	assertSelectorSet(t, "fmt", gotFmt, wantFmt,
		"nothing here writes to the process stdout; renderers take an io.Writer and route through writef")
	assertSelectorSet(t, "import", gotImports, wantImports,
		"README.md lists this package's dependencies and states cobra is not among them; a new import needs that sentence revisited, and cobra in particular belongs in go/cmd/eshu/vuln_scan.go")

	// A scan that read no files, or walked no selectors, is not evidence.
	// Sixteen non-test sources today; the floors sit well under the current
	// counts so ordinary edits do not trip them, but high enough that a walk
	// reaching only the package clause fails.
	if scanned < 10 {
		t.Fatalf("scanned only %d non-test .go files; expected the package's sources to be present", scanned)
	}
	if selectors < 500 {
		t.Fatalf("walked only %d qualified selectors; the AST walk is not reaching the code", selectors)
	}
}

// assertSelectorSet fails when the qualified selectors used against pkg differ
// from want in either direction. An unexpected name means the code outgrew the
// documented boundary; a missing one means the docs describe a call the
// package no longer makes, and a stale expectation is how the next real drift
// gets waved through.
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
