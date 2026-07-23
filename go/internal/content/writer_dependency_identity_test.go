// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package content

import "testing"

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
			// Fail-safe hardening (P3): a truthy lockfile value no current
			// producer emits (JSON number) must still block section-keying,
			// or it would slip the gate and collapse distinct lockfile
			// versions (react@17 + react@18) into one uid.
			name:       "lockfile truthy non-bool (int 1)",
			entityType: "Variable",
			metadata: map[string]any{
				"config_kind":     "dependency",
				"package_manager": "npm",
				"section":         "dependencies",
				"lockfile":        1,
			},
		},
		{
			name:       "lockfile truthy non-bool (string 1)",
			entityType: "Variable",
			metadata: map[string]any{
				"config_kind":     "dependency",
				"package_manager": "npm",
				"section":         "dependencies",
				"lockfile":        "1",
			},
		},
		{
			name:       "lockfile truthy non-bool (string yes)",
			entityType: "Variable",
			metadata: map[string]any{
				"config_kind":     "dependency",
				"package_manager": "npm",
				"section":         "dependencies",
				"lockfile":        "yes",
			},
		},
		{
			// A present nil lockfile value is unrecognized, so the fail-safe
			// direction is to treat it as a lockfile and fall back.
			name:       "lockfile present but nil",
			entityType: "Variable",
			metadata: map[string]any{
				"config_kind":     "dependency",
				"package_manager": "npm",
				"section":         "dependencies",
				"lockfile":        nil,
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
			// "pubspec" (not "pub") is not, and never has been, an emitted
			// package_manager value — it guards against a typo/renamed
			// producer ever silently widening the gate.
			name:       "unsupported package_manager string",
			entityType: "Variable",
			metadata: map[string]any{
				"config_kind":     "dependency",
				"package_manager": "pubspec",
				"section":         "dependencies",
			},
		},
		{
			// #5507 deliberately did NOT add "swift" to
			// dependencyIdentityPackageManagers: the only current
			// package_manager=="swift" producer (Package.resolved) always
			// sets lockfile:true and is excluded by condition 4 regardless.
			// This case proves that even a hypothetical future swift row
			// that forgot to set lockfile would still fall back safely,
			// because "swift" is not (yet) in the allowlist at all.
			name:       "hypothetical swift manifest row, not yet in scope",
			entityType: "Variable",
			metadata: map[string]any{
				"config_kind":     "dependency",
				"package_manager": "swift",
				"section":         "Package.swift",
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

// TestCanonicalEntityIDWithMetadataAdmitsRecognizedFalseLockfile guards the
// other direction of the fail-safe lockfile check: an explicitly false
// lockfile value (bool false, or the strings "false"/"") is a recognized
// manifest row and MUST still route to the section-keyed id, not fall back.
// Without this, a producer that sets lockfile:false explicitly (rather than
// omitting it) would lose the reorder-no-churn benefit.
func TestCanonicalEntityIDWithMetadataAdmitsRecognizedFalseLockfile(t *testing.T) {
	t.Parallel()

	const (
		repoID  = "repository:r_12345678"
		path    = "package.json"
		name    = "react"
		section = "dependencies"
		line    = 12
	)

	cases := []struct {
		name  string
		value any
	}{
		{name: "bool false", value: false},
		{name: "string false", value: "false"},
		{name: "string FALSE mixed case", value: "FALSE"},
		{name: "empty string", value: ""},
		{name: "whitespace string", value: "  "},
	}

	want := CanonicalDependencyEntityID(repoID, path, section, name)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			metadata := map[string]any{
				"config_kind":     "dependency",
				"package_manager": "npm",
				"section":         section,
				"lockfile":        tc.value,
			}

			got := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, line, metadata)
			if got != want {
				t.Fatalf("CanonicalEntityIDWithMetadata() with lockfile=%#v = %q, want section-keyed %q", tc.value, got, want)
			}
		})
	}
}
