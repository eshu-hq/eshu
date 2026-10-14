// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"sort"
	"strconv"
	"testing"
)

func TestRepositoryCatalogEntryDerivesRepoIDAndAliases(t *testing.T) {
	t.Parallel()

	entry, ok := RepositoryCatalogEntry(map[string]any{
		"repo_id":   "repo-payments",
		"name":      "payments-service",
		"repo_slug": "acme/payments-service",
	})
	if !ok {
		t.Fatal("RepositoryCatalogEntry returned ok=false for a valid payload")
	}
	if entry.RepoID != "repo-payments" {
		t.Errorf("RepoID = %q, want repo-payments", entry.RepoID)
	}
	// Aliases include RepoID itself first, then name and repo_slug in that
	// order, matching the existing Postgres catalog derivation exactly.
	want := []string{"repo-payments", "payments-service", "acme/payments-service"}
	if !reflect.DeepEqual(entry.Aliases, want) {
		t.Errorf("Aliases = %v, want %v", entry.Aliases, want)
	}
}

func TestRepositoryCatalogEntryFallsBackToGraphIDAndRepoName(t *testing.T) {
	t.Parallel()

	entry, ok := RepositoryCatalogEntry(map[string]any{
		"graph_id":  "repo-fallback",
		"repo_name": "fallback-service",
	})
	if !ok {
		t.Fatal("RepositoryCatalogEntry returned ok=false")
	}
	if entry.RepoID != "repo-fallback" {
		t.Errorf("RepoID = %q, want repo-fallback", entry.RepoID)
	}
	want := []string{"repo-fallback", "fallback-service"}
	if !reflect.DeepEqual(entry.Aliases, want) {
		t.Errorf("Aliases = %v, want %v", entry.Aliases, want)
	}
}

func TestRepositoryCatalogEntryDedupesRepoIDEqualToAlias(t *testing.T) {
	t.Parallel()

	entry, ok := RepositoryCatalogEntry(map[string]any{
		"repo_id": "same-value",
		"name":    "same-value",
	})
	if !ok {
		t.Fatal("RepositoryCatalogEntry returned ok=false")
	}
	if len(entry.Aliases) != 1 || entry.Aliases[0] != "same-value" {
		t.Errorf("Aliases = %v, want [same-value] (deduped against RepoID)", entry.Aliases)
	}
}

func TestRepositoryCatalogEntryRejectsBlankRepoID(t *testing.T) {
	t.Parallel()

	if _, ok := RepositoryCatalogEntry(map[string]any{"unrelated": "value"}); ok {
		t.Fatal("RepositoryCatalogEntry returned ok=true for a payload with no repo_id/graph_id/name")
	}
}

func TestRepositoryCatalogPayloadKeysIsOrderedAndDefensive(t *testing.T) {
	t.Parallel()

	want := []string{"repo_id", "graph_id", "name", "repo_name", "repo_slug", "remote_url"}
	got := RepositoryCatalogPayloadKeys()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RepositoryCatalogPayloadKeys() = %v, want %v", got, want)
	}
	got[0] = "mutated"
	if again := RepositoryCatalogPayloadKeys(); again[0] != "repo_id" {
		t.Fatalf("RepositoryCatalogPayloadKeys() aliases package state: first key = %q", again[0])
	}
}

// TestRepositoryCatalogEntryReadsExactlyTheDeclaredKeys parses catalog.go and
// requires every key literal RepositoryCatalogEntry hands to
// catalogPayloadString to be a declared payload key, and every declared key to
// be read. The backfill loader projects only the declared keys in SQL, so a
// seventh read that is not declared would silently see an empty value there.
func TestRepositoryCatalogEntryReadsExactlyTheDeclaredKeys(t *testing.T) {
	t.Parallel()

	file, err := parser.ParseFile(token.NewFileSet(), "catalog.go", nil, 0)
	if err != nil {
		t.Fatalf("parse catalog.go: %v", err)
	}
	read := map[string]struct{}{}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		ident, ok := call.Fun.(*ast.Ident)
		if !ok || ident.Name != "catalogPayloadString" {
			return true
		}
		for _, arg := range call.Args[1:] {
			lit, ok := arg.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				t.Errorf("catalogPayloadString key at %v is not a string literal", arg.Pos())
				continue
			}
			key, err := strconv.Unquote(lit.Value)
			if err != nil {
				t.Fatalf("unquote %s: %v", lit.Value, err)
			}
			read[key] = struct{}{}
		}
		return true
	})
	got := make([]string, 0, len(read))
	for key := range read {
		got = append(got, key)
	}
	sort.Strings(got)
	want := RepositoryCatalogPayloadKeys()
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("keys read by RepositoryCatalogEntry = %v, declared keys = %v", got, want)
	}
}

// TestRepositoryCatalogEntryIgnoresUndeclaredKeys pins the projection
// contract: the entry derives from the declared keys only, so the in-memory
// derivation and a SQL projection of those keys agree on every payload.
func TestRepositoryCatalogEntryIgnoresUndeclaredKeys(t *testing.T) {
	t.Parallel()

	declared := map[string]any{"repo_id": "repo-a", "name": "a", "remote_url": "https://github.com/acme/a.git"}
	wide := map[string]any{"imports_map": map[string]any{"x": "y"}, "path": "/tmp/a", "alias": "ignored"}
	for key, value := range declared {
		wide[key] = value
	}
	wantEntry, wantOK := RepositoryCatalogEntry(declared)
	gotEntry, gotOK := RepositoryCatalogEntry(wide)
	if gotOK != wantOK || !reflect.DeepEqual(gotEntry, wantEntry) {
		t.Fatalf("entry with undeclared keys = %#v/%t, want %#v/%t", gotEntry, gotOK, wantEntry, wantOK)
	}
	if _, ok := RepositoryCatalogEntry(map[string]any{"alias": "only-undeclared", "path": "/x"}); ok {
		t.Fatal("RepositoryCatalogEntry accepted a payload with no declared key")
	}
}
