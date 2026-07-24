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
		// The standard #5507 multi-targeting pattern sets the Condition once
		// per <ItemGroup> (a group-level condition) and never per item, so the
		// fixed parser (nugetProjectDependencyRow) records it as
		// "condition_group" with "condition_item" absent. Modelling that here
		// keeps this helper faithful to the post-#5725 parser output and keeps
		// the group-only path's entity_id byte-identical to the pre-#5725
		// "condition"-override id.
		metadata["condition_group"] = condition
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

// nugetProjectDependencyMetadataItemAndGroup builds the entity_metadata a
// .csproj PackageReference row contributes when BOTH an item-level Condition
// (on the <PackageReference> element) and a group-level Condition (on its
// <ItemGroup>) are present — the #5725 collision case. It mirrors the parser
// (nugetProjectDependencyRow): "condition" is the pre-merged item-override
// (item wins), while "condition_item" and "condition_group" carry the two
// components separately for the identity discriminator to combine.
func nugetProjectDependencyMetadataItemAndGroup(item, group string) map[string]any {
	metadata := map[string]any{
		"config_kind":     "dependency",
		"package_manager": "nuget",
		"section":         "PackageReference",
	}
	if item != "" {
		metadata["condition_item"] = item
	}
	if group != "" {
		metadata["condition_group"] = group
	}
	// "condition" is the parser's firstNonEmpty(item, group) override.
	if merged := item; merged != "" {
		metadata["condition"] = merged
	} else if group != "" {
		metadata["condition"] = group
	}
	return metadata
}

// TestDependencyIdentityDiscriminatorNuGetCombinesItemAndGroup exercises the
// #5725 combined discriminator directly. The empty-guard branch
// (item=="" || group=="") returns firstNonEmpty(item, group), byte-identical
// to the pre-#5725 override so the standard per-ItemGroup multi-targeting
// pattern (item empty, group set per TFM) and item-only rows keep their
// existing identity. Only when BOTH are present do the two components combine
// — the exact case where the old override masked a real group-level (TFM)
// distinction.
func TestDependencyIdentityDiscriminatorNuGetCombinesItemAndGroup(t *testing.T) {
	t.Parallel()

	const (
		itemCond = "'$(Configuration)' == 'Release'"
		groupA   = "'$(TargetFramework)' == 'net472'"
		groupB   = "'$(TargetFramework)' == 'net6.0'"
	)

	for _, tc := range []struct {
		name  string
		item  string
		group string
		want  string
	}{
		{name: "both empty", item: "", group: "", want: ""},
		// Item-only rows keep the item condition as their discriminator.
		{name: "item only", item: itemCond, group: "", want: itemCond},
		// Standard #5507 per-TFM pattern: discriminator equals the group
		// condition, exactly as the pre-#5725 override produced — identity
		// stability guard.
		{name: "group only (standard per-TFM)", item: "", group: groupA, want: groupA},
		// Both present: the two components combine so a repeated item-level
		// condition no longer masks the differing group-level (TFM) one.
		{name: "both present net472", item: itemCond, group: groupA, want: itemCond + "\x1f" + groupA},
		{name: "both present net6.0", item: itemCond, group: groupB, want: itemCond + "\x1f" + groupB},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			metadata := nugetProjectDependencyMetadataItemAndGroup(tc.item, tc.group)
			got := dependencyIdentityDiscriminator("nuget", metadata)
			if got != tc.want {
				t.Fatalf("dependencyIdentityDiscriminator(nuget) = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestCanonicalEntityIDWithMetadataNuGetSameItemConditionDifferentGroupDistinct
// is the #5725 headline regression. A multi-targeted .csproj repeats the SAME
// PackageReference name with the SAME item-level Condition across two
// ItemGroups whose group-level Conditions differ (net472 vs net6.0 at
// different versions). The pre-#5725 override collapsed both to the identical
// item Condition and merged the two genuinely different-target rows into one
// entity_id (a silent identity merge; the losing row was dropped by
// content_writer's deduplicateEntityRows last-wins). They must now mint
// DISTINCT ids.
func TestCanonicalEntityIDWithMetadataNuGetSameItemConditionDifferentGroupDistinct(t *testing.T) {
	t.Parallel()

	const (
		repoID   = "repository:r_12345678"
		path     = "MultiTarget.csproj"
		name     = "Newtonsoft.Json"
		itemCond = "'$(Configuration)' == 'Release'"
	)

	net472 := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, 3,
		nugetProjectDependencyMetadataItemAndGroup(itemCond, "'$(TargetFramework)' == 'net472'"))
	net6 := CanonicalEntityIDWithMetadata(repoID, path, "Variable", name, 6,
		nugetProjectDependencyMetadataItemAndGroup(itemCond, "'$(TargetFramework)' == 'net6.0'"))

	if net472 == net6 {
		t.Fatalf("same item Condition across different-TFM ItemGroups collapsed into one id: %q", net472)
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

// TestDependencyIdentityDiscriminatorNuGetLegacyFactFallsBackToCondition is the
// #5725 backward-compatibility guard (codex PR #5771 P1): a version-skew or
// replayed content_entity fact produced by the pre-#5725 parser carries ONLY
// the pre-merged "condition" field, with no condition_item/condition_group. The
// discriminator must fall back to "condition" rather than collapse to an empty
// string. An empty discriminator would merge ordinary same-name conditional
// PackageReferences from different ItemGroups and churn the id of even a single
// conditional dependency during a rolling deploy or cassette replay.
func TestDependencyIdentityDiscriminatorNuGetLegacyFactFallsBackToCondition(t *testing.T) {
	t.Parallel()

	const (
		legacyA = "'$(TargetFramework)' == 'net472'"
		legacyB = "'$(TargetFramework)' == 'net6.0'"
	)

	// Legacy fact: only "condition" present. Must return it, not "".
	if got := dependencyIdentityDiscriminator("nuget", map[string]any{"condition": legacyA}); got != legacyA {
		t.Fatalf("legacy-fact discriminator = %q, want %q", got, legacyA)
	}
	// Two legacy conditional facts of the same name under different conditions
	// must stay distinct (the exact silent-merge this fix prevents on replay).
	if a, b := dependencyIdentityDiscriminator("nuget", map[string]any{"condition": legacyA}),
		dependencyIdentityDiscriminator("nuget", map[string]any{"condition": legacyB}); a == b {
		t.Fatalf("two differently-conditioned legacy facts collapsed to one discriminator: %q", a)
	}
	// A legacy fact with no condition at all still discriminates on "".
	if got := dependencyIdentityDiscriminator("nuget", map[string]any{}); got != "" {
		t.Fatalf("no-condition legacy fact discriminator = %q, want empty", got)
	}
}
