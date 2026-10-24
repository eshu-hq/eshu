// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nuget_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

// An escaped .csproj PackageReference spec is never a package identity
// (#8994). MSBuild unescapes %XX before it matches a spec, so "Foo%2EBar"
// names "Foo.Bar" once evaluated and an escaped spec can name any package. The
// props scanner already skips such specs (literalPackageName); the .csproj path
// must skip them too rather than emit a row under the escaped text. A percent
// sign not followed by two hex digits is literal text to MSBuild and keeps its
// row unchanged.
func TestParseNuGetProjectSkipsEscapedPackageSpecs(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	path := filepath.Join(repoRoot, "Escaped.csproj")
	writeTestFile(t, path, `<Project Sdk="Microsoft.NET.Sdk">
  <ItemGroup>
    <PackageReference Include="Escaped%2EDot" Version="1.0.0" />
    <PackageReference Include="%41lpha" Version="1.0.0" />
    <PackageReference Include="Percent%25Sign" Version="1.0.0" />
    <PackageReference Update="Updated%2EEscape" Version="1.0.0" />
    <PackageReference Include="Lower%2eHex" Version="1.0.0" />
    <PackageReference Include="Literal%zz" Version="1.0.0" />
    <PackageReference Include="Bare%" Version="1.0.0" />
    <PackageReference Include="Short%4" Version="1.0.0" />
    <PackageReference Include="Newtonsoft.Json" Version="13.0.3" />
  </ItemGroup>
</Project>`)

	payload := parsertest.MustParsePath(t, repoRoot, path)
	rows := csprojRowsByName(t, payload)
	for _, escaped := range []string{"Escaped%2EDot", "%41lpha", "Percent%25Sign", "Updated%2EEscape", "Lower%2eHex"} {
		if _, ok := rows[escaped]; ok {
			t.Fatalf("escaped spec %q emitted a row, want skipped", escaped)
		}
	}
	for literal, version := range map[string]string{
		"Literal%zz":      "1.0.0",
		"Bare%":           "1.0.0",
		"Short%4":         "1.0.0",
		"Newtonsoft.Json": "13.0.3",
	} {
		row, ok := rows[literal]
		if !ok {
			t.Fatalf("literal spec %q has no row, want one", literal)
		}
		parsertest.AssertStringFieldValue(t, row, "value", version)
		parsertest.AssertStringFieldValue(t, row, "version_evidence", "package_reference")
	}
	if got := len(rows); got != 4 {
		t.Fatalf("rows = %d, want 4 (literal specs only)", got)
	}
}

// An escaped .csproj Version is unresolved (#8994): the declared text is kept
// in value and requested_version, and the row is partial, whether the escape
// is written directly, in a <Version> child, or reached through a property
// whose value holds one. A version whose percent signs are not escapes stays a
// resolved package_reference version.
func TestParseNuGetProjectEscapedVersionIsUnresolved(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	path := filepath.Join(repoRoot, "EscapedVersion.csproj")
	writeTestFile(t, path, `<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <EscapedProp>2.0%2E0</EscapedProp>
    <CleanProp>3.0.0</CleanProp>
  </PropertyGroup>
  <ItemGroup>
    <PackageReference Include="Dot" Version="1.0%2E0" />
    <PackageReference Include="Leading" Version="%31.0.0" />
    <PackageReference Include="Percent" Version="1.0.0%25" />
    <PackageReference Include="Child"><Version>1.0%2E0</Version></PackageReference>
    <PackageReference Include="ViaProperty" Version="$(EscapedProp)" />
    <PackageReference Include="MixedProperty" Version="$(CleanProp)%2E1" />
    <PackageReference Include="LiteralZz" Version="1.0%zz" />
    <PackageReference Include="BarePercent" Version="1.0%" />
    <PackageReference Include="Clean" Version="$(CleanProp)" />
  </ItemGroup>
</Project>`)

	payload := parsertest.MustParsePath(t, repoRoot, path)
	rows := csprojRowsByName(t, payload)
	unresolved := map[string]string{
		"Dot":           "1.0%2E0",
		"Leading":       "%31.0.0",
		"Percent":       "1.0.0%25",
		"Child":         "1.0%2E0",
		"ViaProperty":   "$(EscapedProp)",
		"MixedProperty": "$(CleanProp)%2E1",
	}
	for name, raw := range unresolved {
		row := rows[name]
		if row == nil {
			t.Fatalf("row %q missing", name)
		}
		parsertest.AssertStringFieldValue(t, row, "value", raw)
		parsertest.AssertStringFieldValue(t, row, "requested_version", raw)
		parsertest.AssertStringFieldValue(t, row, "version_evidence", "unresolved_msbuild_property")
		assertBoolFieldValue(t, row, "partial_evidence", true)
		if _, ok := row["version_property"]; ok {
			t.Fatalf("row %q version_property = %#v, want absent for an unresolved version", name, row["version_property"])
		}
	}
	for name, want := range map[string]string{"LiteralZz": "1.0%zz", "BarePercent": "1.0%"} {
		row := rows[name]
		parsertest.AssertStringFieldValue(t, row, "value", want)
		parsertest.AssertStringFieldValue(t, row, "version_evidence", "package_reference")
		if _, ok := row["partial_evidence"]; ok {
			t.Fatalf("row %q partial_evidence set, want absent for a literal version", name)
		}
	}
	clean := rows["Clean"]
	parsertest.AssertStringFieldValue(t, clean, "value", "3.0.0")
	parsertest.AssertStringFieldValue(t, clean, "version_evidence", "project_property")
}

// csprojRowsByName indexes the PackageReference rows by name and fails when a
// name repeats, so a skipped spec cannot hide behind a duplicate.
func csprojRowsByName(t *testing.T, payload map[string]any) map[string]map[string]any {
	t.Helper()

	out := map[string]map[string]any{}
	for _, row := range rowsInSection(t, payload, "PackageReference") {
		name, _ := row["name"].(string)
		if _, dup := out[name]; dup {
			t.Fatalf("row %q emitted twice", name)
		}
		out[name] = row
	}
	return out
}
