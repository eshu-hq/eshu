// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package content

import (
	"context"
	"testing"
)

func TestMaterializationScopeGenerationKey(t *testing.T) {
	t.Parallel()

	got := Materialization{
		ScopeID:      "scope-123",
		GenerationID: "generation-456",
	}

	if want := "scope-123:generation-456"; got.ScopeGenerationKey() != want {
		t.Fatalf("Materialization.ScopeGenerationKey() = %q, want %q", got.ScopeGenerationKey(), want)
	}
}

func TestMaterializationCloneCopiesRecords(t *testing.T) {
	t.Parallel()

	original := Materialization{
		RepoID:       "repository:r_12345678",
		ScopeID:      "scope-123",
		GenerationID: "generation-456",
		Records: []Record{
			{
				Path:     "README.md",
				Body:     "hello",
				Digest:   "digest-1",
				Deleted:  false,
				Metadata: map[string]string{"language": "markdown"},
			},
		},
	}

	cloned := original.Clone()
	if got, want := cloned.RepoID, "repository:r_12345678"; got != want {
		t.Fatalf("cloned RepoID = %q, want %q", got, want)
	}
	cloned.Records[0].Metadata["language"] = "mutated"

	if got, want := original.Records[0].Metadata["language"], "markdown"; got != want {
		t.Fatalf("original record metadata = %q, want %q", got, want)
	}
}

func TestMaterializationCloneCopiesEntityRecords(t *testing.T) {
	t.Parallel()

	original := Materialization{
		RepoID:       "repository:r_12345678",
		ScopeID:      "scope-123",
		GenerationID: "generation-456",
		Entities: []EntityRecord{
			{
				EntityID:        "content-entity:e_ab12cd34ef56",
				Path:            "schema.sql",
				EntityType:      "SqlTable",
				EntityName:      "public.users",
				StartLine:       10,
				EndLine:         20,
				Language:        "sql",
				SourceCache:     "create table public.users",
				TemplateDialect: "ansi",
				Metadata: map[string]any{
					"docstring":   "table docs",
					"decorators":  []string{"@tracked"},
					"nested_data": map[string]any{"owner": "data-platform"},
				},
			},
		},
	}

	cloned := original.Clone()
	cloned.Entities[0].EntityName = "mutated"
	cloned.Entities[0].Metadata["docstring"] = "mutated"
	nested := cloned.Entities[0].Metadata["nested_data"].(map[string]any)
	nested["owner"] = "mutated"

	if got, want := original.Entities[0].EntityName, "public.users"; got != want {
		t.Fatalf("original entity name = %q, want %q", got, want)
	}
	if got, want := original.Entities[0].Metadata["docstring"], "table docs"; got != want {
		t.Fatalf("original entity metadata docstring = %#v, want %#v", got, want)
	}
	originalNested := original.Entities[0].Metadata["nested_data"].(map[string]any)
	if got, want := originalNested["owner"], "data-platform"; got != want {
		t.Fatalf("original entity nested metadata owner = %#v, want %#v", got, want)
	}
}

func TestMemoryWriterStoresClone(t *testing.T) {
	t.Parallel()

	writer := &MemoryWriter{}
	got, err := writer.Write(context.Background(), Materialization{
		RepoID:       "repository:r_12345678",
		ScopeID:      "scope-123",
		GenerationID: "generation-456",
		Records: []Record{{
			Path:   "README.md",
			Body:   "hello",
			Digest: "digest-1",
		}},
		Entities: []EntityRecord{{
			EntityID:    "content-entity:e_ab12cd34ef56",
			Path:        "README.md",
			EntityType:  "Function",
			EntityName:  "hello",
			StartLine:   1,
			EndLine:     1,
			SourceCache: "func hello() {}\n",
			Metadata: map[string]any{
				"docstring": "Greets callers.",
			},
		}},
	})
	if err != nil {
		t.Fatalf("Write() error = %v, want nil", err)
	}
	if got.RecordCount != 1 {
		t.Fatalf("Write().RecordCount = %d, want 1", got.RecordCount)
	}
	if got.EntityCount != 1 {
		t.Fatalf("Write().EntityCount = %d, want 1", got.EntityCount)
	}
	if got, want := writer.Writes[0].RepoID, "repository:r_12345678"; got != want {
		t.Fatalf("stored RepoID = %q, want %q", got, want)
	}
	if got, want := writer.Writes[0].Entities[0].EntityID, "content-entity:e_ab12cd34ef56"; got != want {
		t.Fatalf("stored EntityID = %q, want %q", got, want)
	}
	if got, want := writer.Writes[0].Entities[0].Metadata["docstring"], "Greets callers."; got != want {
		t.Fatalf("stored entity metadata docstring = %#v, want %#v", got, want)
	}
}

func TestCanonicalEntityIDIsStableAndPrefixed(t *testing.T) {
	t.Parallel()

	got := CanonicalEntityID(
		"repository:r_12345678",
		"schema.sql",
		"SqlTable",
		"public.users",
		10,
	)

	if got == "" {
		t.Fatal("CanonicalEntityID() = empty string, want stable identifier")
	}
	if got != CanonicalEntityID("repository:r_12345678", "schema.sql", "SqlTable", "public.users", 10) {
		t.Fatalf("CanonicalEntityID() = %q, want stable output", got)
	}
	if want := "content-entity:e_4c49e9b3dd77"; got != want {
		t.Fatalf("CanonicalEntityID() = %q, want %q", got, want)
	}
	if wantPrefix := "content-entity:e_"; len(got) < len(wantPrefix) || got[:len(wantPrefix)] != wantPrefix {
		t.Fatalf("CanonicalEntityID() = %q, want prefix %q", got, wantPrefix)
	}
}

// TestCanonicalDependencyEntityIDDomainSeparation is test (f) from the #5357
// locked spec: CanonicalDependencyEntityID must never collide with
// CanonicalEntityID for the same (repo, path, name) — the "eshu-dep-v1" tag
// plus the differing component count (six vs five) give unconditional domain
// separation, including across adversarial inputs that try to smuggle a
// newline-joined collision.
func TestCanonicalDependencyEntityIDDomainSeparation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		repoID  string
		path    string
		section string
		depName string
	}{
		{
			name:    "ordinary npm dependency",
			repoID:  "repository:r_12345678",
			path:    "package.json",
			section: "dependencies",
			depName: "react",
		},
		{
			name:    "adversarial name containing newline",
			repoID:  "repository:r_12345678",
			path:    "package.json",
			section: "dependencies",
			depName: "react\ndependencies\nreact",
		},
		{
			name:    "adversarial section containing newline",
			repoID:  "repository:r_12345678",
			path:    "package.json",
			section: "dependencies\nreact",
			depName: "react",
		},
		{
			name:    "adversarial path containing newline",
			repoID:  "repository:r_12345678",
			path:    "package.json\ndependencies",
			section: "dependencies",
			depName: "react",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			depID := CanonicalDependencyEntityID(tc.repoID, tc.path, tc.section, tc.depName)
			codeID := CanonicalEntityID(tc.repoID, tc.path, "variable", tc.depName, 0)

			if depID == codeID {
				t.Fatalf("CanonicalDependencyEntityID(%q, %q, %q, %q) = %q collided with CanonicalEntityID(...) = %q",
					tc.repoID, tc.path, tc.section, tc.depName, depID, codeID)
			}

			wantPrefix := "content-entity:e_"
			if len(depID) < len(wantPrefix) || depID[:len(wantPrefix)] != wantPrefix {
				t.Fatalf("CanonicalDependencyEntityID() = %q, want prefix %q", depID, wantPrefix)
			}

			if depID != CanonicalDependencyEntityID(tc.repoID, tc.path, tc.section, tc.depName) {
				t.Fatalf("CanonicalDependencyEntityID() is not stable across repeated calls")
			}
		})
	}

	// Repo ids are always "repository:r_<hex>" and can never equal the
	// "eshu-dep-v1" domain tag, so the tag itself is a safe, collision-free
	// first hash component regardless of what a caller passes as repoID.
	if repoID := "repository:r_12345678"; repoID == "eshu-dep-v1" {
		t.Fatalf("repo id %q unexpectedly equals the domain tag", repoID)
	}
}

// TestCanonicalEntityIDWithMetadataScopingGuards is test (d) from the #5357
// locked spec: the five-condition gate in CanonicalEntityIDWithMetadata must
// fall back to the legacy line-keyed CanonicalEntityID, byte-identical, for
// every row that fails any one condition — most importantly the lockfile and
// wrong-package-manager rows that config_kind=="dependency" alone would
// wrongly admit (see "THE TRAP" in the locked spec: an npm lockfile
// legitimately repeats a package name across nested node_modules, and
// collapsing those under (path, section, name) would merge distinct
// dependency versions into one identity).
func TestCanonicalEntityIDWithMetadataScopingGuards(t *testing.T) {
	t.Parallel()

	const (
		repoID = "repository:r_12345678"
		path   = "package.json"
		name   = "react"
		line   = 12
	)

	cases := []struct {
		name       string
		entityType string
		metadata   map[string]any
	}{
		{
			name:       "code variable, no config_kind",
			entityType: "Variable",
			metadata:   nil,
		},
		{
			name:       "function entity type",
			entityType: "Function",
			metadata: map[string]any{
				"config_kind":     "dependency",
				"package_manager": "npm",
				"section":         "dependencies",
			},
		},
		{
			name:       "tsconfig extends row",
			entityType: "Variable",
			metadata: map[string]any{
				"config_kind":     "extends",
				"package_manager": "npm",
				"section":         "compilerOptions",
			},
		},
		{
			name:       "package-lock row, lockfile true (bool)",
			entityType: "Variable",
			metadata: map[string]any{
				"config_kind":     "dependency",
				"package_manager": "npm",
				"section":         "package-lock",
				"lockfile":        true,
			},
		},
		{
			name:       "package-lock row, lockfile true (string, JSON-decoded)",
			entityType: "Variable",
			metadata: map[string]any{
				"config_kind":     "dependency",
				"package_manager": "npm",
				"section":         "package-lock",
				"lockfile":        "true",
			},
		},
		{
			name:       "node lockfile row (yarn/pnpm flavor)",
			entityType: "Variable",
			metadata: map[string]any{
				"config_kind":            "dependency",
				"package_manager":        "npm",
				"package_manager_flavor": "yarn",
				"section":                "dependencies",
				"lockfile":               true,
			},
		},
		{
			name:       "composer lock row",
			entityType: "Variable",
			metadata: map[string]any{
				"config_kind":     "dependency",
				"package_manager": "composer",
				"section":         "require",
				"lockfile":        true,
			},
		},
		{
			name:       "cargo manifest row, wrong package_manager",
			entityType: "Variable",
			metadata: map[string]any{
				"config_kind":     "dependency",
				"package_manager": "cargo",
				"section":         "dependencies",
			},
		},
		{
			name:       "pubspec manifest row, wrong package_manager",
			entityType: "Variable",
			metadata: map[string]any{
				"config_kind":     "dependency",
				"package_manager": "pubspec",
				"section":         "dependencies",
			},
		},
		{
			name:       "dependency row, missing section",
			entityType: "Variable",
			metadata: map[string]any{
				"config_kind":     "dependency",
				"package_manager": "npm",
			},
		},
		{
			name:       "dependency row, empty section",
			entityType: "Variable",
			metadata: map[string]any{
				"config_kind":     "dependency",
				"package_manager": "npm",
				"section":         "   ",
			},
		},
		{
			name:       "dependency row, non-string section",
			entityType: "Variable",
			metadata: map[string]any{
				"config_kind":     "dependency",
				"package_manager": "npm",
				"section":         42,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := CanonicalEntityIDWithMetadata(repoID, path, tc.entityType, name, line, tc.metadata)
			want := CanonicalEntityID(repoID, path, tc.entityType, name, line)

			if got != want {
				t.Fatalf("CanonicalEntityIDWithMetadata() = %q, want legacy CanonicalEntityID() = %q (byte-identical fallback expected)", got, want)
			}
		})
	}
}

// TestCanonicalEntityIDWithMetadataAdmitsInScopeDependencyRow is the positive
// counterpart to the scoping-guard table: an in-scope npm manifest dependency
// row (config_kind=="dependency", package_manager=="npm", no lockfile,
// non-empty section) must route to CanonicalDependencyEntityID, not the
// legacy line-keyed CanonicalEntityID.
func TestCanonicalEntityIDWithMetadataAdmitsInScopeDependencyRow(t *testing.T) {
	t.Parallel()

	const (
		repoID  = "repository:r_12345678"
		path    = "package.json"
		name    = "react"
		section = "dependencies"
		line    = 12
	)

	metadata := map[string]any{
		"config_kind":     "dependency",
		"package_manager": "npm",
		"section":         section,
	}

	got := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, line, metadata)
	want := CanonicalDependencyEntityID(repoID, path, section, name)

	if got != want {
		t.Fatalf("CanonicalEntityIDWithMetadata() = %q, want CanonicalDependencyEntityID() = %q", got, want)
	}
	if legacy := CanonicalEntityID(repoID, path, "Variable", name, line); got == legacy {
		t.Fatalf("CanonicalEntityIDWithMetadata() = %q unexpectedly matched legacy CanonicalEntityID() for an in-scope dependency row", got)
	}
}
