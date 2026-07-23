// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package content

import "testing"

// pypiRequirementMetadata builds the entity_metadata a requirements.txt or
// PEP 621/Hatch array-form pyproject.toml dependency row
// (pythondep/shared.go's rowBuilder) contributes.
func pypiRequirementMetadata(section string, extras []string, marker string) map[string]any {
	metadata := map[string]any{
		"config_kind":     "dependency",
		"package_manager": "pypi",
		"section":         section,
	}
	if len(extras) > 0 {
		metadata["extras"] = extras
	}
	if marker != "" {
		metadata["marker"] = marker
	}
	return metadata
}

// pypiPoetryTableMetadata builds the entity_metadata a `[tool.poetry.
// dependencies]`-style TABLE row (pythondep/pyproject.go's
// poetryDependencyRow) contributes; the row's "name" is a TOML table key and
// is already unique within the section without a discriminator.
func pypiPoetryTableMetadata(section string) map[string]any {
	return map[string]any{
		"config_kind":     "dependency",
		"package_manager": "pypi",
		"section":         section,
	}
}

// TestCanonicalEntityIDWithMetadataPyPIAdmitsInScopeRow proves an ordinary
// requirements.txt dependency row (#5507) routes to the section-keyed scheme.
func TestCanonicalEntityIDWithMetadataPyPIAdmitsInScopeRow(t *testing.T) {
	t.Parallel()

	const (
		repoID = "repository:r_12345678"
		path   = "requirements.txt"
		name   = "requests"
		line   = 3
	)
	metadata := pypiRequirementMetadata("requirements", nil, "")

	got := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, line, metadata)
	if legacy := CanonicalEntityID(repoID, path, "Variable", name, line); got == legacy {
		t.Fatalf("CanonicalEntityIDWithMetadata() = %q unexpectedly matched legacy CanonicalEntityID() for an in-scope pypi row", got)
	}
}

// TestCanonicalEntityIDWithMetadataPyPIReorderNoChurn proves a requirement's
// identity is stable when its line moves.
func TestCanonicalEntityIDWithMetadataPyPIReorderNoChurn(t *testing.T) {
	t.Parallel()

	const (
		repoID = "repository:r_12345678"
		path   = "requirements.txt"
		name   = "requests"
	)
	metadata := pypiRequirementMetadata("requirements", nil, "")

	before := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, 1, metadata)
	after := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, 25, metadata)
	if before != after {
		t.Fatalf("reordering changed the pypi dependency id: line 1 = %q, line 25 = %q", before, after)
	}
}

// TestCanonicalEntityIDWithMetadataPyPIExtrasDistinctness proves the case
// #5507 flagged for pypi: the same package can legitimately repeat within one
// requirements.txt section with different extras declared side by side
// (`requests[socks]` and `requests[toml]`) to cover different install
// contexts simultaneously. (section, name) alone would collapse them; the
// extras discriminator must keep them distinct.
func TestCanonicalEntityIDWithMetadataPyPIExtrasDistinctness(t *testing.T) {
	t.Parallel()

	const (
		repoID = "repository:r_12345678"
		path   = "requirements.txt"
		name   = "requests"
	)

	socks := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, 1,
		pypiRequirementMetadata("requirements", []string{"socks"}, ""))
	toml := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, 2,
		pypiRequirementMetadata("requirements", []string{"toml"}, ""))
	bare := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, 3,
		pypiRequirementMetadata("requirements", nil, ""))

	if socks == toml || socks == bare || toml == bare {
		t.Fatalf("distinct pypi extras collapsed into one id: socks=%q toml=%q bare=%q", socks, toml, bare)
	}
}

// TestCanonicalEntityIDWithMetadataPyPIExtrasOrderStable proves the extras
// discriminator sorts extras before hashing, so listing the same extras in a
// different order does not churn the identity.
func TestCanonicalEntityIDWithMetadataPyPIExtrasOrderStable(t *testing.T) {
	t.Parallel()

	const (
		repoID = "repository:r_12345678"
		path   = "requirements.txt"
		name   = "requests"
		line   = 1
	)

	socksThenToml := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, line,
		pypiRequirementMetadata("requirements", []string{"socks", "toml"}, ""))
	tomlThenSocks := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, line,
		pypiRequirementMetadata("requirements", []string{"toml", "socks"}, ""))

	if socksThenToml != tomlThenSocks {
		t.Fatalf("extras order changed the identity: %q vs %q", socksThenToml, tomlThenSocks)
	}
}

// TestCanonicalEntityIDWithMetadataPyPIMarkerDistinctness proves two
// platform-conditional declarations of the same package with different PEP
// 508 environment markers (`foo; sys_platform=="win32"` vs
// `foo; sys_platform=="linux"`), declared side by side in one section, stay
// distinct.
func TestCanonicalEntityIDWithMetadataPyPIMarkerDistinctness(t *testing.T) {
	t.Parallel()

	const (
		repoID = "repository:r_12345678"
		path   = "requirements.txt"
		name   = "pywin32"
	)

	windows := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, 1,
		pypiRequirementMetadata("requirements", nil, `sys_platform == "win32"`))
	linux := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, 2,
		pypiRequirementMetadata("requirements", nil, `sys_platform == "linux"`))

	if windows == linux {
		t.Fatalf("two distinct pypi environment markers collapsed into one id: %q", windows)
	}
}

// TestCanonicalEntityIDWithMetadataPyPIPoetryTableRowAdmitsInScope proves a
// Poetry TABLE-form dependency ([tool.poetry.dependencies]) also routes to
// the section-keyed scheme; its TOML key already guarantees uniqueness so no
// discriminator is required, matching the npm/composer precedent.
func TestCanonicalEntityIDWithMetadataPyPIPoetryTableRowAdmitsInScope(t *testing.T) {
	t.Parallel()

	const (
		repoID = "repository:r_12345678"
		path   = "pyproject.toml"
		name   = "requests"
		line   = 6
	)
	metadata := pypiPoetryTableMetadata("tool.poetry.dependencies")

	got := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, line, metadata)
	want := CanonicalDependencyEntityID(repoID, path, "tool.poetry.dependencies", name)
	if got != want {
		t.Fatalf("CanonicalEntityIDWithMetadata() = %q, want CanonicalDependencyEntityID() = %q for a discriminator-less poetry table row", got, want)
	}
}
