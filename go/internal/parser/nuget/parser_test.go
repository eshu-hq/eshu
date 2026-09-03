// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nuget_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
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

	payload := parsertest.MustParsePath(t, repoRoot, path)

	newtonsoft := parsertest.AssertBucketItemByName(t, payload, "variables", "Newtonsoft.Json")
	parsertest.AssertStringFieldValue(t, newtonsoft, "package_manager", "nuget")
	parsertest.AssertStringFieldValue(t, newtonsoft, "config_kind", "dependency")
	parsertest.AssertStringFieldValue(t, newtonsoft, "section", "PackageReference")
	parsertest.AssertStringFieldValue(t, newtonsoft, "value", "[13.0.3]")
	parsertest.AssertStringFieldValue(t, newtonsoft, "requested_version", "[13.0.3]")
	parsertest.AssertStringFieldValue(t, newtonsoft, "dependency_scope", "runtime")

	serilog := parsertest.AssertBucketItemByName(t, payload, "variables", "Serilog")
	parsertest.AssertStringFieldValue(t, serilog, "value", "3.1.1")
	parsertest.AssertStringFieldValue(t, serilog, "requested_version", "$(SerilogVersion)")
	parsertest.AssertStringFieldValue(t, serilog, "version_property", "SerilogVersion")
	parsertest.AssertStringFieldValue(t, serilog, "version_evidence", "project_property")

	compound := parsertest.AssertBucketItemByName(t, payload, "variables", "Compound.Dependency")
	parsertest.AssertStringFieldValue(t, compound, "value", "1.2.3")
	parsertest.AssertStringFieldValue(t, compound, "requested_version", "$(CompoundVersionPrefix).3")
	parsertest.AssertStringFieldValue(t, compound, "version_property", "CompoundVersionPrefix")
	parsertest.AssertStringFieldValue(t, compound, "version_evidence", "project_property")

	xunit := parsertest.AssertBucketItemByName(t, payload, "variables", "xunit")
	parsertest.AssertStringFieldValue(t, xunit, "private_assets", "all")
	parsertest.AssertStringFieldValue(t, xunit, "include_assets", "runtime; build; native; contentfiles; analyzers; buildtransitive")
	assertBoolFieldValue(t, xunit, "development_dependency", true)
	assertBoolFieldValue(t, xunit, "test_dependency", true)
	parsertest.AssertStringFieldValue(t, xunit, "dependency_scope", "test")

	unresolved := parsertest.AssertBucketItemByName(t, payload, "variables", "Unresolved.Dependency")
	parsertest.AssertStringFieldValue(t, unresolved, "value", "$(MissingVersion)")
	parsertest.AssertStringFieldValue(t, unresolved, "requested_version", "$(MissingVersion)")
	parsertest.AssertStringFieldValue(t, unresolved, "unresolved_msbuild_property", "MissingVersion")
	parsertest.AssertStringFieldValue(t, unresolved, "version_evidence", "unresolved_msbuild_property")
	assertBoolFieldValue(t, unresolved, "partial_evidence", true)

	unresolvedCompound := parsertest.AssertBucketItemByName(t, payload, "variables", "Unresolved.Compound")
	parsertest.AssertStringFieldValue(t, unresolvedCompound, "value", "$(MissingPrefix).1")
	parsertest.AssertStringFieldValue(t, unresolvedCompound, "unresolved_msbuild_property", "MissingPrefix")
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

	payload := parsertest.MustParsePath(t, repoRoot, path)

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
		parsertest.AssertStringFieldValue(t, row, "condition_item", itemCondition)
		parsertest.AssertStringFieldValue(t, row, "condition_group", tc.groupCond)
		// The pre-merged override field is preserved byte-for-byte: item wins.
		parsertest.AssertStringFieldValue(t, row, "condition", itemCondition)
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

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v", err)
	}
	if _, err := engine.ParsePath(repoRoot, path, false, parser.Options{}); err == nil {
		t.Fatal("ParsePath() error = nil, want malformed XML error")
	}
}
