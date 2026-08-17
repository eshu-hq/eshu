// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
)

// commandTreeMu guards every test access to the process-wide cobra command
// tree rooted at rootCmd.
//
// The tree is a set of package-level singletons: each command is its own
// `var xxxCmd = &cobra.Command{...}`, and roughly forty init() functions wire
// them together with rootCmd.AddCommand. Cobra exposes no read-only accessor
// for that tree -- Find, Execute, Help, Commands and Flags all lazily memoize
// state in place, so what reads like a lookup is a write:
//
//	Find -> stripFlags -> mergePersistentFlags -> updateParentsPflags
//
// updateParentsPflags allocates c.parentsPflags on first use and then runs
// c.Root().PersistentFlags().AddFlagSet(flag.CommandLine) unconditionally, so a
// lookup starting at ANY node writes through to the root's flag set. Commands()
// sorts c.commands in place and sets c.commandsAreSorted. The whole tree is
// therefore one conflict domain, and a per-command lock would not be sound.
//
// The eshu binary never trips this: main() calls rootCmd.Execute() on a single
// goroutine and the package starts no goroutines of its own. Only parallel
// tests reach the tree concurrently, so the synchronization belongs here rather
// than in production code.
//
// This mutex is what lets the package keep its t.Parallel() coverage. It
// serializes the tree lookup, not the tests: no test waits on another test's
// work, only on another test's microsecond-scale access to this one shared
// object.
var commandTreeMu sync.Mutex

// lockCommandTree grants the calling test exclusive use of the shared cobra
// command tree and releases it when the test finishes.
//
// The lock is held for the remainder of the test, not just for the lookup,
// because the *cobra.Command a lookup returns is still part of the shared tree:
// inspecting cmd.Flags() afterwards memoizes into it exactly like the lookup
// did.
//
// Call this once, at the top of a top-level test function, after t.Parallel().
// It is a plain sync.Mutex and is NOT reentrant: helpers must not call it, and
// a subtest must not call it when its parent already did.
func lockCommandTree(t *testing.T) {
	t.Helper()
	commandTreeMu.Lock()
	t.Cleanup(commandTreeMu.Unlock)
}

// TestSharedCommandTreeSurvivesConcurrentLookups is the regression test for the
// data race that `go test -race ./cmd/eshu -count=4` exposed between parallel
// command-registration tests. It drives concurrent lookups through
// commandTreeMu; delete the Lock/Unlock pair below and the race detector fails
// this test, which is what makes it a guard rather than a smoke test.
func TestSharedCommandTreeSurvivesConcurrentLookups(t *testing.T) {
	t.Parallel()

	paths := [][]string{
		{"docs", "verify"},
		{"component", "extraction-readiness"},
		{"first-run-benchmark"},
		{"trace", "service", "checkout"},
	}

	var wg sync.WaitGroup
	for _, path := range paths {
		for range 4 {
			wg.Add(1)
			go func() {
				defer wg.Done()

				commandTreeMu.Lock()
				defer commandTreeMu.Unlock()

				cmd, _, err := rootCmd.Find(path)
				if err != nil {
					t.Errorf("rootCmd.Find(%v) error = %v, want nil", path, err)
					return
				}
				// Touch the lazily merged flag set the race report implicated,
				// still under the lock.
				_ = cmd.Flags().Lookup("json")
			}()
		}
	}
	wg.Wait()
}

// commandTreeGlobals returns the package-level cobra command variables that make
// up the shared tree, read out of the package's own non-test sources.
//
// This is derived rather than hand-listed on purpose: a checked-in list would
// silently stop covering a command global somebody adds later, and the guard
// below would keep passing while the new global went unsynchronized. Commands
// built as locals inside init() (graphCmd and friends) are not reachable by name
// from a test and are correctly absent.
func commandTreeGlobals(t *testing.T) []string {
	t.Helper()

	sources, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("Glob(*.go) error = %v, want nil", err)
	}

	fset := token.NewFileSet()
	var names []string
	for _, path := range sources {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("ParseFile(%s) error = %v, want nil", path, err)
		}
		for _, decl := range file.Decls {
			decl, ok := decl.(*ast.GenDecl)
			if !ok || decl.Tok != token.VAR {
				continue
			}
			for _, spec := range decl.Specs {
				spec, ok := spec.(*ast.ValueSpec)
				if !ok || !declaresCobraCommand(spec) {
					continue
				}
				for _, name := range spec.Names {
					if name.Name != "_" {
						names = append(names, name.Name)
					}
				}
			}
		}
	}

	if len(names) == 0 {
		t.Fatal("found no package-level cobra command variables, want at least rootCmd")
	}
	sort.Strings(names)
	return names
}

// declaresCobraCommand reports whether a package-level var spec holds a cobra
// command, covering both `var x = &cobra.Command{...}` and the bare
// `var x *cobra.Command` form that is assigned later in init().
func declaresCobraCommand(spec *ast.ValueSpec) bool {
	if isCobraCommandPointer(spec.Type) {
		return true
	}
	for _, value := range spec.Values {
		unary, ok := value.(*ast.UnaryExpr)
		if !ok || unary.Op != token.AND {
			continue
		}
		lit, ok := unary.X.(*ast.CompositeLit)
		if ok && isCobraCommandSelector(lit.Type) {
			return true
		}
	}
	return false
}

func isCobraCommandPointer(expr ast.Expr) bool {
	star, ok := expr.(*ast.StarExpr)
	return ok && isCobraCommandSelector(star.X)
}

func isCobraCommandSelector(expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Command" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "cobra"
}

// commandTreeAccessFile is the file that owns the lock, and so is the one file
// allowed to reach the tree without calling lockCommandTree.
const commandTreeAccessFile = "command_tree_test.go"

// TestSharedCommandTreeAccessIsGuarded fails when a test file reaches the
// shared command tree without taking commandTreeMu, so the fixed race cannot be
// reintroduced by a new test file that reaches for rootCmd.Find out of habit.
//
// The check is file-scoped: it proves a file that touches the tree also locks
// it. It does not prove that every individual test in an already-locking file
// locks. That residual gap is why lockCommandTree is documented as a
// once-per-test call rather than left to the reader.
func TestSharedCommandTreeAccessIsGuarded(t *testing.T) {
	t.Parallel()

	globals := regexp.MustCompile(`\b(` + strings.Join(commandTreeGlobals(t), "|") + `)\.`)

	entries, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatalf("Glob(*_test.go) error = %v, want nil", err)
	}
	if len(entries) == 0 {
		t.Fatal("Glob(*_test.go) matched no files, want the package test sources")
	}

	var offenders []string
	for _, name := range entries {
		if name == commandTreeAccessFile {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("ReadFile(%s) error = %v, want nil", name, err)
		}
		code := stripStringsAndComments(string(src))
		if globals.MatchString(code) && !strings.Contains(code, "lockCommandTree(") {
			offenders = append(offenders, name)
		}
	}

	if len(offenders) > 0 {
		t.Fatalf("test files reach the shared cobra command tree without lockCommandTree(t): %v\n"+
			"Add lockCommandTree(t) at the top of each test in those files; see %s for why.",
			offenders, commandTreeAccessFile)
	}
}

// stripLiterals removes double-quoted strings, raw strings, and comments so the
// guard matches real command-tree references rather than the same identifiers
// quoted inside a t.Fatalf message.
var stripLiterals = regexp.MustCompile("\"(?:[^\"\\\\\n]|\\\\.)*\"|`[^`]*`|//[^\n]*|(?s)/\\*.*?\\*/")

func stripStringsAndComments(src string) string {
	return stripLiterals.ReplaceAllString(src, " ")
}
