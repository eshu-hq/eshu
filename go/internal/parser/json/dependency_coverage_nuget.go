// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package json

// nugetCentralCoverage returns the coverage entries for the central NuGet
// declaration files (#8824). They live apart from DependencyCoverage so the
// matrix file stays under the file cap; DependencyCoverage appends them before
// it sorts.
func nugetCentralCoverage() []DependencyCoverageEntry {
	return []DependencyCoverageEntry{
		{
			Ecosystem:               "nuget",
			FilePattern:             "directory.build.props",
			FileKind:                "manifest",
			Status:                  DependencyCoverageCovered,
			CapturesPackageIdentity: true,
			CapturesVersionRange:    true,
			CapturesScope:           true,
			CapturesDevRuntimeSplit: true,
			SourceReference:         "go/internal/parser/nuget/props.go",
			Notes:                   "Shared PackageReference rows and literal PropertyGroup values from the file itself; conditional, expression, %XX-escaped, or ambiguous values stay partial, a row that a later Update or Remove, a duplicate Include, or an Import can change is partial with item_ambiguity, dependency scope is absent when the PrivateAssets or IncludeAssets value that decides it is unproven, and no sibling project file is read.",
		},
		{
			Ecosystem:               "nuget",
			FilePattern:             "directory.packages.props",
			FileKind:                "manifest",
			Status:                  DependencyCoverageCovered,
			CapturesPackageIdentity: true,
			CapturesVersionRange:    true,
			CapturesScope:           true,
			CapturesDevRuntimeSplit: true,
			SourceReference:         "go/internal/parser/nuget/props.go",
			Notes:                   "Central PackageVersion and GlobalPackageReference rows; a PackageVersion is a version pin, not proof that a project references the package, so neither the reducer nor the readiness package.consumption count treats it as consumption, and project PackageReference rows are not joined to it. A GlobalPackageReference is always development scope, because NuGet forces PrivateAssets=All on it. A row that a later Update or Remove, a duplicate Include, an Import, a %XX-escaped item spec, or an unproven ManagePackageVersionsCentrally can change is partial with item_ambiguity.",
		},
	}
}
