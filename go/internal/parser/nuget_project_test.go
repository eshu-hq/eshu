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
