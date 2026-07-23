// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package content

import "testing"

// nugetProjectDependencyMetadata builds the entity_metadata a .csproj
// PackageReference row (parser/nuget_project_language.go) contributes. Every
// in-scope row's "section" is the fixed literal "PackageReference" regardless
// of which ItemGroup it came from — see nugetProjectDependencyRow.
func nugetProjectDependencyMetadata(condition string) map[string]any {
	metadata := map[string]any{
		"config_kind":     "dependency",
		"package_manager": "nuget",
		"section":         "PackageReference",
	}
	if condition != "" {
		metadata["condition"] = condition
	}
	return metadata
}

// TestCanonicalEntityIDWithMetadataNuGetAdmitsInScopeRow proves an ordinary
// .csproj PackageReference row (#5507) routes to the section-keyed scheme.
func TestCanonicalEntityIDWithMetadataNuGetAdmitsInScopeRow(t *testing.T) {
	t.Parallel()

	const (
		repoID = "repository:r_12345678"
		path   = "App.csproj"
		name   = "Newtonsoft.Json"
		line   = 9
	)
	metadata := nugetProjectDependencyMetadata("")

	got := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, line, metadata)
	if legacy := CanonicalEntityID(repoID, path, "Variable", name, line); got == legacy {
		t.Fatalf("CanonicalEntityIDWithMetadata() = %q unexpectedly matched legacy CanonicalEntityID() for an in-scope nuget row", got)
	}
}

// TestCanonicalEntityIDWithMetadataNuGetReorderNoChurn proves a
// PackageReference's identity is stable when it moves within the file.
func TestCanonicalEntityIDWithMetadataNuGetReorderNoChurn(t *testing.T) {
	t.Parallel()

	const (
		repoID = "repository:r_12345678"
		path   = "App.csproj"
		name   = "Newtonsoft.Json"
	)
	metadata := nugetProjectDependencyMetadata("")

	before := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, 4, metadata)
	after := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, 44, metadata)
	if before != after {
		t.Fatalf("reordering changed the nuget dependency id: line 4 = %q, line 44 = %q", before, after)
	}
}

// TestCanonicalEntityIDWithMetadataNuGetMultiTargetConditionDistinctness
// proves the case #5507 flagged for nuget: a multi-targeted .csproj
// conditionally declares the SAME PackageReference name more than once across
// different ItemGroups gated on `$(TargetFramework)`, each potentially at a
// different version (e.g. Newtonsoft.Json pinned to 9.0.1 for net472 and to
// 13.0.1 for net6.0). Both rows share the fixed section literal
// "PackageReference", so (section, name) alone would collapse them; the
// merged MSBuild "condition" discriminator must keep them distinct.
func TestCanonicalEntityIDWithMetadataNuGetMultiTargetConditionDistinctness(t *testing.T) {
	t.Parallel()

	const (
		repoID = "repository:r_12345678"
		path   = "App.csproj"
		name   = "Newtonsoft.Json"
	)

	net472 := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, 5,
		nugetProjectDependencyMetadata("'$(TargetFramework)' == 'net472'"))
	net6 := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, 9,
		nugetProjectDependencyMetadata("'$(TargetFramework)' == 'net6.0'"))

	if net472 == net6 {
		t.Fatalf("two distinct target-framework conditions of the same package collapsed into one id: %q", net472)
	}
}

// TestCanonicalEntityIDWithMetadataNuGetUnconditionalDuplicateCollapses
// documents the accepted merge direction: two PackageReference rows for the
// same name with no Condition attribute at all (an accidental copy/paste
// duplicate MSBuild would otherwise just evaluate twice) collapse to one id,
// since there is no data distinguishing them as different declarations.
func TestCanonicalEntityIDWithMetadataNuGetUnconditionalDuplicateCollapses(t *testing.T) {
	t.Parallel()

	const (
		repoID = "repository:r_12345678"
		path   = "App.csproj"
		name   = "Newtonsoft.Json"
	)

	first := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, 5, nugetProjectDependencyMetadata(""))
	second := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, 9, nugetProjectDependencyMetadata(""))

	if first != second {
		t.Fatalf("two unconditioned PackageReference rows for the same name unexpectedly diverged: %q vs %q", first, second)
	}
}
