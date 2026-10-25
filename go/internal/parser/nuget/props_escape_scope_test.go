// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nuget_test

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

// MSBuild unescapes %XX (two hex digits) in item specs and metadata before it
// uses them, so Newtonsoft%2EJson is the package Newtonsoft.Json. The scanner
// does not unescape, so an escaped spec or version is unresolved, never
// resolved under the escaped spelling (#8824).

func TestPropsEscapedIncludeIsNotAResolvedName(t *testing.T) {
	t.Parallel()

	payload := parseProps(t, "Directory.Packages.props", `<Project><ItemGroup>
<PackageVersion Include="Newtonsoft%2EJson" Version="13.0.1" />
<PackageVersion Include="Serilog" Version="3.1.1" />
</ItemGroup></Project>`)
	rows := rowsInSection(t, payload, "PackageVersion")
	if len(rows) != 1 {
		t.Fatalf("an escaped Include must not emit a row: %#v", rows)
	}
	if got := limitCount(t, payload, "skipped_items"); got != 1 {
		t.Fatalf("skipped_items = %d, want 1", got)
	}
	// The escaped spec could name any package of the type once unescaped.
	requireAmbiguity(t, onlyRow(t, payload, "PackageVersion", "Serilog"), "unresolved_item_spec")
}

func TestPropsEscapedDuplicateIncludeDemotesTheLiteral(t *testing.T) {
	t.Parallel()

	payload := parseProps(t, "Directory.Packages.props", `<Project><ItemGroup>
<PackageVersion Include="%41" Version="1.0.0" />
<PackageVersion Include="A" Version="2.0.0" />
</ItemGroup></Project>`)
	requireAmbiguity(t, onlyRow(t, payload, "PackageVersion", "A"), "unresolved_item_spec")
}

func TestPropsEscapedUpdateOrRemoveDemotesEarlierRows(t *testing.T) {
	t.Parallel()

	payload := parseProps(t, "Directory.Packages.props", `<Project><ItemGroup>
<PackageVersion Include="A" Version="1.0.0" />
<PackageVersion Include="B" Version="1.0.0" />
<PackageVersion Update="%41" Version="2.0.0" />
<PackageVersion Remove="%42" />
<PackageVersion Include="C" Version="1.0.0" />
</ItemGroup></Project>`)
	requireAmbiguity(t, onlyRow(t, payload, "PackageVersion", "A"), "removed,updated")
	requireAmbiguity(t, onlyRow(t, payload, "PackageVersion", "B"), "removed,updated")
	requireNoAmbiguity(t, onlyRow(t, payload, "PackageVersion", "C"))
}

func TestPropsEscapedVersionIsUnresolved(t *testing.T) {
	t.Parallel()

	payload := parseProps(t, "Directory.Packages.props", `<Project>
<PropertyGroup><V>13.0%2E1</V></PropertyGroup>
<ItemGroup>
<PackageVersion Include="A" Version="13.0%2E1" />
<PackageVersion Include="B" Version="$(V)" />
<PackageVersion Include="C" Version="50%" />
<PackageVersion Include="D" Version="1.0%zz" />
</ItemGroup></Project>`)
	for _, name := range []string{"A", "B"} {
		row := onlyRow(t, payload, "PackageVersion", name)
		parsertest.AssertStringFieldValue(t, row, "version_evidence", "unresolved_msbuild_property")
		assertBoolFieldValue(t, row, "partial_evidence", true)
	}
	parsertest.AssertStringFieldValue(t, onlyRow(t, payload, "PackageVersion", "B"), "value", "$(V)")
	// A percent sign that is not followed by two hex digits is literal text.
	for _, name := range []string{"C", "D"} {
		row := onlyRow(t, payload, "PackageVersion", name)
		parsertest.AssertStringFieldValue(t, row, "version_evidence", "package_reference")
		requireNoAmbiguity(t, row)
	}
	property := onlyRow(t, payload, "PropertyGroup", "V")
	parsertest.AssertStringFieldValue(t, property, "value_evidence", "msbuild_expression")
}

// NuGet.targets turns each GlobalPackageReference into a PackageReference
// whose element sets PrivateAssets="All" and the development IncludeAssets.
// Element metadata is applied after the copied source metadata, so these win
// over anything the GlobalPackageReference declares.
func TestPropsGlobalPackageReferenceIsAlwaysDevelopment(t *testing.T) {
	t.Parallel()

	payload := parseProps(t, "Directory.Packages.props", `<Project><ItemGroup>
<GlobalPackageReference Include="Bare" Version="1.0.0" />
<GlobalPackageReference Include="NonePrivate" Version="1.0.0" PrivateAssets="none" />
<GlobalPackageReference Include="Dynamic" Version="1.0.0" PrivateAssets="$(P)" />
<GlobalPackageReference Include="Conditioned" Version="1.0.0"><PrivateAssets Condition="'$(X)'=='y'">none</PrivateAssets></GlobalPackageReference>
</ItemGroup></Project>`)
	for _, name := range []string{"Bare", "NonePrivate", "Dynamic"} {
		row := onlyRow(t, payload, "GlobalPackageReference", name)
		parsertest.AssertStringFieldValue(t, row, "dependency_scope", "development")
		assertBoolFieldValue(t, row, "development_dependency", true)
		requireNoAmbiguity(t, row)
	}
	conditioned := onlyRow(t, payload, "GlobalPackageReference", "Conditioned")
	parsertest.AssertStringFieldValue(t, conditioned, "dependency_scope", "development")
	requireAmbiguity(t, conditioned, "conditional_metadata")
}

// A PackageReference scope comes from its own PrivateAssets/IncludeAssets.
// When the deciding metadata is an expression, escaped, or conditional, the
// scope is not guessed: dependency_scope is absent and the row is partial.
func TestPropsPackageReferenceScopeIsNotGuessedFromUnresolvedAssets(t *testing.T) {
	t.Parallel()

	payload := parseProps(t, "Directory.Build.props", `<Project><ItemGroup>
<PackageReference Include="Plain" Version="1.0.0" />
<PackageReference Include="Private" Version="1.0.0" PrivateAssets="All" />
<PackageReference Include="Dynamic" Version="1.0.0" PrivateAssets="$(P)" />
<PackageReference Include="Escaped" Version="1.0.0" PrivateAssets="%41ll" />
<PackageReference Include="DynamicInclude" Version="1.0.0" IncludeAssets="$(I)" />
<PackageReference Include="Conditioned" Version="1.0.0"><PrivateAssets Condition="'$(X)'=='y'">All</PrivateAssets></PackageReference>
<PackageReference Include="Decided" Version="1.0.0" IncludeAssets="none"><PrivateAssets Condition="'$(X)'=='y'">All</PrivateAssets></PackageReference>
<PackageReference Include="xunit" Version="2.9.0" PrivateAssets="$(P)" />
</ItemGroup></Project>`)
	parsertest.AssertStringFieldValue(t, onlyRow(t, payload, "PackageReference", "Plain"), "dependency_scope", "runtime")
	parsertest.AssertStringFieldValue(t, onlyRow(t, payload, "PackageReference", "Private"), "dependency_scope", "development")
	for _, tc := range []struct{ name, reasons string }{
		{"Dynamic", "unresolved_asset_metadata"},
		{"Escaped", "unresolved_asset_metadata"},
		{"DynamicInclude", "unresolved_asset_metadata"},
		{"Conditioned", "conditional_metadata,unresolved_asset_metadata"},
	} {
		row := onlyRow(t, payload, "PackageReference", tc.name)
		for _, key := range []string{"dependency_scope", "development_dependency", "test_dependency"} {
			if hasKey(row, key) {
				t.Fatalf("%s: %s must be absent when the asset metadata is unresolved: %#v", tc.name, key, row)
			}
		}
		requireAmbiguity(t, row, tc.reasons)
	}
	decided := onlyRow(t, payload, "PackageReference", "Decided")
	parsertest.AssertStringFieldValue(t, decided, "dependency_scope", "development")
	requireAmbiguity(t, decided, "conditional_metadata")
	// The test-name rule does not read asset metadata, as in the .csproj path.
	test := onlyRow(t, payload, "PackageReference", "xunit")
	parsertest.AssertStringFieldValue(t, test, "dependency_scope", "test")
	requireNoAmbiguity(t, test)
}
