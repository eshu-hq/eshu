// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nuget_test

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

const centralPackagesFixture = `<Project>
  <PropertyGroup>
    <ManagePackageVersionsCentrally>true</ManagePackageVersionsCentrally>
    <SerilogVersion>3.1.1</SerilogVersion>
  </PropertyGroup>
  <ItemGroup>
    <PackageVersion Include="Newtonsoft.Json" Version="13.0.3" />
    <PackageVersion Include="Serilog" Version="$(SerilogVersion)" />
    <PackageVersion Include="xunit" Version="2.7.0" />
    <PackageVersion Include="Unpinned.Package" />
    <GlobalPackageReference Include="StyleCop.Analyzers" Version="1.2.0-beta.556" PrivateAssets="all" />
  </ItemGroup>
</Project>`

func TestParseDirectoryPackagesPropsEmitsCentralVersionRows(t *testing.T) {
	t.Parallel()

	payload := parseProps(t, "Directory.Packages.props", centralPackagesFixture)

	newtonsoft := onlyRow(t, payload, "PackageVersion", "Newtonsoft.Json")
	parsertest.AssertStringFieldValue(t, newtonsoft, "value", "13.0.3")
	parsertest.AssertStringFieldValue(t, newtonsoft, "requested_version", "13.0.3")
	parsertest.AssertStringFieldValue(t, newtonsoft, "package_manager", "nuget")
	parsertest.AssertStringFieldValue(t, newtonsoft, "config_kind", "dependency")
	parsertest.AssertStringFieldValue(t, newtonsoft, "lang", "nuget_project")
	parsertest.AssertStringFieldValue(t, newtonsoft, "version_evidence", "package_reference")
	assertBoolFieldValue(t, newtonsoft, "central_version_declaration", true)
	if hasKey(newtonsoft, "direct_dependency") {
		t.Fatalf("PackageVersion declares a version policy, not a reference: direct_dependency = %v, want absent", newtonsoft["direct_dependency"])
	}
	if hasKey(newtonsoft, "partial_evidence") {
		t.Fatalf("a literal unconditional PackageVersion is not partial: %v", newtonsoft)
	}

	serilog := onlyRow(t, payload, "PackageVersion", "Serilog")
	parsertest.AssertStringFieldValue(t, serilog, "value", "3.1.1")
	parsertest.AssertStringFieldValue(t, serilog, "requested_version", "$(SerilogVersion)")
	parsertest.AssertStringFieldValue(t, serilog, "version_property", "SerilogVersion")
	parsertest.AssertStringFieldValue(t, serilog, "version_evidence", "project_property")

	unpinned := onlyRow(t, payload, "PackageVersion", "Unpinned.Package")
	parsertest.AssertStringFieldValue(t, unpinned, "value", "")
	parsertest.AssertStringFieldValue(t, unpinned, "version_evidence", "missing_version")
	assertBoolFieldValue(t, unpinned, "partial_evidence", true)

	global := onlyRow(t, payload, "GlobalPackageReference", "StyleCop.Analyzers")
	parsertest.AssertStringFieldValue(t, global, "value", "1.2.0-beta.556")
	parsertest.AssertStringFieldValue(t, global, "dependency_scope", "development")
	assertBoolFieldValue(t, global, "direct_dependency", true)

	manage := onlyRow(t, payload, "PropertyGroup", "ManagePackageVersionsCentrally")
	parsertest.AssertStringFieldValue(t, manage, "value", "true")
	parsertest.AssertStringFieldValue(t, manage, "config_kind", "msbuild_property")
	parsertest.AssertStringFieldValue(t, manage, "value_evidence", "literal")
	if hasKey(manage, "package_manager") {
		t.Fatalf("a property row is not a package: %v", manage)
	}

	if got := limitCount(t, payload, "skipped_elements"); got != 0 {
		t.Fatalf("skipped_elements = %d, want 0 for a fully recognised file", got)
	}
}

func TestParseDirectoryBuildPropsEmitsSharedPropertiesAndReferences(t *testing.T) {
	t.Parallel()

	payload := parseProps(t, "Directory.Build.props", `<Project>
  <PropertyGroup>
    <TargetFramework>net8.0</TargetFramework>
    <Version>1.4.0</Version>
    <LangVersion>$(DefaultLang)</LangVersion>
  </PropertyGroup>
  <ItemGroup>
    <PackageReference Include="Microsoft.SourceLink.GitHub" Version="8.0.0" PrivateAssets="All" />
    <PackageReference Include="Central.Managed" />
    <PackageReference Include="Overridden.Package" VersionOverride="2.0.0" />
  </ItemGroup>
</Project>`)

	framework := onlyRow(t, payload, "PropertyGroup", "TargetFramework")
	parsertest.AssertStringFieldValue(t, framework, "value", "net8.0")
	parsertest.AssertStringFieldValue(t, framework, "value_evidence", "literal")

	version := onlyRow(t, payload, "PropertyGroup", "Version")
	parsertest.AssertStringFieldValue(t, version, "value", "1.4.0")

	lang := onlyRow(t, payload, "PropertyGroup", "LangVersion")
	parsertest.AssertStringFieldValue(t, lang, "value", "$(DefaultLang)")
	parsertest.AssertStringFieldValue(t, lang, "value_evidence", "msbuild_expression")
	assertBoolFieldValue(t, lang, "partial_evidence", true)

	source := onlyRow(t, payload, "PackageReference", "Microsoft.SourceLink.GitHub")
	parsertest.AssertStringFieldValue(t, source, "value", "8.0.0")
	parsertest.AssertStringFieldValue(t, source, "dependency_scope", "development")
	assertBoolFieldValue(t, source, "direct_dependency", true)

	managed := onlyRow(t, payload, "PackageReference", "Central.Managed")
	parsertest.AssertStringFieldValue(t, managed, "value", "")
	parsertest.AssertStringFieldValue(t, managed, "version_evidence", "missing_version")
	assertBoolFieldValue(t, managed, "partial_evidence", true)

	overridden := onlyRow(t, payload, "PackageReference", "Overridden.Package")
	parsertest.AssertStringFieldValue(t, overridden, "value", "2.0.0")
	parsertest.AssertStringFieldValue(t, overridden, "version_override", "2.0.0")
	parsertest.AssertStringFieldValue(t, overridden, "version_evidence", "package_reference")
}

func TestParseCentralPropsBindsByCaseInsensitiveFileName(t *testing.T) {
	t.Parallel()

	payload := parseProps(t, "directory.packages.props", centralPackagesFixture)
	onlyRow(t, payload, "PackageVersion", "Newtonsoft.Json")
}

func TestParseCentralPropsFixtureWithoutDependenciesEmitsOnlyProperties(t *testing.T) {
	t.Parallel()

	payload := parseProps(t, "Directory.Build.props", `<Project>
  <PropertyGroup><Company>Example</Company></PropertyGroup>
</Project>`)
	if got := len(rowsInSection(t, payload, "PackageReference")); got != 0 {
		t.Fatalf("PackageReference rows = %d, want 0", got)
	}
	onlyRow(t, payload, "PropertyGroup", "Company")
}

func TestParseCentralPropsSkipsAndCountsConstructsOutsideTheGrammar(t *testing.T) {
	t.Parallel()

	payload := parseProps(t, "Directory.Packages.props", `<Project>
  <Import Project="shared.props" />
  <Choose>
    <When Condition="'$(Legacy)' == 'true'">
      <ItemGroup><PackageVersion Include="Hidden.In.Choose" Version="1.0.0" /></ItemGroup>
    </When>
  </Choose>
  <ItemGroup>
    <PackageVersion Update="Updated.Only" Version="9.9.9" />
    <PackageVersion Include="First;Second" Version="1.0.0" />
    <PackageVersion Include="$(DynamicName)" Version="1.0.0" />
    <PackageVersion Include="Wild*" Version="1.0.0" />
    <PackageVersion Version="1.0.0" />
    <PackageVersion Include="Conflicting" Version="1.0.0"><Version>2.0.0</Version></PackageVersion>
    <PackageVersion Include="Both" Update="Both" Version="3.0.0" />
    <PackageVersion Include="Real.One" Version="4.0.0" />
  </ItemGroup>
</Project>`)

	names := map[string]bool{}
	for _, row := range variableRows(t, payload) {
		if name, ok := row["name"].(string); ok {
			names[name] = true
		}
	}
	for _, forbidden := range []string{"Hidden.In.Choose", "Updated.Only", "First;Second", "First", "Second", "$(DynamicName)", "Wild*", "Conflicting", "Both"} {
		if names[forbidden] {
			t.Fatalf("row %q was emitted; the construct is outside the grammar and must be skipped", forbidden)
		}
	}
	if !names["Real.One"] {
		t.Fatalf("Real.One missing: skipped constructs must not poison the rest of the file")
	}
	if got := limitCount(t, payload, "skipped_elements"); got != 2 {
		t.Fatalf("skipped_elements = %d, want 2 (Import, Choose)", got)
	}
	if got := limitCount(t, payload, "skipped_items"); got != 7 {
		t.Fatalf("skipped_items = %d, want 7", got)
	}
}

func TestParseCentralPropsMarksConditionalDeclarationsPartial(t *testing.T) {
	t.Parallel()

	payload := parseProps(t, "Directory.Packages.props", `<Project>
  <ItemGroup Condition="'$(TargetFramework)' == 'net48'">
    <PackageVersion Include="Group.Conditional" Version="1.0.0" />
  </ItemGroup>
  <ItemGroup>
    <PackageVersion Include="Item.Conditional" Version="2.0.0" Condition="'$(UseIt)' == 'true'" />
  </ItemGroup>
</Project>`)

	group := onlyRow(t, payload, "PackageVersion", "Group.Conditional")
	parsertest.AssertStringFieldValue(t, group, "value", "1.0.0")
	parsertest.AssertStringFieldValue(t, group, "condition_group", "'$(TargetFramework)' == 'net48'")
	assertBoolFieldValue(t, group, "partial_evidence", true)
	assertBoolFieldValue(t, group, "conditional_declaration", true)

	item := onlyRow(t, payload, "PackageVersion", "Item.Conditional")
	parsertest.AssertStringFieldValue(t, item, "condition_item", "'$(UseIt)' == 'true'")
	parsertest.AssertStringFieldValue(t, item, "condition", "'$(UseIt)' == 'true'")
	assertBoolFieldValue(t, item, "partial_evidence", true)
}

func TestParseCentralPropsRejectsMalformedXML(t *testing.T) {
	t.Parallel()

	for name, body := range map[string]string{
		"unterminated tag":     `<Project><ItemGroup><PackageVersion Include="A" Version="1"`,
		"unterminated comment": `<Project><!-- never closed <ItemGroup/></Project>`,
		"unterminated cdata":   `<Project><PropertyGroup><A><![CDATA[never closed</A></PropertyGroup></Project>`,
		"mismatched end tag":   `<Project><ItemGroup></PropertyGroup></Project>`,
		"undefined entity":     `<Project><PropertyGroup><A>&undefined;</A></PropertyGroup></Project>`,
		"empty document":       ``,
		"no root element":      `just text`,
	} {
		if err := parsePropsError(t, "Directory.Packages.props", body); err == nil {
			t.Fatalf("%s: err = nil, want a parse error and no partial payload", name)
		}
	}
}

func TestParseCentralPropsRequiresProjectRoot(t *testing.T) {
	t.Parallel()

	if err := parsePropsError(t, "Directory.Build.props", `<NotProject><ItemGroup/></NotProject>`); err == nil {
		t.Fatal("err = nil, want an error for a root element that is not Project")
	}
}
