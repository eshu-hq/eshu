// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nuget_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
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

	payload := parsertest.MustParsePath(t, repoRoot, path)

	dependency := parsertest.AssertBucketItemByName(t, payload, "variables", "Newtonsoft.Json")
	parsertest.AssertStringFieldValue(t, dependency, "value", "$(NewtonsoftJsonVersion)")
	parsertest.AssertStringFieldValue(t, dependency, "requested_version", "$(NewtonsoftJsonVersion)")
	parsertest.AssertStringFieldValue(t, dependency, "ambiguous_msbuild_property", "NewtonsoftJsonVersion")
	parsertest.AssertStringFieldValue(t, dependency, "version_evidence", "ambiguous_msbuild_property")
	assertBoolFieldValue(t, dependency, "partial_evidence", true)
}
