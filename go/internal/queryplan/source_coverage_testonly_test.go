// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDiscoverQueryCallsitesSkipsTestOnlyHelperPackage covers a package whose
// non-test files hold test doubles rather than production code.
//
// internal/query/querytestutil exists because a symbol declared in a _test.go
// file cannot be imported across a package boundary, so the fakes that handler
// families need have to live in ordinary .go files (#6060, epic #6053). A graph
// fake necessarily has Run and RunSingle methods, and RunSingle answers by
// calling Run. To this walk that reads as a production graph query, and the
// manifest has nowhere honest to put it: every entry there asserts a hotness
// disposition about a real backend read, which a fake does not have.
//
// The alternatives are worse. Registering the fake in the manifest records a
// false claim about production behavior. Restructuring the fake so the call is
// not a selector expression would be rewriting production-shaped code to evade
// a gate rather than to fix anything.
func TestDiscoverQueryCallsitesSkipsTestOnlyHelperPackage(t *testing.T) {
	dir := t.TempDir()

	production := `package query

func handle(graph Graph) {
	graph.Run(nil, "RETURN 1", nil)
}
`
	if err := os.WriteFile(filepath.Join(dir, "handler.go"), []byte(production), 0o600); err != nil {
		t.Fatalf("write production fixture: %v", err)
	}

	helperDir := filepath.Join(dir, testOnlyHelperPackage)
	if err := os.MkdirAll(helperDir, 0o700); err != nil {
		t.Fatalf("create helper fixture directory: %v", err)
	}
	fake := `package querytestutil

type FakeGraphReader struct {
	RunFn func() ([]map[string]any, error)
}

func (f FakeGraphReader) Run() ([]map[string]any, error) {
	if f.RunFn == nil {
		return nil, nil
	}
	return f.RunFn()
}

func (f FakeGraphReader) RunSingle() (map[string]any, error) {
	rows, err := f.Run()
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}
`
	if err := os.WriteFile(filepath.Join(helperDir, "graphreader.go"), []byte(fake), 0o600); err != nil {
		t.Fatalf("write helper fixture: %v", err)
	}

	got, err := DiscoverQueryCallsites(dir)
	if err != nil {
		t.Fatalf("DiscoverQueryCallsites() error = %v", err)
	}

	for _, coverage := range got {
		if filepath.ToSlash(coverage.File) == testOnlyHelperPackage+"/graphreader.go" {
			t.Fatalf(
				"DiscoverQueryCallsites() reported %s; test doubles are not production query callsites",
				coverage.File,
			)
		}
	}

	// The over-skip guard, and the reason this test is not just an assertion
	// that something is absent: an exclusion that swallowed the sibling
	// production file would also make the check above pass, while silently
	// dropping real graph reads out of the inventory this gate exists to keep.
	var sawProduction bool
	for _, coverage := range got {
		if filepath.ToSlash(coverage.File) == "handler.go" {
			sawProduction = true
		}
	}
	if !sawProduction {
		t.Fatalf("DiscoverQueryCallsites() = %#v, want handler.go still covered", got)
	}
}

// TestDiscoverQueryCallsitesSkipsOnlyTheNamedHelperPackage keeps the exclusion
// from widening by accident. A directory whose name merely contains the helper
// package's name is ordinary production code and must stay in the inventory.
func TestDiscoverQueryCallsitesSkipsOnlyTheNamedHelperPackage(t *testing.T) {
	dir := t.TempDir()

	source := `package nearby

func handle(graph Graph) {
	graph.RunSingle(nil, "RETURN 1", nil)
}
`
	for _, name := range []string{
		testOnlyHelperPackage + "extra",
		"pre" + testOnlyHelperPackage,
	} {
		nested := filepath.Join(dir, name)
		if err := os.MkdirAll(nested, 0o700); err != nil {
			t.Fatalf("create %s fixture directory: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(nested, "handler.go"), []byte(source), 0o600); err != nil {
			t.Fatalf("write %s fixture: %v", name, err)
		}
	}

	got, err := DiscoverQueryCallsites(dir)
	if err != nil {
		t.Fatalf("DiscoverQueryCallsites() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("DiscoverQueryCallsites() covered %d file(s), want 2: %#v", len(got), got)
	}
}

// writeTestOnlyHelperFake writes a minimal, legitimate graph fake into the
// excluded helper directory so the tests below vary one thing at a time: the
// directory always earns its exclusion except for the defect under test.
func writeTestOnlyHelperFake(t *testing.T, dir string) string {
	t.Helper()

	helperDir := filepath.Join(dir, testOnlyHelperPackage)
	if err := os.MkdirAll(helperDir, 0o700); err != nil {
		t.Fatalf("create helper fixture directory: %v", err)
	}
	fake := `package querytestutil

type FakeGraphReader struct {
	RunFn func() ([]map[string]any, error)
}

func (f FakeGraphReader) Run() ([]map[string]any, error) {
	if f.RunFn == nil {
		return nil, nil
	}
	return f.RunFn()
}

func (f FakeGraphReader) RunSingle() (map[string]any, error) {
	rows, err := f.Run()
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}
`
	if err := os.WriteFile(filepath.Join(helperDir, "graphreader.go"), []byte(fake), 0o600); err != nil {
		t.Fatalf("write helper fixture: %v", err)
	}
	return helperDir
}

// TestDiscoverQueryCallsitesRejectsProductionCallInTestOnlyHelperPackage is the
// reason the exclusion can no longer be fail-open. Skipping the directory used
// to be an unchecked assertion that nothing production-shaped lives there; the
// walk now proves it, and a graph read on anything but the fake's own receiver
// fails the inventory instead of disappearing from it.
func TestDiscoverQueryCallsitesRejectsProductionCallInTestOnlyHelperPackage(t *testing.T) {
	dir := t.TempDir()
	helperDir := writeTestOnlyHelperFake(t, dir)

	production := `package querytestutil

type Graph interface {
	Run() ([]map[string]any, error)
}

func Probe(graph Graph) ([]map[string]any, error) {
	return graph.Run()
}
`
	if err := os.WriteFile(filepath.Join(helperDir, "probe.go"), []byte(production), 0o600); err != nil {
		t.Fatalf("write production fixture: %v", err)
	}

	_, err := DiscoverQueryCallsites(dir)
	if err == nil {
		t.Fatal("DiscoverQueryCallsites() error = nil, want a rejected production graph read in the excluded package")
	}
	for _, want := range []string{"probe.go", "Probe"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("DiscoverQueryCallsites() error = %v, want it to name %q", err, want)
		}
	}
}

// TestDiscoverQueryCallsitesRejectsPackageLevelCallInTestOnlyHelperPackage
// covers the same defect outside a function body: a package-level initializer
// is where a graph read would land if someone wired a real reader here.
func TestDiscoverQueryCallsitesRejectsPackageLevelCallInTestOnlyHelperPackage(t *testing.T) {
	dir := t.TempDir()
	helperDir := writeTestOnlyHelperFake(t, dir)

	production := `package querytestutil

var seeded, _ = defaultGraph.Run()
`
	if err := os.WriteFile(filepath.Join(helperDir, "seed.go"), []byte(production), 0o600); err != nil {
		t.Fatalf("write package-level fixture: %v", err)
	}

	_, err := DiscoverQueryCallsites(dir)
	if err == nil {
		t.Fatal("DiscoverQueryCallsites() error = nil, want a rejected package-level graph read")
	}
	if !strings.Contains(err.Error(), "seed.go") {
		t.Fatalf("DiscoverQueryCallsites() error = %v, want it to name seed.go", err)
	}
}

// TestDiscoverQueryCallsitesRejectsNonStdlibImportInTestOnlyHelperPackage
// enforces the dependency half of the invariant. The fake answers from funcs a
// caller installs; reaching a driver, a handler family, or any other module
// package is the shape a production read would need.
func TestDiscoverQueryCallsitesRejectsNonStdlibImportInTestOnlyHelperPackage(t *testing.T) {
	dir := t.TempDir()
	helperDir := writeTestOnlyHelperFake(t, dir)

	source := `package querytestutil

import "github.com/eshu-hq/eshu/go/internal/query"

type Alias = query.Graph
`
	if err := os.WriteFile(filepath.Join(helperDir, "alias.go"), []byte(source), 0o600); err != nil {
		t.Fatalf("write import fixture: %v", err)
	}

	_, err := DiscoverQueryCallsites(dir)
	if err == nil {
		t.Fatal("DiscoverQueryCallsites() error = nil, want a rejected non-standard-library import")
	}
	if !strings.Contains(err.Error(), "github.com/eshu-hq/eshu/go/internal/query") {
		t.Fatalf("DiscoverQueryCallsites() error = %v, want it to name the offending import", err)
	}
}

// TestDiscoverQueryCallsitesRejectsProductionImportOfTestOnlyHelperPackage is
// the import-direction check: the exclusion is only honest while the excluded
// package is unreachable from production code.
func TestDiscoverQueryCallsitesRejectsProductionImportOfTestOnlyHelperPackage(t *testing.T) {
	dir := t.TempDir()
	writeTestOnlyHelperFake(t, dir)

	consumer := `package query

import "github.com/eshu-hq/eshu/go/internal/query/querytestutil"

var _ = querytestutil.FakeGraphReader{}
`
	if err := os.WriteFile(filepath.Join(dir, "handler.go"), []byte(consumer), 0o600); err != nil {
		t.Fatalf("write consumer fixture: %v", err)
	}

	_, err := DiscoverQueryCallsites(dir)
	if err == nil {
		t.Fatal("DiscoverQueryCallsites() error = nil, want a rejected production import of the helper package")
	}
	if !strings.Contains(err.Error(), "handler.go") {
		t.Fatalf("DiscoverQueryCallsites() error = %v, want it to name handler.go", err)
	}
}

// TestDiscoverQueryCallsitesRejectsEmptyTestOnlyHelperPackage keeps the check
// from passing on nothing. An excluded directory with no non-test Go file is a
// stale exclusion, and a validator that reports success over zero files is
// indistinguishable from one that works.
func TestDiscoverQueryCallsitesRejectsEmptyTestOnlyHelperPackage(t *testing.T) {
	dir := t.TempDir()
	helperDir := filepath.Join(dir, testOnlyHelperPackage)
	if err := os.MkdirAll(helperDir, 0o700); err != nil {
		t.Fatalf("create helper fixture directory: %v", err)
	}
	onlyTest := `package querytestutil

import "testing"

func TestNothing(t *testing.T) {}
`
	if err := os.WriteFile(filepath.Join(helperDir, "nothing_test.go"), []byte(onlyTest), 0o600); err != nil {
		t.Fatalf("write helper test fixture: %v", err)
	}

	_, err := DiscoverQueryCallsites(dir)
	if err == nil {
		t.Fatal("DiscoverQueryCallsites() error = nil, want a rejected empty exclusion")
	}
	if !strings.Contains(err.Error(), testOnlyHelperPackage) {
		t.Fatalf("DiscoverQueryCallsites() error = %v, want it to name %q", err, testOnlyHelperPackage)
	}
}

// TestDiscoverQueryCallsitesCoversNestedDirectoryNamedLikeTheHelper is the
// basename half of the fix. The exclusion is one exact path, so a directory
// that merely reuses the name deeper in the tree stays in the inventory rather
// than dropping its graph reads out of the gate.
func TestDiscoverQueryCallsitesCoversNestedDirectoryNamedLikeTheHelper(t *testing.T) {
	dir := t.TempDir()
	writeTestOnlyHelperFake(t, dir)

	nested := filepath.Join(dir, "deadcode", testOnlyHelperPackage)
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatalf("create nested fixture directory: %v", err)
	}
	source := `package querytestutil

func handle(graph Graph) {
	graph.RunSingle(nil, "RETURN 1", nil)
}
`
	if err := os.WriteFile(filepath.Join(nested, "handler.go"), []byte(source), 0o600); err != nil {
		t.Fatalf("write nested fixture: %v", err)
	}

	got, err := DiscoverQueryCallsites(dir)
	if err != nil {
		t.Fatalf("DiscoverQueryCallsites() error = %v", err)
	}
	want := "deadcode/" + testOnlyHelperPackage + "/handler.go"
	var covered bool
	for _, coverage := range got {
		if filepath.ToSlash(coverage.File) == want {
			covered = true
		}
	}
	if !covered {
		t.Fatalf("DiscoverQueryCallsites() = %#v, want %s covered", got, want)
	}
}
