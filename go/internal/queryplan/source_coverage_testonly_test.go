// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
	"os"
	"path/filepath"
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
