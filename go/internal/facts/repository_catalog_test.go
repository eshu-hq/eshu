// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import (
	"reflect"
	"testing"
)

func TestRepositoryCatalogOmittedPayloadKeysIsTheHeavyKeySet(t *testing.T) {
	t.Parallel()

	want := []string{"imports_map", "git_refs", "delta_relative_paths", "delta_deleted_relative_paths"}
	if got := RepositoryCatalogOmittedPayloadKeys(); !reflect.DeepEqual(got, want) {
		t.Fatalf("RepositoryCatalogOmittedPayloadKeys() = %v, want %v", got, want)
	}

	// The returned slice is a copy: a caller cannot widen or narrow the
	// projection for every other caller.
	keys := RepositoryCatalogOmittedPayloadKeys()
	keys[0] = "graph_id"
	if got := RepositoryCatalogOmittedPayloadKeys()[0]; got != "imports_map" {
		t.Fatalf("mutating the returned slice changed the contract: first key = %q", got)
	}
}

func TestProjectRepositoryCatalogPayloadDropsOnlyHeavyKeys(t *testing.T) {
	t.Parallel()

	full := map[string]any{
		"graph_id":                     "repository:r_1",
		"repo_id":                      "repository:r_1",
		"name":                         "service-1",
		"remote_url":                   "https://github.com/acme/service-1",
		"default_branch":               "main",
		"imports_map":                  map[string]any{"pkg.Symbol": []any{"a.py"}},
		"git_refs":                     []any{map[string]any{"name": "main"}},
		"delta_relative_paths":         []any{"a.go"},
		"delta_deleted_relative_paths": []any{"b.go"},
	}

	got := ProjectRepositoryCatalogPayload(full)
	want := map[string]any{
		"graph_id":       "repository:r_1",
		"repo_id":        "repository:r_1",
		"name":           "service-1",
		"remote_url":     "https://github.com/acme/service-1",
		"default_branch": "main",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ProjectRepositoryCatalogPayload() = %v, want %v", got, want)
	}
	if _, ok := full["imports_map"]; !ok {
		t.Fatal("ProjectRepositoryCatalogPayload mutated its input")
	}
	if ProjectRepositoryCatalogPayload(nil) != nil {
		t.Fatal("ProjectRepositoryCatalogPayload(nil) must stay nil")
	}
}
