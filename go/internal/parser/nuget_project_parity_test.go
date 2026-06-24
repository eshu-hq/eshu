// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"testing"
)

func TestParseNuGetProjectKeepsAmbiguousMSBuildPropertyPartial(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	path := filepath.Join(repoRoot, "Ambiguous.csproj")
	writeTestFile(t, path, `<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <NewtonsoftJsonVersion>13.0.1</NewtonsoftJsonVersion>
  </PropertyGroup>
  <PropertyGroup Condition="'$(TargetFramework)' == 'net8.0'">
    <NewtonsoftJsonVersion>13.0.3</NewtonsoftJsonVersion>
  </PropertyGroup>
  <ItemGroup>
    <PackageReference Include="Newtonsoft.Json" Version="$(NewtonsoftJsonVersion)" />
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

	dependency := assertBucketItemByName(t, payload, "variables", "Newtonsoft.Json")
	assertStringFieldValue(t, dependency, "value", "$(NewtonsoftJsonVersion)")
	assertStringFieldValue(t, dependency, "requested_version", "$(NewtonsoftJsonVersion)")
	assertStringFieldValue(t, dependency, "ambiguous_msbuild_property", "NewtonsoftJsonVersion")
	assertStringFieldValue(t, dependency, "version_evidence", "ambiguous_msbuild_property")
	assertBoolFieldValue(t, dependency, "partial_evidence", true)
}
