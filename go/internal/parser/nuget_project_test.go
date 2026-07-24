// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"testing"
)

func TestParseNuGetProjectPackageReferencesEmitsDependencyRows(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	path := filepath.Join(repoRoot, "src", "Worker", "Worker.csproj")
	writeTestFile(t, path, `<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <TargetFramework>net8.0</TargetFramework>
    <SerilogVersion>3.1.1</SerilogVersion>
    <CompoundVersionPrefix>1.2</CompoundVersionPrefix>
  </PropertyGroup>
  <ItemGroup>
    <PackageReference Include="Newtonsoft.Json" Version="[13.0.3]" />
    <PackageReference Include="Serilog" Version="$(SerilogVersion)" />
    <PackageReference Include="Compound.Dependency" Version="$(CompoundVersionPrefix).3" />
    <PackageReference Include="xunit" Version="2.7.0" PrivateAssets="all" IncludeAssets="runtime; build; native; contentfiles; analyzers; buildtransitive" />
    <PackageReference Include="Unresolved.Dependency" Version="$(MissingVersion)" />
    <PackageReference Include="Unresolved.Compound" Version="$(MissingPrefix).1" />
  </ItemGroup>
</Project>`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v", err)
	}
	payload, err := engine.ParsePath(repoRoot, path, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	newtonsoft := assertBucketItemByName(t, payload, "variables", "Newtonsoft.Json")
	assertStringFieldValue(t, newtonsoft, "package_manager", "nuget")
	assertStringFieldValue(t, newtonsoft, "config_kind", "dependency")
	assertStringFieldValue(t, newtonsoft, "section", "PackageReference")
	assertStringFieldValue(t, newtonsoft, "value", "[13.0.3]")
	assertStringFieldValue(t, newtonsoft, "requested_version", "[13.0.3]")
	assertStringFieldValue(t, newtonsoft, "dependency_scope", "runtime")

	serilog := assertBucketItemByName(t, payload, "variables", "Serilog")
	assertStringFieldValue(t, serilog, "value", "3.1.1")
	assertStringFieldValue(t, serilog, "requested_version", "$(SerilogVersion)")
	assertStringFieldValue(t, serilog, "version_property", "SerilogVersion")
	assertStringFieldValue(t, serilog, "version_evidence", "project_property")

	compound := assertBucketItemByName(t, payload, "variables", "Compound.Dependency")
	assertStringFieldValue(t, compound, "value", "1.2.3")
	assertStringFieldValue(t, compound, "requested_version", "$(CompoundVersionPrefix).3")
	assertStringFieldValue(t, compound, "version_property", "CompoundVersionPrefix")
	assertStringFieldValue(t, compound, "version_evidence", "project_property")

	xunit := assertBucketItemByName(t, payload, "variables", "xunit")
	assertStringFieldValue(t, xunit, "private_assets", "all")
	assertStringFieldValue(t, xunit, "include_assets", "runtime; build; native; contentfiles; analyzers; buildtransitive")
	assertBoolFieldValue(t, xunit, "development_dependency", true)
	assertBoolFieldValue(t, xunit, "test_dependency", true)
	assertStringFieldValue(t, xunit, "dependency_scope", "test")

	unresolved := assertBucketItemByName(t, payload, "variables", "Unresolved.Dependency")
	assertStringFieldValue(t, unresolved, "value", "$(MissingVersion)")
	assertStringFieldValue(t, unresolved, "requested_version", "$(MissingVersion)")
	assertStringFieldValue(t, unresolved, "unresolved_msbuild_property", "MissingVersion")
	assertStringFieldValue(t, unresolved, "version_evidence", "unresolved_msbuild_property")
	assertBoolFieldValue(t, unresolved, "partial_evidence", true)

	unresolvedCompound := assertBucketItemByName(t, payload, "variables", "Unresolved.Compound")
	assertStringFieldValue(t, unresolvedCompound, "value", "$(MissingPrefix).1")
	assertStringFieldValue(t, unresolvedCompound, "unresolved_msbuild_property", "MissingPrefix")
	assertBoolFieldValue(t, unresolvedCompound, "partial_evidence", true)
}

// TestParseNuGetProjectExposesItemAndGroupConditionSeparately proves the
// #5725 fix: when the SAME item-level Condition is repeated on the same
// PackageReference name across two ItemGroups whose group-level Conditions
// differ (a multi-targeted .csproj), the parser must expose the item-level
// and group-level Condition as SEPARATE metadata fields ("condition_item",
// "condition_group") so the identity layer can keep the two genuinely
// different-target rows distinct. The pre-merged "condition" (item-override,
// group-fallback) is kept UNCHANGED for existing display/identity consumers.
func TestParseNuGetProjectExposesItemAndGroupConditionSeparately(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	path := filepath.Join(repoRoot, "MultiTarget.csproj")
	writeTestFile(t, path, `<Project Sdk="Microsoft.NET.Sdk">
  <ItemGroup Condition="'$(TargetFramework)' == 'net472'">
    <PackageReference Include="Newtonsoft.Json" Version="9.0.1" Condition="'$(Configuration)' == 'Release'" />
  </ItemGroup>
  <ItemGroup Condition="'$(TargetFramework)' == 'net6.0'">
    <PackageReference Include="Newtonsoft.Json" Version="13.0.1" Condition="'$(Configuration)' == 'Release'" />
  </ItemGroup>
</Project>`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v", err)
	}
	payload, err := engine.ParsePath(repoRoot, path, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	rows := nugetRowsByName(t, payload, "Newtonsoft.Json")
	if len(rows) != 2 {
		t.Fatalf("Newtonsoft.Json rows = %d, want 2 (one per ItemGroup)", len(rows))
	}

	const itemCondition = "'$(Configuration)' == 'Release'"
	for _, tc := range []struct {
		version   string
		groupCond string
	}{
		{version: "9.0.1", groupCond: "'$(TargetFramework)' == 'net472'"},
		{version: "13.0.1", groupCond: "'$(TargetFramework)' == 'net6.0'"},
	} {
		row := nugetRowByVersion(t, rows, tc.version)
		// The item- and group-level Conditions are now exposed separately.
		assertStringFieldValue(t, row, "condition_item", itemCondition)
		assertStringFieldValue(t, row, "condition_group", tc.groupCond)
		// The pre-merged override field is preserved byte-for-byte: item wins.
		assertStringFieldValue(t, row, "condition", itemCondition)
	}
}

// nugetRowsByName collects every "variables" row whose name matches, unlike
// assertBucketItemByName which returns only the first. A multi-targeted
// .csproj legitimately emits the same PackageReference name more than once.
func nugetRowsByName(t *testing.T, payload map[string]any, name string) []map[string]any {
	t.Helper()

	items, ok := payload["variables"].([]map[string]any)
	if !ok {
		t.Fatalf("variables = %T, want []map[string]any", payload["variables"])
	}
	matches := make([]map[string]any, 0, 2)
	for _, item := range items {
		if itemName, _ := item["name"].(string); itemName == name {
			matches = append(matches, item)
		}
	}
	return matches
}

// nugetRowByVersion returns the single row whose "value" (resolved version)
// matches, failing if zero or more than one match.
func nugetRowByVersion(t *testing.T, rows []map[string]any, version string) map[string]any {
	t.Helper()

	var found map[string]any
	for _, row := range rows {
		if value, _ := row["value"].(string); value == version {
			if found != nil {
				t.Fatalf("more than one row with value %q", version)
			}
			found = row
		}
	}
	if found == nil {
		t.Fatalf("no row with value %q in %#v", version, rows)
	}
	return found
}

func TestParseNuGetProjectRejectsMalformedXML(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	path := filepath.Join(repoRoot, "Broken.csproj")
	writeTestFile(t, path, `<Project><ItemGroup><PackageReference Include="Broken" Version="1.0.0"></ItemGroup></Project>`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v", err)
	}
	if _, err := engine.ParsePath(repoRoot, path, false, Options{}); err == nil {
		t.Fatal("ParsePath() error = nil, want malformed XML error")
	}
}
