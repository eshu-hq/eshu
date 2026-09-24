// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDiscoverQueryCallsitesCoversTestOnlyHelperPackage is the test that
// replaced the exclusion.
//
// internal/query/querytestutil holds test doubles in ordinary .go files,
// because a symbol declared in a _test.go file cannot be imported across a
// package boundary (#6060, epic #6053). The walk used to skip the directory: a
// graph fake whose RunSingle answers by calling Run is indistinguishable here
// from a production read, and the manifest has nowhere honest to put a fake.
//
// Granting that skip meant whitelisting the self-delegation shape, so a real
// graph read wearing it stayed invisible to the gate. The fake instead routes
// both methods through an unexported helper and no longer calls either one, so
// the directory can be walked like any other and a graph read landing here is
// an unregistered callsite rather than a silent pass.
func TestDiscoverQueryCallsitesCoversTestOnlyHelperPackage(t *testing.T) {
	dir := t.TempDir()

	helperDir := filepath.Join(dir, testOnlyHelperPackage)
	if err := os.MkdirAll(helperDir, 0o700); err != nil {
		t.Fatalf("create helper fixture directory: %v", err)
	}
	// The exact shape the old exclusion whitelisted: a Run method reaching Run
	// on its own receiver. It passed the inventory silently before this change.
	fake := `package querytestutil

type PlantReader struct{}

func (p PlantReader) Run() ([]map[string]any, error) {
	return p.Run()
}
`
	if err := os.WriteFile(filepath.Join(helperDir, "graphreader.go"), []byte(fake), 0o600); err != nil {
		t.Fatalf("write helper fixture: %v", err)
	}

	got, err := DiscoverQueryCallsites(dir)
	if err != nil {
		t.Fatalf("DiscoverQueryCallsites() error = %v", err)
	}

	want := testOnlyHelperPackage + "/graphreader.go"
	for _, coverage := range got {
		if filepath.ToSlash(coverage.File) != want {
			continue
		}
		if len(coverage.Calls) != 1 || coverage.Calls[0].Symbol != "(PlantReader).Run" {
			t.Fatalf("DiscoverQueryCallsites() calls for %s = %#v, want the single (PlantReader).Run callsite", want, coverage.Calls)
		}
		return
	}
	t.Fatalf("DiscoverQueryCallsites() = %#v, want %s covered", got, want)
}

// TestDiscoverQueryCallsitesRejectsProductionImportOfTestOnlyHelperPackage
// covers the boundary rule that outlived the exclusion: production code must
// not depend on test doubles, whatever the inventory does.
func TestDiscoverQueryCallsitesRejectsProductionImportOfTestOnlyHelperPackage(t *testing.T) {
	dir := t.TempDir()

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
	for _, want := range []string{"handler.go", "github.com/eshu-hq/eshu/go/internal/query/querytestutil", "querycontract", "_test.go"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("DiscoverQueryCallsites() error = %v, want it to name %q", err, want)
		}
	}
}

// TestDiscoverQueryCallsitesIgnoresTestFileImportOfTestOnlyHelperPackage pins
// the legal exit the error text offers. The import check reads non-test files
// only, so the fake's actual consumers -- the tests -- stay unaffected. Without
// this, a check that rejected every import would make the package unusable for
// the one purpose it exists to serve.
func TestDiscoverQueryCallsitesIgnoresTestFileImportOfTestOnlyHelperPackage(t *testing.T) {
	dir := t.TempDir()

	consumer := `package query

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func TestSomething(t *testing.T) {
	_ = querytestutil.FakeGraphReader{}
}
`
	if err := os.WriteFile(filepath.Join(dir, "handler_test.go"), []byte(consumer), 0o600); err != nil {
		t.Fatalf("write consumer fixture: %v", err)
	}

	if _, err := DiscoverQueryCallsites(dir); err != nil {
		t.Fatalf("DiscoverQueryCallsites() error = %v, want a _test.go import of the helper package accepted", err)
	}
}

// TestDiscoverQueryCallsitesRejectsProductionImportOfNestedTestOnlyHelperLeaf
// covers the nested leaves #6642 split out of the helper package
// (querytestutil/content and querytestutil/graph). They are the same test
// doubles one directory down, so a production import of one is the same
// defect. A check on the trailing path element alone missed them.
func TestDiscoverQueryCallsitesRejectsProductionImportOfNestedTestOnlyHelperLeaf(t *testing.T) {
	for _, leaf := range []string{"content", "graph"} {
		t.Run(leaf, func(t *testing.T) {
			dir := t.TempDir()
			importPath := "github.com/eshu-hq/eshu/go/internal/query/querytestutil/" + leaf
			consumer := "package query\n\nimport \"" + importPath + "\"\n\nvar _ = " + leaf + ".Anything\n"
			if err := os.WriteFile(filepath.Join(dir, "handler.go"), []byte(consumer), 0o600); err != nil {
				t.Fatalf("write consumer fixture: %v", err)
			}

			_, err := DiscoverQueryCallsites(dir)
			if err == nil {
				t.Fatalf("DiscoverQueryCallsites() error = nil, want a rejected production import of %s", importPath)
			}
			if !strings.Contains(err.Error(), importPath) {
				t.Fatalf("DiscoverQueryCallsites() error = %v, want it to name %q", err, importPath)
			}
		})
	}
}

// TestDiscoverQueryCallsitesAcceptsHelperTreeImportingItsOwnLeaf pins the
// other side of the widened match. A double under the helper tree may build on
// another double there; that is test code reaching test code, not production
// reaching a fake. A package whose last element merely starts with the
// helper's name is unrelated and must not match either.
func TestDiscoverQueryCallsitesAcceptsHelperTreeImportingItsOwnLeaf(t *testing.T) {
	dir := t.TempDir()
	leafDir := filepath.Join(dir, testOnlyHelperPackage, "graph")
	if err := os.MkdirAll(leafDir, 0o700); err != nil {
		t.Fatalf("create helper leaf directory: %v", err)
	}
	sibling := "package graph\n\nimport \"github.com/eshu-hq/eshu/go/internal/query/querytestutil/content\"\n\nvar _ = content.Anything\n"
	if err := os.WriteFile(filepath.Join(leafDir, "store.go"), []byte(sibling), 0o600); err != nil {
		t.Fatalf("write helper leaf fixture: %v", err)
	}
	lookalike := "package query\n\nimport \"example.com/querytestutilities\"\n\nvar _ = querytestutilities.Anything\n"
	if err := os.WriteFile(filepath.Join(dir, "handler.go"), []byte(lookalike), 0o600); err != nil {
		t.Fatalf("write lookalike fixture: %v", err)
	}

	if _, err := DiscoverQueryCallsites(dir); err != nil {
		t.Fatalf("DiscoverQueryCallsites() error = %v, want the helper tree and a lookalike package accepted", err)
	}
}
