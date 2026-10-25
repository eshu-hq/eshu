// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nuget_test

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

// These cases cover MSBuild constructs that change an item after its Include,
// or that this scanner cannot evaluate. Each must leave the row partial with
// a named item_ambiguity reason, never resolved.

func requireAmbiguity(t *testing.T, row map[string]any, want string) {
	t.Helper()
	parsertest.AssertStringFieldValue(t, row, "item_ambiguity", want)
	assertBoolFieldValue(t, row, "partial_evidence", true)
}

func requireNoAmbiguity(t *testing.T, row map[string]any) {
	t.Helper()
	if hasKey(row, "item_ambiguity") || hasKey(row, "partial_evidence") {
		t.Fatalf("row must stay resolved: %#v", row)
	}
}

func TestPropsConditionedMetadataChildIsPartial(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ file, kind string }{
		{"Directory.Packages.props", "PackageVersion"},
		{"Directory.Build.props", "PackageReference"},
	} {
		payload := parseProps(t, tc.file, `<Project><ItemGroup><`+tc.kind+` Include="A"><Version Condition="'$(X)'=='y'">2.0.0</Version></`+tc.kind+`></ItemGroup></Project>`)
		row := onlyRow(t, payload, tc.kind, "A")
		parsertest.AssertStringFieldValue(t, row, "value", "2.0.0")
		requireAmbiguity(t, row, "conditional_metadata")
	}
}

func TestPropsEmptyMetadataChildOverridesAttribute(t *testing.T) {
	t.Parallel()

	payload := parseProps(t, "Directory.Packages.props", `<Project><ItemGroup>
<PackageVersion Include="A" Version="1.0.0"><Version></Version></PackageVersion>
<PackageVersion Include="B" Version="1.0.0"><Version Condition="'$(X)'=='y'">2.0.0</Version></PackageVersion>
</ItemGroup></Project>`)
	if rows := rowsInSection(t, payload, "PackageVersion"); len(rows) != 0 {
		t.Fatalf("an empty or conditioned child that disagrees with the attribute must not resolve: %#v", rows)
	}
	if got := limitCount(t, payload, "skipped_items"); got != 2 {
		t.Fatalf("skipped_items = %d, want 2", got)
	}
}

func TestPropsCaseVariantAttributeIsAConflict(t *testing.T) {
	t.Parallel()

	payload := parseProps(t, "Directory.Packages.props", `<Project><ItemGroup>
<PackageVersion Include="A" Version="1.0.0" version="2.0.0" />
<PackageVersion include="B" Version="1.0.0" />
</ItemGroup></Project>`)
	if rows := rowsInSection(t, payload, "PackageVersion"); len(rows) != 0 {
		t.Fatalf("a case-variant attribute must not resolve: %#v", rows)
	}
}

func TestPropsLaterUpdateOrRemoveDemotesEarlierRow(t *testing.T) {
	t.Parallel()

	payload := parseProps(t, "Directory.Packages.props", `<Project><ItemGroup>
<PackageVersion Include="A" Version="1.0.0" />
<PackageVersion Include="B" Version="1.0.0" />
<PackageVersion Include="C" Version="1.0.0" />
<PackageVersion Include="D" Version="1.0.0" />
<GlobalPackageReference Include="E" Version="1.0.0" />
<PackageVersion Update="a" Version="2.0.0" />
<PackageVersion Remove="B" />
<PackageVersion Update="X;C" Version="2.0.0" />
<PackageReference Update="D" Version="2.0.0" />
<PackageVersion Update="F" Version="2.0.0" />
<PackageVersion Include="F" Version="1.0.0" />
<PackageVersion Remove="G" />
<PackageVersion Include="G" Version="1.0.0" />
</ItemGroup></Project>`)
	requireAmbiguity(t, onlyRow(t, payload, "PackageVersion", "A"), "updated")
	requireAmbiguity(t, onlyRow(t, payload, "PackageVersion", "B"), "removed")
	requireAmbiguity(t, onlyRow(t, payload, "PackageVersion", "C"), "updated")
	// Update binds to the item type and to items already defined.
	requireNoAmbiguity(t, onlyRow(t, payload, "PackageVersion", "D"))
	requireNoAmbiguity(t, onlyRow(t, payload, "GlobalPackageReference", "E"))
	requireNoAmbiguity(t, onlyRow(t, payload, "PackageVersion", "F"))
	requireNoAmbiguity(t, onlyRow(t, payload, "PackageVersion", "G"))
	if got := limitCount(t, payload, "skipped_items"); got != 6 {
		t.Fatalf("skipped_items = %d, want 6 Update/Remove items", got)
	}
}

func TestPropsWildcardUpdateDemotesEveryEarlierRowOfItsType(t *testing.T) {
	t.Parallel()

	payload := parseProps(t, "Directory.Packages.props", `<Project><ItemGroup>
<PackageVersion Include="Alpha" Version="1.0.0" />
<PackageVersion Include="Beta" Version="1.0.0" />
<GlobalPackageReference Include="Gamma" Version="1.0.0" />
<PackageVersion Update="Al*" Version="2.0.0" />
<PackageVersion Include="Delta" Version="1.0.0" />
</ItemGroup></Project>`)
	requireAmbiguity(t, onlyRow(t, payload, "PackageVersion", "Alpha"), "updated")
	requireAmbiguity(t, onlyRow(t, payload, "PackageVersion", "Beta"), "updated")
	requireNoAmbiguity(t, onlyRow(t, payload, "GlobalPackageReference", "Gamma"))
	requireNoAmbiguity(t, onlyRow(t, payload, "PackageVersion", "Delta"))
}

func TestPropsDuplicateIncludesAreAllPartial(t *testing.T) {
	t.Parallel()

	payload := parseProps(t, "Directory.Packages.props", `<Project><ItemGroup>
<PackageVersion Include="A" Version="1.0.0" VersionOverride="3.0.0" />
<PackageVersion Include="B" Version="1.0.0" />
<PackageVersion Include="b" Version="2.0.0" />
<PackageVersion Include="A" Version="1.0.0" />
<PackageReference Include="B" Version="1.0.0" />
</ItemGroup></Project>`)
	requireAmbiguity(t, onlyRow(t, payload, "PackageVersion", "A"), "duplicate_include")
	requireAmbiguity(t, onlyRow(t, payload, "PackageVersion", "B"), "duplicate_include")
	requireAmbiguity(t, onlyRow(t, payload, "PackageVersion", "b"), "duplicate_include")
	requireNoAmbiguity(t, onlyRow(t, payload, "PackageReference", "B"))
}

func TestPropsUnresolvedItemSpecDemotesItsType(t *testing.T) {
	t.Parallel()

	for _, spec := range []string{
		`<PackageVersion Include="X;Y" Version="9.0.0" />`,
		`<PackageVersion Include="Pkg.*" Version="9.0.0" />`,
		`<PackageVersion Include="$(Name)" Version="9.0.0" />`,
		`<packageversion Include="A" Version="9.0.0" />`,
	} {
		payload := parseProps(t, "Directory.Packages.props", `<Project><ItemGroup>
<PackageVersion Include="A" Version="1.0.0" />
<GlobalPackageReference Include="G" Version="1.0.0" />
`+spec+`
</ItemGroup></Project>`)
		requireAmbiguity(t, onlyRow(t, payload, "PackageVersion", "A"), "unresolved_item_spec")
		requireNoAmbiguity(t, onlyRow(t, payload, "GlobalPackageReference", "G"))
	}
}

func TestPropsUnevaluatedElementDemotesEveryItem(t *testing.T) {
	t.Parallel()

	for _, element := range []string{
		`<Import Project="$([MSBuild]::GetPathOfFileAbove(Directory.Packages.props, $(MSBuildThisFileDirectory)..))" />`,
		`<ImportGroup><Import Project="x.props" /></ImportGroup>`,
		`<Choose><When Condition="true"><ItemGroup><PackageVersion Update="A" Version="2.0.0" /></ItemGroup></When></Choose>`,
		`<ItemDefinitionGroup><PackageReference><Version>9.0.0</Version></PackageReference></ItemDefinitionGroup>`,
	} {
		for _, body := range []string{
			`<Project><ItemGroup><PackageVersion Include="A" Version="1.0.0" /><PackageReference Include="R" /></ItemGroup>` + element + `</Project>`,
			`<Project>` + element + `<ItemGroup><PackageVersion Include="A" Version="1.0.0" /><PackageReference Include="R" /></ItemGroup></Project>`,
		} {
			payload := parseProps(t, "Directory.Packages.props", body)
			requireAmbiguity(t, onlyRow(t, payload, "PackageVersion", "A"), "unevaluated_element")
			parsertest.AssertStringFieldValue(t, onlyRow(t, payload, "PackageReference", "R"), "item_ambiguity", "unevaluated_element")
		}
	}

	payload := parseProps(t, "Directory.Packages.props", `<Project Sdk="Microsoft.Build.NoTargets"><ItemGroup><PackageVersion Include="A" Version="1.0.0" /></ItemGroup></Project>`)
	requireAmbiguity(t, onlyRow(t, payload, "PackageVersion", "A"), "unevaluated_element")
}

func TestPropsCentralManagementNotProvenDemotesCentralRows(t *testing.T) {
	t.Parallel()

	items := `<ItemGroup>
<PackageVersion Include="A" Version="1.0.0" />
<GlobalPackageReference Include="G" Version="1.0.0" />
<PackageReference Include="R" Version="1.0.0" />
</ItemGroup>`
	for _, property := range []string{
		`<PropertyGroup><ManagePackageVersionsCentrally>false</ManagePackageVersionsCentrally></PropertyGroup>`,
		`<PropertyGroup Condition="'$(X)'=='y'"><ManagePackageVersionsCentrally>true</ManagePackageVersionsCentrally></PropertyGroup>`,
		`<PropertyGroup><ManagePackageVersionsCentrally>$(UseCpm)</ManagePackageVersionsCentrally></PropertyGroup>`,
		`<PropertyGroup><managepackageversionscentrally>true</managepackageversionscentrally><ManagePackageVersionsCentrally>false</ManagePackageVersionsCentrally></PropertyGroup>`,
	} {
		payload := parseProps(t, "Directory.Packages.props", `<Project>`+property+items+`</Project>`)
		requireAmbiguity(t, onlyRow(t, payload, "PackageVersion", "A"), "central_management_unproven")
		requireAmbiguity(t, onlyRow(t, payload, "GlobalPackageReference", "G"), "central_management_unproven")
		requireNoAmbiguity(t, onlyRow(t, payload, "PackageReference", "R"))
	}

	for _, property := range []string{
		``,
		`<PropertyGroup><ManagePackageVersionsCentrally> TRUE </ManagePackageVersionsCentrally></PropertyGroup>`,
	} {
		payload := parseProps(t, "Directory.Packages.props", `<Project>`+property+items+`</Project>`)
		requireNoAmbiguity(t, onlyRow(t, payload, "PackageVersion", "A"))
		requireNoAmbiguity(t, onlyRow(t, payload, "GlobalPackageReference", "G"))
	}
}

func TestPropsExcludeMakesTheRowPartial(t *testing.T) {
	t.Parallel()

	payload := parseProps(t, "Directory.Build.props", `<Project><ItemGroup>
<PackageReference Include="A" Exclude="A" Version="1.0.0" />
</ItemGroup></Project>`)
	requireAmbiguity(t, onlyRow(t, payload, "PackageReference", "A"), "exclude")
}

func TestPropsAmbiguityReasonsAreSortedAndJoined(t *testing.T) {
	t.Parallel()

	payload := parseProps(t, "Directory.Packages.props", `<Project><ItemGroup>
<PackageVersion Include="A" Exclude="Z"><Version Condition="'$(X)'=='y'">1.0.0</Version></PackageVersion>
<PackageVersion Include="A" Version="1.0.0" />
<PackageVersion Update="A" Version="2.0.0" />
</ItemGroup></Project>`)
	rows := rowsInSection(t, payload, "PackageVersion")
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	requireAmbiguity(t, rows[0], "conditional_metadata,duplicate_include,exclude,updated")
	requireAmbiguity(t, rows[1], "duplicate_include,updated")
}

func TestPropsIncludeWithRemoveOrUpdateIsSkipped(t *testing.T) {
	t.Parallel()

	payload := parseProps(t, "Directory.Packages.props", `<Project><ItemGroup>
<PackageVersion Include="A" Remove="A" Version="1.0.0" />
<PackageVersion Include="B" Update="B" Version="1.0.0" />
<PackageVersion Include="C" Version="1.0.0" />
</ItemGroup></Project>`)
	rows := rowsInSection(t, payload, "PackageVersion")
	if len(rows) != 1 || rows[0]["name"] != "C" {
		t.Fatalf("rows = %#v, want only C", rows)
	}
	// A and B still exist as literal names, so C is untouched.
	requireNoAmbiguity(t, rows[0])
}

// TestPropsCaseVariantUpdateOrRemoveDemotesEarlierRows pins that a wrongly
// cased Update or Remove attribute still retargets earlier rows in MSBuild
// (attribute names are case-insensitive), so the scanner demotes every
// earlier row of the item type instead of skipping only that element.
func TestPropsCaseVariantUpdateOrRemoveDemotesEarlierRows(t *testing.T) {
	t.Parallel()

	payload := parseProps(t, "Directory.Packages.props", `<Project><ItemGroup>
<PackageVersion Include="A" Version="1.0.0" />
<PackageVersion Include="B" Version="1.0.0" />
<GlobalPackageReference Include="E" Version="1.0.0" />
<PackageVersion update="A" Version="2.0.0" />
<PackageVersion REMOVE="B" />
<PackageVersion Include="C" Version="1.0.0" />
</ItemGroup></Project>`)
	// Each wildcard operation covers every earlier row of the type.
	requireAmbiguity(t, onlyRow(t, payload, "PackageVersion", "A"), "removed,updated")
	requireAmbiguity(t, onlyRow(t, payload, "PackageVersion", "B"), "removed,updated")
	// A later row of the type is defined after the Update, and another item
	// type is untouched.
	requireNoAmbiguity(t, onlyRow(t, payload, "PackageVersion", "C"))
	requireNoAmbiguity(t, onlyRow(t, payload, "GlobalPackageReference", "E"))
}
