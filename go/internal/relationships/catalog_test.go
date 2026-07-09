// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"reflect"
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
