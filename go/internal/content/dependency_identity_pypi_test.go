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

// pypiRequirementMetadataWithValue is pypiRequirementMetadata plus a "value"
// field (the row's version specifier/constraint), for the tests proving
// "value" is intentionally excluded from the pypi discriminator.
func pypiRequirementMetadataWithValue(section string, extras []string, marker, value string) map[string]any {
	metadata := pypiRequirementMetadata(section, extras, marker)
	metadata["value"] = value
	return metadata
}

// TestCanonicalEntityIDWithMetadataPyPIConstraintValueIntentionallyOmittedFromDiscriminator
// proves the case codex review flagged for #5507 PR 5731: two requirement
// lines for the same package in the same section with the same extras/marker
// but different version constraints (`requests>=2` and `requests<3`) mint
// the SAME id. This is intentional, not an oversight — see
// dependencyIdentityDiscriminator's "pypi" case doc comment: pip's resolver
// combines every constraint for one canonical package name into a single
// intersected specifier set before resolving one install candidate, so these
// two lines express ONE logical dependency (`requests>=2,<3`), not two. This
// mirrors TestCanonicalEntityIDWithMetadataGomodSameVersionDuplicateCollapses'
// "no distinguishing evidence to preserve" reasoning, except here it holds
// even when the constraint text differs, because pip — unlike Go modules —
// merges rather than picks one.
func TestCanonicalEntityIDWithMetadataPyPIConstraintValueIntentionallyOmittedFromDiscriminator(t *testing.T) {
	t.Parallel()

	const (
		repoID = "repository:r_12345678"
		path   = "requirements.txt"
		name   = "requests"
	)

	lowerBound := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, 1,
		pypiRequirementMetadataWithValue("requirements", nil, "", ">=2"))
	upperBound := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, 2,
		pypiRequirementMetadataWithValue("requirements", nil, "", "<3"))

	if lowerBound != upperBound {
		t.Fatalf("requests>=2 and requests<3 unexpectedly minted different ids (%q vs %q); pip combines these into one constraint set and they must collapse to one dependency node", lowerBound, upperBound)
	}
}

// TestCanonicalEntityIDWithMetadataPyPIExtrasAcceptsJSONDecodedAnySlice proves
// the []any code path in metadataStringSliceValue — the shape
// json.Unmarshal produces for the projector's fact-replay fallback
// (entityMetadataFromPayload), as opposed to the native []string the
// collector snapshot mint site (shape.Materialize) uses — mints the
// identical id to the []string case. Only the []string path was exercised
// before this test (human review finding on #5507 PR 5731); a bug in the
// []any branch would silently break pypi dependency identity on real fact
// replay from Postgres while every other test stayed green.
func TestCanonicalEntityIDWithMetadataPyPIExtrasAcceptsJSONDecodedAnySlice(t *testing.T) {
	t.Parallel()

	const (
		repoID = "repository:r_12345678"
		path   = "requirements.txt"
		name   = "requests"
		line   = 1
	)

	nativeStringSlice := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, line,
		pypiRequirementMetadata("requirements", []string{"socks", "toml"}, ""))

	jsonDecodedMetadata := map[string]any{
		"config_kind":     "dependency",
		"package_manager": "pypi",
		"section":         "requirements",
		"extras":          []any{"socks", "toml"},
	}
	jsonDecodedAnySlice := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, line, jsonDecodedMetadata)

	if jsonDecodedAnySlice != nativeStringSlice {
		t.Fatalf("[]any extras minted %q, want the same id as the []string case %q", jsonDecodedAnySlice, nativeStringSlice)
	}

	// Direct unit coverage of the helper itself, including order-independence
	// and a non-string element being dropped rather than corrupting the sort
	// (a malformed/unexpected JSON payload should degrade gracefully, not
	// panic or silently reorder the valid entries incorrectly).
	got := metadataStringSliceValue(map[string]any{"extras": []any{"toml", "socks"}}, "extras")
	if len(got) != 2 || got[0] != "toml" || got[1] != "socks" {
		t.Fatalf("metadataStringSliceValue([]any{\"toml\",\"socks\"}) = %v, want [\"toml\" \"socks\"] preserving input order (sorting is dependencyExtrasAndMarker's job, not this helper's)", got)
	}

	gotWithNonString := metadataStringSliceValue(map[string]any{"extras": []any{"socks", float64(1), "toml"}}, "extras")
	if len(gotWithNonString) != 2 || gotWithNonString[0] != "socks" || gotWithNonString[1] != "toml" {
		t.Fatalf("metadataStringSliceValue with a non-string element = %v, want the non-string element dropped and the two strings preserved in order", gotWithNonString)
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
